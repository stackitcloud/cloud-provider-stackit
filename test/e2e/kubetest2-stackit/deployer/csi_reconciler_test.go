package deployer

import (
	"os"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRenderCSIAssetsUsesConfiguredDriverAndClasses(t *testing.T) {
	reconciler := newManifestCSIReconciler()
	cfg := csiInstallConfig{
		DriverName:           "custom.csi.stackit.cloud",
		StorageClassName:     "fast-stackit",
		StorageClassType:     "storage_fast_perf2",
		SnapshotClassName:    "stackit-snapshots",
		SnapshotType:         "backup",
		ImageName:            "example.invalid/stackit-csi-plugin",
		ImageTag:             "test-tag",
		RescanOnResize:       true,
		TestDriverOutputPath: t.TempDir() + "/csi-testdriver.yaml",
	}

	assets, err := reconciler.renderAssets(cfg)
	if err != nil {
		t.Fatalf("renderAssets() returned error: %v", err)
	}

	storageClass := findObjectByKind(t, assets.csi, "StorageClass")
	if storageClass.GetName() != cfg.StorageClassName {
		t.Fatalf("unexpected StorageClass name: %q", storageClass.GetName())
	}
	provisioner, _, err := unstructured.NestedString(storageClass.Object, "provisioner")
	if err != nil {
		t.Fatalf("failed reading StorageClass provisioner: %v", err)
	}
	if provisioner != cfg.DriverName {
		t.Fatalf("unexpected StorageClass provisioner: %q", provisioner)
	}

	snapshotClass := findObjectByKind(t, assets.csi, "VolumeSnapshotClass")
	if snapshotClass.GetName() != cfg.SnapshotClassName {
		t.Fatalf("unexpected VolumeSnapshotClass name: %q", snapshotClass.GetName())
	}
	driver, _, err := unstructured.NestedString(snapshotClass.Object, "driver")
	if err != nil {
		t.Fatalf("failed reading VolumeSnapshotClass driver: %v", err)
	}
	if driver != cfg.DriverName {
		t.Fatalf("unexpected VolumeSnapshotClass driver: %q", driver)
	}

	csiDriver := findObjectByKind(t, assets.csi, "CSIDriver")
	if csiDriver.GetName() != cfg.DriverName {
		t.Fatalf("unexpected CSIDriver name: %q", csiDriver.GetName())
	}
}

func TestRenderCSIAssetsOverridesPluginImageAndNodePaths(t *testing.T) {
	reconciler := newManifestCSIReconciler()
	cfg := csiInstallConfig{
		DriverName:           "custom.csi.stackit.cloud",
		StorageClassName:     defaultCSIStorageClassName,
		StorageClassType:     defaultCSIStorageClassType,
		SnapshotClassName:    defaultCSISnapshotClassName,
		SnapshotType:         defaultCSISnapshotType,
		ImageName:            "example.invalid/stackit-csi-plugin",
		ImageTag:             "test-tag",
		RescanOnResize:       true,
		TestDriverOutputPath: t.TempDir() + "/csi-testdriver.yaml",
	}

	assets, err := reconciler.renderAssets(cfg)
	if err != nil {
		t.Fatalf("renderAssets() returned error: %v", err)
	}

	deployment := findObjectByName(t, assets.csi, "Deployment", csiControllerDeploymentName)
	if image := containerImage(t, deployment, csiStackitPluginContainerName); image != cfg.pluginImage() {
		t.Fatalf("unexpected controller plugin image: %q", image)
	}
	if policy := containerImagePullPolicy(t, deployment, csiStackitPluginContainerName); policy != "Always" {
		t.Fatalf("unexpected controller plugin imagePullPolicy: %q", policy)
	}
	if !strings.Contains(strings.Join(containerArgs(t, deployment, csiStackitPluginContainerName), " "), "--driver-name="+cfg.DriverName) {
		t.Fatalf("controller plugin args do not include custom driver name")
	}
	if !strings.Contains(strings.Join(containerArgs(t, deployment, "liveness-probe"), " "), "--http-endpoint=:9810") {
		t.Fatalf("controller liveness-probe args do not include the overridden health port")
	}
	if !deploymentUsesHostNetwork(t, deployment) {
		t.Fatalf("controller deployment should use hostNetwork")
	}
	if dnsPolicy := podDNSPolicy(t, deployment); dnsPolicy != "ClusterFirstWithHostNet" {
		t.Fatalf("unexpected controller deployment dnsPolicy: %q", dnsPolicy)
	}
	if containerHasPorts(t, deployment, csiStackitPluginContainerName) {
		t.Fatalf("controller plugin container should not declare ports when hostNetwork is enabled")
	}

	daemonSet := findObjectByName(t, assets.csi, "DaemonSet", csiNodeDaemonSetName)
	if image := containerImage(t, daemonSet, csiStackitPluginContainerName); image != cfg.pluginImage() {
		t.Fatalf("unexpected node plugin image: %q", image)
	}
	if policy := containerImagePullPolicy(t, daemonSet, csiStackitPluginContainerName); policy != "Always" {
		t.Fatalf("unexpected node plugin imagePullPolicy: %q", policy)
	}
	if !strings.Contains(strings.Join(containerArgs(t, daemonSet, csiStackitPluginContainerName), " "), "--driver-name="+cfg.DriverName) {
		t.Fatalf("node plugin args do not include custom driver name")
	}
	if env := containerEnvValue(t, daemonSet, nodeDriverRegistrarContainerName, "DRIVER_REG_SOCK_PATH"); env != "/var/lib/kubelet/plugins/"+cfg.DriverName+"/csi.sock" {
		t.Fatalf("unexpected registrar socket path: %q", env)
	}
	if !strings.Contains(strings.Join(containerArgs(t, daemonSet, "liveness-probe"), " "), "--http-endpoint=:9809") {
		t.Fatalf("nodeplugin liveness-probe args do not include the overridden health port")
	}
	if containerHasPorts(t, daemonSet, csiStackitPluginContainerName) {
		t.Fatalf("node plugin container should not declare ports when hostNetwork is enabled")
	}
	socketVolume := findNamedVolume(t, daemonSet, csiSocketVolumeName)
	hostPath, _, err := unstructured.NestedString(socketVolume, "hostPath", "path")
	if err != nil {
		t.Fatalf("failed reading socket-dir hostPath: %v", err)
	}
	if hostPath != "/var/lib/kubelet/plugins/"+cfg.DriverName {
		t.Fatalf("unexpected node plugin host path: %q", hostPath)
	}
}

func TestBuildCSICloudConfigUsesNestedShape(t *testing.T) {
	content, err := buildCSICloudConfig(csiInstallConfig{
		ProjectID:      "project-id",
		Region:         "eu01",
		RescanOnResize: true,
	})
	if err != nil {
		t.Fatalf("buildCSICloudConfig() returned error: %v", err)
	}
	if !strings.Contains(content, "global:\n") || !strings.Contains(content, "projectId: project-id") {
		t.Fatalf("expected nested global projectId in cloud config, got:\n%s", content)
	}
	if !strings.Contains(content, "blockStorage:\n") || !strings.Contains(content, "rescanOnResize: true") {
		t.Fatalf("expected blockStorage.rescanOnResize in cloud config, got:\n%s", content)
	}
}

func TestWriteCSITestDriverConfigUsesLowercaseCapabilityKeys(t *testing.T) {
	path := t.TempDir() + "/csi-testdriver.yaml"

	err := writeCSITestDriverConfig(path, csiInstallConfig{
		DriverName:        "custom.csi.stackit.cloud",
		StorageClassName:  "fast-stackit",
		SnapshotClassName: "stackit-snapshots",
	})
	if err != nil {
		t.Fatalf("writeCSITestDriverConfig() returned error: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed reading generated CSI testdriver config: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "block: true") {
		t.Fatalf("expected lowercase block capability key in generated testdriver config, got:\n%s", text)
	}
	if strings.Contains(text, "Block: true") {
		t.Fatalf("unexpected uppercase block capability key in generated testdriver config, got:\n%s", text)
	}
}

func TestRenderCSIAssetsForcesSnapshotControllerInClusterAPIAccess(t *testing.T) {
	reconciler := newManifestCSIReconciler()
	cfg := csiInstallConfig{
		DriverName:           defaultCSIDriverName,
		StorageClassName:     defaultCSIStorageClassName,
		StorageClassType:     defaultCSIStorageClassType,
		SnapshotClassName:    defaultCSISnapshotClassName,
		SnapshotType:         defaultCSISnapshotType,
		ImageName:            defaultCSIImageName,
		ImageTag:             defaultCSIImageTag,
		RescanOnResize:       true,
		TestDriverOutputPath: t.TempDir() + "/csi-testdriver.yaml",
	}

	assets, err := reconciler.renderAssets(cfg)
	if err != nil {
		t.Fatalf("renderAssets() returned error: %v", err)
	}

	deployment := findObjectByName(t, assets.snapshotController, "Deployment", snapshotControllerDeploymentName)
	hostNetwork, found, err := unstructured.NestedBool(deployment.Object, "spec", "template", "spec", "hostNetwork")
	if err != nil {
		t.Fatalf("failed reading snapshot-controller hostNetwork: %v", err)
	}
	if !found || !hostNetwork {
		t.Fatalf("expected snapshot-controller deployment to use hostNetwork")
	}
	dnsPolicy, found, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "dnsPolicy")
	if err != nil {
		t.Fatalf("failed reading snapshot-controller dnsPolicy: %v", err)
	}
	if !found || dnsPolicy != "ClusterFirstWithHostNet" {
		t.Fatalf("unexpected snapshot-controller dnsPolicy: %q", dnsPolicy)
	}
}

func findObjectByKind(t *testing.T, objects []*unstructured.Unstructured, kind string) *unstructured.Unstructured {
	t.Helper()
	for _, object := range objects {
		if object.GetKind() == kind {
			return object
		}
	}
	t.Fatalf("did not find kind %q", kind)
	return nil
}

func findObjectByName(t *testing.T, objects []*unstructured.Unstructured, kind, name string) *unstructured.Unstructured {
	t.Helper()
	for _, object := range objects {
		if object.GetKind() == kind && object.GetName() == name {
			return object
		}
	}
	t.Fatalf("did not find %s %q", kind, name)
	return nil
}

func containerImage(t *testing.T, object *unstructured.Unstructured, name string) string {
	t.Helper()
	container := findNamedContainer(t, object, name)
	image, _ := container["image"].(string)
	return image
}

func containerArgs(t *testing.T, object *unstructured.Unstructured, name string) []string {
	t.Helper()
	container := findNamedContainer(t, object, name)
	args, err := stringSliceFromAny(container["args"])
	if err != nil {
		t.Fatalf("failed reading args for container %q: %v", name, err)
	}
	return args
}

func containerImagePullPolicy(t *testing.T, object *unstructured.Unstructured, name string) string {
	t.Helper()
	container := findNamedContainer(t, object, name)
	policy, _ := container["imagePullPolicy"].(string)
	return policy
}

func containerHasPorts(t *testing.T, object *unstructured.Unstructured, name string) bool {
	t.Helper()
	container := findNamedContainer(t, object, name)
	ports, found := container["ports"]
	if !found || ports == nil {
		return false
	}
	items, ok := ports.([]any)
	if !ok {
		t.Fatalf("unexpected ports type for container %q: %T", name, ports)
	}
	return len(items) > 0
}

func containerEnvValue(t *testing.T, object *unstructured.Unstructured, containerName, envName string) string {
	t.Helper()
	container := findNamedContainer(t, object, containerName)
	env, err := nestedSliceMap(container, "env")
	if err != nil {
		t.Fatalf("failed reading env for container %q: %v", containerName, err)
	}
	for _, item := range env {
		if item["name"] == envName {
			value, _ := item["value"].(string)
			return value
		}
	}
	t.Fatalf("did not find env %q in container %q", envName, containerName)
	return ""
}

func deploymentUsesHostNetwork(t *testing.T, object *unstructured.Unstructured) bool {
	t.Helper()
	hostNetwork, found, err := unstructured.NestedBool(object.Object, "spec", "template", "spec", "hostNetwork")
	if err != nil {
		t.Fatalf("failed reading hostNetwork: %v", err)
	}
	return found && hostNetwork
}

func podDNSPolicy(t *testing.T, object *unstructured.Unstructured) string {
	t.Helper()
	dnsPolicy, found, err := unstructured.NestedString(object.Object, "spec", "template", "spec", "dnsPolicy")
	if err != nil {
		t.Fatalf("failed reading dnsPolicy: %v", err)
	}
	if !found {
		t.Fatalf("object %s/%s is missing dnsPolicy", object.GetKind(), object.GetName())
	}
	return dnsPolicy
}

func findNamedContainer(t *testing.T, object *unstructured.Unstructured, name string) map[string]any {
	t.Helper()
	containers, found, err := unstructured.NestedSlice(object.Object, "spec", "template", "spec", "containers")
	if err != nil {
		t.Fatalf("failed reading containers: %v", err)
	}
	if !found {
		t.Fatalf("object %s/%s is missing containers", object.GetKind(), object.GetName())
	}
	for _, item := range containers {
		container, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if container["name"] == name {
			return container
		}
	}
	t.Fatalf("did not find container %q", name)
	return nil
}

func findNamedVolume(t *testing.T, object *unstructured.Unstructured, name string) map[string]any {
	t.Helper()
	volumes, found, err := unstructured.NestedSlice(object.Object, "spec", "template", "spec", "volumes")
	if err != nil {
		t.Fatalf("failed reading volumes: %v", err)
	}
	if !found {
		t.Fatalf("object %s/%s is missing volumes", object.GetKind(), object.GetName())
	}
	for _, item := range volumes {
		volume, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if volume["name"] == name {
			return volume
		}
	}
	t.Fatalf("did not find volume %q", name)
	return nil
}
