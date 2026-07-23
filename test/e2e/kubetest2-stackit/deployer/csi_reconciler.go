package deployer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	deployassets "github.com/stackitcloud/cloud-provider-stackit/deploy"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

const (
	csiTestDriverFileName = "csi-testdriver.yaml"

	kubeSystemNamespace               = "kube-system"
	stackitCloudConfigMapName         = "stackit-cloud-config"
	stackitCloudConfigMapKey          = "cloud.yaml"
	stackitCloudSecretName            = "stackit-cloud-secret"
	stackitCloudSecretKey             = "sa_key.json"
	snapshotControllerDeploymentName  = "snapshot-controller"
	csiControllerDeploymentName       = "csi-stackit-controllerplugin"
	csiNodeDaemonSetName              = "csi-stackit-nodeplugin"
	csiStackitPluginContainerName     = "stackit-csi-plugin"
	nodeDriverRegistrarContainerName  = "node-driver-registrar"
	csiSocketVolumeName               = "socket-dir"
	defaultManifestPollInterval       = 2 * time.Second
	defaultManifestReadinessTimeout   = 5 * time.Minute
	defaultBlockStorageRescanOnResize = true
)

var (
	customResourceDefinitionGVR = schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}
)

type csiInstallConfig struct {
	KubeconfigPath       string
	ProjectID            string
	Region               string
	ServiceAccountJSON   string
	DriverName           string
	StorageClassName     string
	StorageClassType     string
	SnapshotClassName    string
	SnapshotType         string
	ImageName            string
	ImageTag             string
	RescanOnResize       bool
	TestDriverOutputPath string
}

func (c csiInstallConfig) pluginImage() string {
	return fmt.Sprintf("%s:%s", c.ImageName, c.ImageTag)
}

type csiReconciler interface {
	Reconcile(context.Context, csiInstallConfig) error
}

type clientFactory func(string) (kubernetes.Interface, dynamic.Interface, error)

type manifestCSIReconciler struct {
	assets     fs.FS
	newClients clientFactory
}

type renderedCSIAssets struct {
	snapshotCRDs       []*unstructured.Unstructured
	snapshotController []*unstructured.Unstructured
	csi                []*unstructured.Unstructured
}

func newManifestCSIReconciler() *manifestCSIReconciler {
	return &manifestCSIReconciler{
		assets:     deployassets.FS,
		newClients: newCSIClients,
	}
}

func newCSIClients(kubeconfigPath string) (kubernetes.Interface, dynamic.Interface, error) {
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}

	return kubeClient, dynamicClient, nil
}

func (r *manifestCSIReconciler) Reconcile(ctx context.Context, cfg csiInstallConfig) error {
	kubeClient, dynamicClient, err := r.newClients(cfg.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("build kubernetes clients: %w", err)
	}

	assets, err := r.renderAssets(cfg)
	if err != nil {
		return err
	}

	if err := ensureNamespace(ctx, kubeClient, kubeSystemNamespace); err != nil {
		return err
	}
	if err := upsertCloudConfig(ctx, kubeClient, cfg); err != nil {
		return err
	}
	if err := upsertCloudSecret(ctx, kubeClient, cfg); err != nil {
		return err
	}

	if err := r.applyObjects(ctx, dynamicClient, assets.snapshotCRDs); err != nil {
		return fmt.Errorf("apply snapshot CRDs: %w", err)
	}
	for _, name := range []string{
		"volumesnapshots.snapshot.storage.k8s.io",
		"volumesnapshotclasses.snapshot.storage.k8s.io",
		"volumesnapshotcontents.snapshot.storage.k8s.io",
	} {
		if err := waitForCRDEstablished(ctx, dynamicClient, name); err != nil {
			return err
		}
	}

	if err := r.applyObjects(ctx, dynamicClient, assets.snapshotController); err != nil {
		return fmt.Errorf("apply snapshot controller: %w", err)
	}
	if err := waitForDeploymentRollout(ctx, kubeClient, kubeSystemNamespace, snapshotControllerDeploymentName); err != nil {
		return err
	}

	if err := r.applyObjects(ctx, dynamicClient, assets.csi); err != nil {
		return fmt.Errorf("apply STACKIT CSI manifests: %w", err)
	}
	if err := waitForDeploymentRollout(ctx, kubeClient, kubeSystemNamespace, csiControllerDeploymentName); err != nil {
		return err
	}
	if err := waitForDaemonSetRollout(ctx, kubeClient, kubeSystemNamespace, csiNodeDaemonSetName); err != nil {
		return err
	}

	return writeCSITestDriverConfig(cfg.TestDriverOutputPath, cfg)
}

