package kubetest2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

type csiApplier func(ctx context.Context, kubeconfigPath, stagedKustomizeDir string) error

const (
	csiFieldManager   = "kubetest2-stackit"
	csiPollInterval   = 2 * time.Second
	csiRolloutTimeout = 5 * time.Minute

	csiManifestsPath          = "test/kustomize"
	baseManifestsPath         = "deploy/csi-plugin"
	csiTestDriverTemplatePath = "test/csi-plugin/block-storage.yaml"
	csiTestDriverFileName     = "csi-testdriver.yaml"

	kubetest2CSIStorageClassName = "premium-perf4-stackit-kubetest2"
	kubetest2CSIDriverName       = "kubetest2.csi.stackit.cloud"

	csiConfigMapName   = "stackit-cloud-config"
	csiConfigMapKey    = "cloud.yaml"
	csiConfigNamespace = "kube-system"
)

// ensureCSI deploys the STACKIT CSI driver into the freshly provisioned
// cluster as the last bootstrap step. It stages the Kustomize overlay and base
// manifests into the run directory, applies the necessary image, project, region,
// and credential substitutions, and applies the manifests to the cluster using
// native Go Kubernetes clients.
func (d *Deployer) ensureCSI(ctx context.Context) error {
	klog.Infof("Starting CSI deployment into cluster=%q", d.clusterName())

	serviceAccountKey, ok, err := d.readCachedChildServiceAccountKey()
	if err != nil {
		return fmt.Errorf("read cached child service-account key %q: %w", d.serviceAccountKeyPath, err)
	}
	if !ok || strings.TrimSpace(serviceAccountKey) == "" {
		return fmt.Errorf("cached child service-account key is missing or empty at %q", d.serviceAccountKeyPath)
	}

	stagedKustomizeDir, err := d.stageCSIManifests(serviceAccountKey)
	if err != nil {
		return fmt.Errorf("stage CSI manifests: %w", err)
	}
	if _, err := d.stageCSITestDriver(); err != nil {
		return fmt.Errorf("stage CSI testdriver: %w", err)
	}

	applier := d.csiApplier
	if applier == nil {
		applier = applyCSIManifestsNative
	}

	if err := applier(ctx, d.kubeconfigPath, stagedKustomizeDir); err != nil {
		return fmt.Errorf("apply CSI manifests: %w", err)
	}

	klog.Infof("CSI deployment completed successfully for cluster=%q", d.clusterName())
	return nil
}

func applyCSIManifestsNative(ctx context.Context, kubeconfigPath, stagedKustomizeDir string) error {
	objects, err := renderKustomize(stagedKustomizeDir)
	if err != nil {
		return fmt.Errorf("render Kustomize overlay %q: %w", stagedKustomizeDir, err)
	}

	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return fmt.Errorf("build rest config from %q: %w", kubeconfigPath, err)
	}

	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create dynamic client: %w", err)
	}

	discoClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create discovery client: %w", err)
	}

	cachedDisco := memory.NewMemCacheClient(discoClient)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDisco)

	klog.Infof("Applying %d rendered Kubernetes objects via server-side apply", len(objects))
	if err := applyUnstructuredObjects(ctx, dynClient, mapper, objects); err != nil {
		return fmt.Errorf("apply Kubernetes objects: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create kubernetes clientset: %w", err)
	}

	klog.Infof("Waiting for CSI controller deployment rollout")
	if err := waitForDeploymentReady(ctx, clientset, "kube-system", "csi-stackit-controllerplugin", csiRolloutTimeout); err != nil {
		return fmt.Errorf("wait for CSI controller deployment rollout: %w", err)
	}

	klog.Infof("Waiting for CSI node daemonset rollout")
	if err := waitForDaemonSetReady(ctx, clientset, "kube-system", "csi-stackit-nodeplugin", csiRolloutTimeout); err != nil {
		return fmt.Errorf("wait for CSI node daemonset rollout: %w", err)
	}

	return nil
}

func renderKustomize(dir string) ([]*unstructured.Unstructured, error) {
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fSys := filesys.MakeFsOnDisk()

	resMap, err := k.Run(fSys, dir)
	if err != nil {
		return nil, fmt.Errorf("kustomize run: %w", err)
	}

	yamlBytes, err := resMap.AsYaml()
	if err != nil {
		return nil, fmt.Errorf("kustomize as yaml: %w", err)
	}

	return decodeYAMLToUnstructured(yamlBytes)
}

func decodeYAMLToUnstructured(yamlBytes []byte) ([]*unstructured.Unstructured, error) {
	decoder := k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader(yamlBytes), 4096)
	var objects []*unstructured.Unstructured

	for {
		var rawObj map[string]interface{}
		err := decoder.Decode(&rawObj)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode YAML object: %w", err)
		}
		if len(rawObj) == 0 {
			continue
		}

		u := &unstructured.Unstructured{Object: rawObj}
		if u.GetKind() == "" || u.GetName() == "" {
			continue
		}
		objects = append(objects, u)
	}

	return objects, nil
}