func (r *manifestCSIReconciler) renderAssets(cfg csiInstallConfig) (*renderedCSIAssets, error) {
	snapshotCRDs, err := r.loadObjects([]string{
		"snapshot-controller/crds/snapshot.storage.k8s.io_volumesnapshots.yaml",
		"snapshot-controller/crds/snapshot.storage.k8s.io_volumesnapshotclasses.yaml",
		"snapshot-controller/crds/snapshot.storage.k8s.io_volumesnapshotcontents.yaml",
	}, nil)
	if err != nil {
		return nil, err
	}

	snapshotController, err := r.loadObjects([]string{
		"snapshot-controller/controller.yaml",
	}, nil)
	if err != nil {
		return nil, err
	}

	csiObjects, err := r.loadObjects([]string{
		"csi-plugin/controllerplugin-rbac.yaml",
		"csi-plugin/nodeplugin-rbac.yaml",
		"csi-plugin/controllerplugin.yaml",
		"csi-plugin/nodeplugin.yaml",
		"csi-plugin/csi-driver.yaml",
		"csi-plugin/storageclass.yaml",
		"csi-plugin/snapshotclass.yaml",
	}, func(obj *unstructured.Unstructured) error {
		return patchCSIObject(obj, cfg)
	})
	if err != nil {
		return nil, err
	}

	return &renderedCSIAssets{
		snapshotCRDs:       snapshotCRDs,
		snapshotController: snapshotController,
		csi:                csiObjects,
	}, nil
}

func (r *manifestCSIReconciler) loadObjects(paths []string, patch func(*unstructured.Unstructured) error) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured
	for _, path := range paths {
		content, err := fs.ReadFile(r.assets, path)
		if err != nil {
			return nil, fmt.Errorf("read embedded manifest %q: %w", path, err)
		}

		decoded, err := decodeManifestObjects(content)
		if err != nil {
			return nil, fmt.Errorf("decode manifest %q: %w", path, err)
		}

		for _, obj := range decoded {
			if patch != nil {
				if err := patch(obj); err != nil {
					return nil, fmt.Errorf("patch manifest %q %s/%s: %w", path, obj.GetKind(), obj.GetName(), err)
				}
			}
			objects = append(objects, obj)
		}
	}

	return objects, nil
}

func decodeManifestObjects(content []byte) ([]*unstructured.Unstructured, error) {
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(content)))
	objects := make([]*unstructured.Unstructured, 0)

	for {
		document, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return objects, nil
		}
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(document)) == 0 {
			continue
		}

		raw := map[string]any{}
		decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(document), 4096)
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			continue
		}

		objects = append(objects, &unstructured.Unstructured{Object: raw})
	}
}

func patchCSIObject(obj *unstructured.Unstructured, cfg csiInstallConfig) error {
	switch {
	case matchesGVK(obj, "storage.k8s.io/v1", "StorageClass"):
		obj.SetName(cfg.StorageClassName)
		if err := unstructured.SetNestedField(obj.Object, cfg.DriverName, "provisioner"); err != nil {
			return err
		}
		return unstructured.SetNestedField(obj.Object, cfg.StorageClassType, "parameters", "type")
	case matchesGVK(obj, "snapshot.storage.k8s.io/v1", "VolumeSnapshotClass"):
		obj.SetName(cfg.SnapshotClassName)
		if err := unstructured.SetNestedField(obj.Object, cfg.DriverName, "driver"); err != nil {
			return err
		}
		return unstructured.SetNestedField(obj.Object, cfg.SnapshotType, "parameters", "type")
	case matchesGVK(obj, "storage.k8s.io/v1", "CSIDriver"):
		obj.SetName(cfg.DriverName)
		return nil
	case matchesGVK(obj, "apps/v1", "Deployment") && obj.GetName() == csiControllerDeploymentName:
		return patchControllerDeployment(obj, cfg)
	case matchesGVK(obj, "apps/v1", "DaemonSet") && obj.GetName() == csiNodeDaemonSetName:
		return patchNodeDaemonSet(obj, cfg)
	default:
		return nil
	}
}

func patchControllerDeployment(obj *unstructured.Unstructured, cfg csiInstallConfig) error {
	return patchPodTemplateContainers(obj, func(container map[string]any) error {
		if value, ok := container["name"].(string); ok && value == csiStackitPluginContainerName {
			container["image"] = cfg.pluginImage()
			container["imagePullPolicy"] = string(corev1.PullAlways)
			args, err := stringSliceFromAny(container["args"])
			if err != nil {
				return err
			}
			container["args"] = stringSliceToAny(upsertFlag(args, "--driver-name", cfg.DriverName))
		}
		return nil
	})
}

func patchNodeDaemonSet(obj *unstructured.Unstructured, cfg csiInstallConfig) error {
	if err := patchPodTemplateContainers(obj, func(container map[string]any) error {
		name, _ := container["name"].(string)
		switch name {
		case csiStackitPluginContainerName:
			container["image"] = cfg.pluginImage()
			container["imagePullPolicy"] = string(corev1.PullAlways)
			args, err := stringSliceFromAny(container["args"])
			if err != nil {
				return err
			}
			container["args"] = stringSliceToAny(upsertFlag(args, "--driver-name", cfg.DriverName))
		case nodeDriverRegistrarContainerName:
			if err := setEnvVar(container, "DRIVER_REG_SOCK_PATH", fmt.Sprintf("/var/lib/kubelet/plugins/%s/csi.sock", cfg.DriverName)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	volumes, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")
	if err != nil {
		return err
	}
	if !found {
		return errors.New("daemonset is missing pod volumes")
	}
	for i := range volumes {
		volume, ok := volumes[i].(map[string]any)
		if !ok {
			continue
		}
		if name, _ := volume["name"].(string); name != csiSocketVolumeName {
			continue
		}
		if err := unstructured.SetNestedField(volume, fmt.Sprintf("/var/lib/kubelet/plugins/%s", cfg.DriverName), "hostPath", "path"); err != nil {
			return err
		}
		volumes[i] = volume
		break
	}

	return unstructured.SetNestedSlice(obj.Object, volumes, "spec", "template", "spec", "volumes")
}

func patchPodTemplateContainers(obj *unstructured.Unstructured, patch func(map[string]any) error) error {
	containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	if err != nil {
		return err
	}
	if !found {
		return errors.New("workload is missing pod containers")
	}

	for i := range containers {
		container, ok := containers[i].(map[string]any)
		if !ok {
			return fmt.Errorf("unexpected container shape: %T", containers[i])
		}
		if err := patch(container); err != nil {
			return err
		}
		containers[i] = container
	}

	return unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers")
}

func stringSliceFromAny(value any) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("unexpected list entry type %T", item)
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unexpected list type %T", value)
	}
}

func stringSliceToAny(values []string) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func upsertFlag(args []string, flagName, value string) []string {
	desired := flagName + "=" + value
	result := make([]string, 0, len(args)+1)
	replaced := false
	skipNext := false

	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == flagName {
			replaced = true
			result = append(result, desired)
			if i+1 < len(args) {
				skipNext = true
			}
			continue
		}
		if strings.HasPrefix(arg, flagName+"=") {
			replaced = true
			result = append(result, desired)
			continue
		}
		result = append(result, arg)
	}

	if !replaced {
		result = append(result, desired)
	}

	return result
}

func setEnvVar(container map[string]any, envName, value string) error {
	envList, err := nestedSliceMap(container, "env")
	if err != nil {
		return err
	}

	for i := range envList {
		name, _ := envList[i]["name"].(string)
		if name != envName {
			continue
		}
		envList[i]["value"] = value
		delete(envList[i], "valueFrom")
		container["env"] = sliceMapToAny(envList)
		return nil
	}

	envList = append(envList, map[string]any{
		"name":  envName,
		"value": value,
	})
	container["env"] = sliceMapToAny(envList)
	return nil
}

func nestedSliceMap(object map[string]any, fields ...string) ([]map[string]any, error) {
	items, found, err := unstructured.NestedSlice(object, fields...)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		mapped, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unexpected list entry type %T", item)
		}
		result = append(result, mapped)
	}

	return result, nil
}