func applyUnstructuredObjects(ctx context.Context, dynClient dynamic.Interface, mapper meta.RESTMapper, objects []*unstructured.Unstructured) error {
	for _, obj := range objects {
		gvk := obj.GroupVersionKind()
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return fmt.Errorf("get REST mapping for %s: %w", gvk.String(), err)
		}

		var ri dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			ns := obj.GetNamespace()
			if ns == "" {
				ns = metav1.NamespaceDefault
			}
			ri = dynClient.Resource(mapping.Resource).Namespace(ns)
		} else {
			ri = dynClient.Resource(mapping.Resource)
		}

		objJSON, err := json.Marshal(obj)
		if err != nil {
			return fmt.Errorf("marshal object %s/%s to JSON: %w", gvk.Kind, obj.GetName(), err)
		}

		force := true
		applyOpts := metav1.PatchOptions{
			FieldManager: csiFieldManager,
			Force:        &force,
		}

		klog.Infof("Server-side applying %s %s/%s", gvk.Kind, obj.GetNamespace(), obj.GetName())
		if _, err := ri.Patch(ctx, obj.GetName(), types.ApplyPatchType, objJSON, applyOpts); err != nil {
			return fmt.Errorf("server-side apply %s %s/%s: %w", gvk.Kind, obj.GetNamespace(), obj.GetName(), err)
		}
	}
	return nil
}

func waitForDeploymentReady(ctx context.Context, clientset kubernetes.Interface, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, csiPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		deploy, err := clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		specReplicas := int32(1)
		if deploy.Spec.Replicas != nil {
			specReplicas = *deploy.Spec.Replicas
		}

		if deploy.Status.ObservedGeneration >= deploy.Generation &&
			deploy.Status.UpdatedReplicas == specReplicas &&
			deploy.Status.AvailableReplicas == specReplicas &&
			deploy.Status.UnavailableReplicas == 0 {
			return true, nil
		}
		return false, nil
	})
}

func waitForDaemonSetReady(ctx context.Context, clientset kubernetes.Interface, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, csiPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		ds, err := clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		if ds.Status.ObservedGeneration >= ds.Generation &&
			ds.Status.DesiredNumberScheduled > 0 &&
			ds.Status.UpdatedNumberScheduled == ds.Status.DesiredNumberScheduled &&
			ds.Status.NumberAvailable == ds.Status.DesiredNumberScheduled &&
			ds.Status.NumberUnavailable == 0 {
			return true, nil
		}
		return false, nil
	})
}

func resolveManifestPath(relPath string) (string, error) {
	candidates := []string{
		relPath,
		filepath.Join("..", "..", relPath),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Abs(candidate)
		}
	}

	return "", fmt.Errorf("manifest path %q not found (tried %v)", relPath, candidates)
}

func (d *Deployer) stageCSIManifests(serviceAccountKey string) (string, error) {
	stagingRoot := filepath.Join(d.options.RunDir(), "manifests")

	stagedBaseDir := filepath.Join(stagingRoot, "deploy", "csi-plugin")
	stagedKustomizeDir := filepath.Join(stagingRoot, "test", "kustomize")

	baseSrc, err := resolveManifestPath(baseManifestsPath)
	if err != nil {
		return "", fmt.Errorf("resolve base manifests path: %w", err)
	}

	csiSrc, err := resolveManifestPath(csiManifestsPath)
	if err != nil {
		return "", fmt.Errorf("resolve CSI overlay manifests path: %w", err)
	}

	if err := os.RemoveAll(stagingRoot); err != nil {
		return "", fmt.Errorf("clean existing manifests staging dir %q: %w", stagingRoot, err)
	}

	if err := copyDir(baseSrc, stagedBaseDir); err != nil {
		return "", fmt.Errorf("copy base CSI manifests from %q to %q: %w", baseSrc, stagedBaseDir, err)
	}

	if err := copyDir(csiSrc, stagedKustomizeDir); err != nil {
		return "", fmt.Errorf("copy CSI overlay manifests from %q to %q: %w", csiSrc, stagedKustomizeDir, err)
	}

	// 1. Replace image and tag in kustomization.yaml
	kustomizationPath := filepath.Join(stagedKustomizeDir, "kustomization.yaml")
	if err := replaceInFile(kustomizationPath, map[string]string{
		"REPLACE_WITH_IMAGE_NAME": d.csiImageName,
		"REPLACE_WITH_TAG":        d.csiImageTag,
	}); err != nil {
		return "", fmt.Errorf("substitute values in %q: %w", kustomizationPath, err)
	}

	cloudConfigPath := filepath.Join(stagedKustomizeDir, "cloud-config.yaml")
	cloudConfigContent, err := renderCSICloudConfigManifest(d.projectID, d.region, d.iaasEndpoint)
	if err != nil {
		return "", fmt.Errorf("render CSI cloud config manifest: %w", err)
	}
	if err := os.WriteFile(cloudConfigPath, cloudConfigContent, 0o600); err != nil {
		return "", fmt.Errorf("write CSI cloud config manifest %q: %w", cloudConfigPath, err)
	}

	cloudSecretPath := filepath.Join(stagedKustomizeDir, "cloud-secret.yaml")
	if err := replaceInFile(cloudSecretPath, map[string]string{
		"REPLACE_WITH_SERVICEACCOUNT_JSON": serviceAccountKey,
	}); err != nil {
		return "", fmt.Errorf("substitute values in %q: %w", cloudSecretPath, err)
	}

	return stagedKustomizeDir, nil
}