func sliceMapToAny(items []map[string]any) []any {
	result := make([]any, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	return result
}

func matchesGVK(obj *unstructured.Unstructured, apiVersion, kind string) bool {
	return obj.GetAPIVersion() == apiVersion && obj.GetKind() == kind
}

func ensureNamespace(ctx context.Context, client kubernetes.Interface, name string) error {
	_, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get namespace %q: %w", name, err)
	}

	_, err = client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %q: %w", name, err)
	}
	return nil
}

func upsertCloudConfig(ctx context.Context, client kubernetes.Interface, cfg csiInstallConfig) error {
	content, err := buildCSICloudConfig(cfg)
	if err != nil {
		return err
	}

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stackitCloudConfigMapName,
			Namespace: kubeSystemNamespace,
		},
		Data: map[string]string{
			stackitCloudConfigMapKey: content,
		},
	}

	existing, err := client.CoreV1().ConfigMaps(kubeSystemNamespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.CoreV1().ConfigMaps(kubeSystemNamespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create configmap %q: %w", desired.Name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get configmap %q: %w", desired.Name, err)
	}

	desired.ResourceVersion = existing.ResourceVersion
	_, err = client.CoreV1().ConfigMaps(kubeSystemNamespace).Update(ctx, desired, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update configmap %q: %w", desired.Name, err)
	}
	return nil
}

func upsertCloudSecret(ctx context.Context, client kubernetes.Interface, cfg csiInstallConfig) error {
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stackitCloudSecretName,
			Namespace: kubeSystemNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			stackitCloudSecretKey: []byte(cfg.ServiceAccountJSON),
		},
	}

	existing, err := client.CoreV1().Secrets(kubeSystemNamespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.CoreV1().Secrets(kubeSystemNamespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create secret %q: %w", desired.Name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get secret %q: %w", desired.Name, err)
	}

	desired.ResourceVersion = existing.ResourceVersion
	_, err = client.CoreV1().Secrets(kubeSystemNamespace).Update(ctx, desired, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update secret %q: %w", desired.Name, err)
	}
	return nil
}

func buildCSICloudConfig(cfg csiInstallConfig) (string, error) {
	config := map[string]any{
		"global": map[string]any{
			"projectId": cfg.ProjectID,
			"region":    cfg.Region,
		},
		"blockStorage": map[string]any{
			"rescanOnResize": cfg.RescanOnResize,
		},
	}

	content, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("marshal CSI cloud config: %w", err)
	}

	return string(content), nil
}

func waitForCRDEstablished(ctx context.Context, client dynamic.Interface, name string) error {
	klog.Infof("Waiting for CRD %q to become established", name)
	deadlineCtx, cancel := context.WithTimeout(ctx, defaultManifestReadinessTimeout)
	defer cancel()

	for {
		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("wait for CRD %q established: %w", name, deadlineCtx.Err())
		case <-time.After(defaultManifestPollInterval):
			object, err := client.Resource(customResourceDefinitionGVR).Get(deadlineCtx, name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("get CRD %q: %w", name, err)
			}

			conditions, found, err := unstructured.NestedSlice(object.Object, "status", "conditions")
			if err != nil {
				return fmt.Errorf("read CRD %q conditions: %w", name, err)
			}
			if !found {
				continue
			}
			for _, condition := range conditions {
				conditionMap, ok := condition.(map[string]any)
				if !ok {
					continue
				}
				if conditionMap["type"] == "Established" && conditionMap["status"] == "True" {
					return nil
				}
			}
		}
	}
}

func waitForDeploymentRollout(ctx context.Context, client kubernetes.Interface, namespace, name string) error {
	klog.Infof("Waiting for deployment %q in namespace %q to roll out", name, namespace)
	deadlineCtx, cancel := context.WithTimeout(ctx, defaultManifestReadinessTimeout)
	defer cancel()

	for {
		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("wait for deployment %q rollout: %w", name, deadlineCtx.Err())
		case <-time.After(defaultManifestPollInterval):
			deployment, err := client.AppsV1().Deployments(namespace).Get(deadlineCtx, name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("get deployment %q: %w", name, err)
			}

			if deploymentReady(deployment) {
				return nil
			}
		}
	}
}

func waitForDaemonSetRollout(ctx context.Context, client kubernetes.Interface, namespace, name string) error {
	klog.Infof("Waiting for daemonset %q in namespace %q to roll out", name, namespace)
	deadlineCtx, cancel := context.WithTimeout(ctx, defaultManifestReadinessTimeout)
	defer cancel()

	for {
		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("wait for daemonset %q rollout: %w", name, deadlineCtx.Err())
		case <-time.After(defaultManifestPollInterval):
			daemonSet, err := client.AppsV1().DaemonSets(namespace).Get(deadlineCtx, name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("get daemonset %q: %w", name, err)
			}

			if daemonSetReady(daemonSet) {
				return nil
			}
		}
	}
}

func deploymentReady(deployment *appsv1.Deployment) bool {
	if deployment == nil || deployment.Spec.Replicas == nil {
		return false
	}

	expected := *deployment.Spec.Replicas
	return deployment.Status.ObservedGeneration >= deployment.Generation &&
		deployment.Status.UpdatedReplicas == expected &&
		deployment.Status.AvailableReplicas == expected
}

func daemonSetReady(daemonSet *appsv1.DaemonSet) bool {
	if daemonSet == nil {
		return false
	}

	expected := daemonSet.Status.DesiredNumberScheduled
	return daemonSet.Status.ObservedGeneration >= daemonSet.Generation &&
		daemonSet.Status.UpdatedNumberScheduled == expected &&
		daemonSet.Status.NumberAvailable == expected
}

func (r *manifestCSIReconciler) applyObjects(ctx context.Context, client dynamic.Interface, objects []*unstructured.Unstructured) error {
	for _, object := range objects {
		if err := r.applyObject(ctx, client, object); err != nil {
			return err
		}
	}
	return nil
}

func (r *manifestCSIReconciler) applyObject(ctx context.Context, client dynamic.Interface, object *unstructured.Unstructured) error {
	resource, err := resourceInterfaceFor(client, object)
	if err != nil {
		return err
	}

	current, err := resource.Get(ctx, object.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = resource.Create(ctx, object, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create %s %q: %w", object.GetKind(), object.GetName(), err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get %s %q: %w", object.GetKind(), object.GetName(), err)
	}

	object = object.DeepCopy()
	object.SetResourceVersion(current.GetResourceVersion())
	_, err = resource.Update(ctx, object, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update %s %q: %w", object.GetKind(), object.GetName(), err)
	}
	return nil
}

func resourceInterfaceFor(client dynamic.Interface, object *unstructured.Unstructured) (dynamic.ResourceInterface, error) {
	gvr, namespaced, err := gvrForObject(object)
	if err != nil {
		return nil, err
	}
	if namespaced {
		namespace := object.GetNamespace()
		if namespace == "" {
			return nil, fmt.Errorf("%s %q is missing namespace", object.GetKind(), object.GetName())
		}
		return client.Resource(gvr).Namespace(namespace), nil
	}
	return client.Resource(gvr), nil
}