func (d *Deployer) stageCSITestDriver() (string, error) {
	templatePath, err := resolveManifestPath(csiTestDriverTemplatePath)
	if err != nil {
		return "", fmt.Errorf("resolve CSI testdriver template path: %w", err)
	}

	content, err := renderCSITestDriverConfig(templatePath)
	if err != nil {
		return "", err
	}

	outputPath := filepath.Join(d.options.RunDir(), csiTestDriverFileName)
	if err := os.WriteFile(outputPath, content, 0o600); err != nil {
		return "", fmt.Errorf("write CSI testdriver config %q: %w", outputPath, err)
	}

	return outputPath, nil
}

func renderCSITestDriverConfig(templatePath string) ([]byte, error) {
	content, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("read CSI testdriver template %q: %w", templatePath, err)
	}

	var config csiTestDriverConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("decode CSI testdriver template %q: %w", templatePath, err)
	}

	config.StorageClass.FromExistingClassName = kubetest2CSIStorageClassName
	config.DriverInfo.Name = kubetest2CSIDriverName

	rendered, err := yaml.Marshal(&config)
	if err != nil {
		return nil, fmt.Errorf("encode CSI testdriver config: %w", err)
	}

	return rendered, nil
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
	Name            string                    `yaml:"Name"`
	SupportedFsType map[string]map[string]any `yaml:"SupportedFsType,omitempty"`
	Capabilities    map[string]bool           `yaml:"Capabilities,omitempty"`
}

type csiCloudConfigManifest struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Metadata   csiCloudConfigMetadata `yaml:"metadata"`
	Data       map[string]string      `yaml:"data"`
}

type csiCloudConfigMetadata struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

type csiCloudConfig struct {
	Global       csiCloudConfigGlobal       `yaml:"global"`
	BlockStorage csiCloudConfigBlockStorage `yaml:"blockStorage"`
}

type csiCloudConfigGlobal struct {
	ProjectID    string                      `yaml:"projectId"`
	Region       string                      `yaml:"region"`
	APIEndpoints *csiCloudConfigAPIEndpoints `yaml:"apiEndpoints,omitempty"`
}

type csiCloudConfigAPIEndpoints struct {
	IaaSAPI string `yaml:"iaasApi"`
}

type csiCloudConfigBlockStorage struct {
	RescanOnResize bool `yaml:"rescanOnResize"`
}

func renderCSICloudConfigManifest(projectID, region, iaasEndpoint string) ([]byte, error) {
	cloudConfig := csiCloudConfig{
		Global: csiCloudConfigGlobal{
			ProjectID: projectID,
			Region:    region,
		},
		BlockStorage: csiCloudConfigBlockStorage{
			RescanOnResize: true,
		},
	}
	if iaasEndpoint != "" {
		cloudConfig.Global.APIEndpoints = &csiCloudConfigAPIEndpoints{
			IaaSAPI: iaasEndpoint,
		}
	}

	cloudConfigBytes, err := yaml.Marshal(&cloudConfig)
	if err != nil {
		return nil, fmt.Errorf("encode CSI cloud config: %w", err)
	}

	manifest := csiCloudConfigManifest{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata: csiCloudConfigMetadata{
			Name:      csiConfigMapName,
			Namespace: csiConfigNamespace,
		},
		Data: map[string]string{
			csiConfigMapKey: string(cloudConfigBytes),
		},
	}

	rendered, err := yaml.Marshal(&manifest)
	if err != nil {
		return nil, fmt.Errorf("encode CSI cloud config manifest: %w", err)
	}

	return rendered, nil
}

func replaceInFile(path string, replacements map[string]string) error {
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file %q: %w", path, err)
	}

	content := string(contentBytes)
	for oldVal, newVal := range replacements {
		content = strings.ReplaceAll(content, oldVal, newVal)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write file %q: %w", path, err)
	}
	return nil
}

func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		targetPath := filepath.Join(dst, relPath)
		if d.IsDir() {
			dirInfo, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(targetPath, dirInfo.Mode())
		}

		return copyFile(path, targetPath)
	})
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return nil
}