func gvrForObject(object *unstructured.Unstructured) (schema.GroupVersionResource, bool, error) {
	gvk := object.GroupVersionKind()

	switch {
	case gvk.Group == "" && gvk.Version == "v1" && gvk.Kind == "ServiceAccount":
		return schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}, true, nil
	case gvk.Group == "rbac.authorization.k8s.io" && gvk.Version == "v1" && gvk.Kind == "ClusterRole":
		return schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: "clusterroles"}, false, nil
	case gvk.Group == "rbac.authorization.k8s.io" && gvk.Version == "v1" && gvk.Kind == "ClusterRoleBinding":
		return schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: "clusterrolebindings"}, false, nil
	case gvk.Group == "apps" && gvk.Version == "v1" && gvk.Kind == "Deployment":
		return schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: "deployments"}, true, nil
	case gvk.Group == "apps" && gvk.Version == "v1" && gvk.Kind == "DaemonSet":
		return schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: "daemonsets"}, true, nil
	case gvk.Group == "storage.k8s.io" && gvk.Version == "v1" && gvk.Kind == "CSIDriver":
		return schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: "csidrivers"}, false, nil
	case gvk.Group == "storage.k8s.io" && gvk.Version == "v1" && gvk.Kind == "StorageClass":
		return schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: "storageclasses"}, false, nil
	case gvk.Group == "snapshot.storage.k8s.io" && gvk.Version == "v1" && gvk.Kind == "VolumeSnapshotClass":
		return schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: "volumesnapshotclasses"}, false, nil
	case gvk.Group == "apiextensions.k8s.io" && gvk.Version == "v1" && gvk.Kind == "CustomResourceDefinition":
		return customResourceDefinitionGVR, false, nil
	default:
		return schema.GroupVersionResource{}, false, fmt.Errorf("unsupported manifest kind %s %s", gvk.String(), object.GetName())
	}
}

type csiTestDriverConfig struct {
	StorageClass  csiTestClassConfig `yaml:"StorageClass"`
	SnapshotClass csiTestClassConfig `yaml:"SnapshotClass"`
	DriverInfo    csiTestDriverInfo  `yaml:"DriverInfo"`
}

type csiTestClassConfig struct {
	FromExistingClassName string `yaml:"FromExistingClassName"`
}

type csiTestDriverInfo struct {
	Name         string                    `yaml:"Name"`
	Capabilities csiTestDriverCapabilities `yaml:"Capabilities"`
}

type csiTestDriverCapabilities struct {
	Block               bool `yaml:"block" json:"block"`
	ControllerExpansion bool `yaml:"controllerExpansion" json:"controllerExpansion"`
	FSGroup             bool `yaml:"fsGroup" json:"fsGroup"`
	Exec                bool `yaml:"exec" json:"exec"`
	RWX                 bool `yaml:"rwx" json:"rwx"`
	Multipods           bool `yaml:"multipods" json:"multipods"`
	Persistence         bool `yaml:"persistence" json:"persistence"`
	PVCDataSource       bool `yaml:"pvcDataSource" json:"pvcDataSource"`
	SnapshotDataSource  bool `yaml:"snapshotDataSource" json:"snapshotDataSource"`
	Topology            bool `yaml:"topology" json:"topology"`
	Capacity            bool `yaml:"capacity" json:"capacity"`
	ReadWriteOncePod    bool `yaml:"readWriteOncePod" json:"readWriteOncePod"`
	MultiplePVsSameID   bool `yaml:"multiplePVsSameID" json:"multiplePVsSameID"`
	CapReadOnlyMany     bool `yaml:"capReadOnlyMany" json:"capReadOnlyMany"`
}

func writeCSITestDriverConfig(path string, cfg csiInstallConfig) error {
	content, err := yaml.Marshal(csiTestDriverConfig{
		StorageClass: csiTestClassConfig{
			FromExistingClassName: cfg.StorageClassName,
		},
		SnapshotClass: csiTestClassConfig{
			FromExistingClassName: cfg.SnapshotClassName,
		},
		DriverInfo: csiTestDriverInfo{
			Name: cfg.DriverName,
			Capabilities: csiTestDriverCapabilities{
				Block:               true,
				ControllerExpansion: true,
				FSGroup:             true,
				Exec:                true,
				RWX:                 false,
				Multipods:           false,
				Persistence:         true,
				PVCDataSource:       true,
				SnapshotDataSource:  true,
				Topology:            true,
				Capacity:            false,
				ReadWriteOncePod:    true,
				MultiplePVsSameID:   false,
				CapReadOnlyMany:     false,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("marshal CSI testdriver config: %w", err)
	}

	return os.WriteFile(path, content, 0o600)
}
