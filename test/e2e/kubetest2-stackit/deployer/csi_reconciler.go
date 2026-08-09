package deployer

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	e2eassets "github.com/stackitcloud/cloud-provider-stackit/test/e2e"
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
	defaultBlockStorageRescanOnResize = true
	kustomizePath                     = "kustomize"

	kustomizeTestDriverName = "kubetest2.csi.stackit.cloud"
	kustomizeTestClassName  = "kubetest2-stackit"
)

type csiInstallConfig struct {
	KubeconfigPath       string
	ProjectID            string
	Region               string
	ServiceAccountJSON   string
	StorageClassType     string
	SnapshotType         string
	ImageName            string
	ImageTag             string
	RescanOnResize       bool
	TestDriverOutputPath string
}

type csiReconciler interface {
	Reconcile(context.Context, *csiInstallConfig) error
}

type kustomizeCSIReconciler struct{}

func newKustomizeCSIReconciler() *kustomizeCSIReconciler {
	return &kustomizeCSIReconciler{}
}

func (r *kustomizeCSIReconciler) Reconcile(ctx context.Context, cfg *csiInstallConfig) error {
	if err := ensureNamespace(ctx, cfg.KubeconfigPath, kubeSystemNamespace); err != nil {
		return err
	}
	if err := upsertCloudConfig(ctx, cfg.KubeconfigPath, cfg); err != nil {
		return err
	}
	if err := upsertCloudSecret(ctx, cfg.KubeconfigPath, cfg); err != nil {
		return err
	}
	if err := r.applyKustomize(ctx, cfg); err != nil {
		return err
	}
	if err := waitForCRDs(ctx, cfg.KubeconfigPath); err != nil {
		return err
	}
	if err := waitForWorkloads(ctx, cfg.KubeconfigPath); err != nil {
		return err
	}
	return writeCSITestDriverConfig(cfg.TestDriverOutputPath)
}

func (r *kustomizeCSIReconciler) applyKustomize(ctx context.Context, cfg *csiInstallConfig) error {
	tmpDir, err := os.MkdirTemp("", "stackit-csi-*")
	if err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := writeKustomizeOverlay(tmpDir, cfg); err != nil {
		return fmt.Errorf("write kustomize overlay: %w", err)
	}

	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-k", tmpDir, "--kubeconfig", cfg.KubeconfigPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	klog.Infof("Applying kustomize overlay from %s", tmpDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply -k: %w", err)
	}
	return nil
}

func writeKustomizeOverlay(tmpDir string, cfg *csiInstallConfig) error {
	if err := copyEmbeddedFS(tmpDir); err != nil {
		return fmt.Errorf("copy embedded fs: %w", err)
	}

	kustomizationPath := filepath.Join(tmpDir, "kustomization.yaml")
	content, err := os.ReadFile(kustomizationPath)
	if err != nil {
		return fmt.Errorf("read kustomization.yaml: %w", err)
	}

	csiPluginDir, err := resolveCSIPluginDir()
	if err != nil {
		return fmt.Errorf("resolve csi-plugin directory: %w", err)
	}

	kustomization := string(content)
	kustomization = strings.ReplaceAll(kustomization, "../../../deploy/csi-plugin", csiPluginDir)
	kustomization = strings.ReplaceAll(kustomization, "IMAGE_NAME_PLACEHOLDER", cfg.ImageName)
	kustomization = strings.ReplaceAll(kustomization, "IMAGE_TAG_PLACEHOLDER", cfg.ImageTag)
	kustomization = strings.ReplaceAll(kustomization, "CSI_DRIVER_NAME_PLACEHOLDER", kustomizeTestDriverName)
	kustomization = strings.ReplaceAll(kustomization, "CSI_CLASS_NAME_PLACEHOLDER", kustomizeTestClassName)

	if err := os.WriteFile(kustomizationPath, []byte(kustomization), 0o644); err != nil {
		return fmt.Errorf("write kustomization.yaml: %w", err)
	}

	return nil
}

func copyEmbeddedFS(tmpDir string) error {
	return copyFSDir(e2eassets.FS, kustomizePath, tmpDir)
}

func resolveCSIPluginDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..", "..", "..", "..")
	return filepath.Abs(filepath.Join(repoRoot, "deploy", "csi-plugin"))
}

func copyFSDir(fsys fs.FS, srcDir, dstDir string) error {
	entries, err := fs.ReadDir(fsys, srcDir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", srcDir, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return err
			}
			if err := copyFSDir(fsys, srcPath, dstPath); err != nil {
				return err
			}
		} else {
			content, err := fs.ReadFile(fsys, srcPath)
			if err != nil {
				return fmt.Errorf("read file %s: %w", srcPath, err)
			}
			if err := os.WriteFile(dstPath, content, 0o644); err != nil {
				return fmt.Errorf("write file %s: %w", dstPath, err)
			}
		}
	}
	return nil
}

func ensureNamespace(ctx context.Context, kubeconfigPath, name string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "create", "namespace", name,
		"--kubeconfig", kubeconfigPath, "--dry-run=client", "-o", "yaml")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("generate namespace manifest: %w", err)
	}

	applyCmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-", "--kubeconfig", kubeconfigPath)
	applyCmd.Stdin = &stdout
	applyCmd.Stdout = os.Stdout
	applyCmd.Stderr = os.Stderr
	if err := applyCmd.Run(); err != nil {
		return fmt.Errorf("apply namespace %q: %w", name, err)
	}
	return nil
}

func upsertCloudConfig(ctx context.Context, kubeconfigPath string, cfg *csiInstallConfig) error {
	content, err := buildCSICloudConfig(cfg)
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "stackit-cloud-config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	manifest := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
data:
  %s: |
%s`, stackitCloudConfigMapName, kubeSystemNamespace, stackitCloudConfigMapKey, indentYAML(content, 4))

	if err := os.WriteFile(tmpFile.Name(), []byte(manifest), 0o600); err != nil {
		return fmt.Errorf("write configmap manifest: %w", err)
	}

	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", tmpFile.Name(), "--kubeconfig", kubeconfigPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("apply configmap %q: %w", stackitCloudConfigMapName, err)
	}
	return nil
}

func upsertCloudSecret(ctx context.Context, kubeconfigPath string, cfg *csiInstallConfig) error {
	tmpFile, err := os.CreateTemp("", "stackit-cloud-secret-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	manifest := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: Opaque
stringData:
  %s: |
%s`, stackitCloudSecretName, kubeSystemNamespace, stackitCloudSecretKey, indentYAML(cfg.ServiceAccountJSON, 4))

	if err := os.WriteFile(tmpFile.Name(), []byte(manifest), 0o600); err != nil {
		return fmt.Errorf("write secret manifest: %w", err)
	}

	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", tmpFile.Name(), "--kubeconfig", kubeconfigPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("apply secret %q: %w", stackitCloudSecretName, err)
	}
	return nil
}

func indentYAML(content string, spaces int) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(content, "\n")
	var result strings.Builder
	for i, line := range lines {
		if i > 0 {
			result.WriteByte('\n')
		}
		if line != "" {
			result.WriteString(indent)
			result.WriteString(line)
		}
	}
	return result.String()
}

func waitForCRDs(ctx context.Context, kubeconfigPath string) error {
	crds := []string{
		"volumesnapshots.snapshot.storage.k8s.io",
		"volumesnapshotclasses.snapshot.storage.k8s.io",
		"volumesnapshotcontents.snapshot.storage.k8s.io",
	}
	for _, crd := range crds {
		klog.Infof("Waiting for CRD %q to become established", crd)
		cmd := exec.CommandContext(ctx, "kubectl", "wait", "--for=condition=Established",
			fmt.Sprintf("crd/%s", crd), "--timeout=5m", "--kubeconfig", kubeconfigPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("wait for CRD %q: %w", crd, err)
		}
	}
	return nil
}

func waitForWorkloads(ctx context.Context, kubeconfigPath string) error {
	workloads := []struct {
		kind      string
		name      string
		namespace string
	}{
		{"deployment", snapshotControllerDeploymentName, kubeSystemNamespace},
		{"deployment", csiControllerDeploymentName, kubeSystemNamespace},
		{"daemonset", csiNodeDaemonSetName, kubeSystemNamespace},
	}
	for _, w := range workloads {
		klog.Infof("Waiting for %s %q in namespace %q to roll out", w.kind, w.name, w.namespace)
		cmd := exec.CommandContext(ctx, "kubectl", "rollout", "status",
			fmt.Sprintf("%s/%s", w.kind, w.name), "-n", w.namespace, "--timeout=5m",
			"--kubeconfig", kubeconfigPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("wait for %s %q rollout: %w", w.kind, w.name, err)
		}
	}
	return nil
}

func buildCSICloudConfig(cfg *csiInstallConfig) (string, error) {
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

func writeCSITestDriverConfig(path string) error {
	content, err := yaml.Marshal(csiTestDriverConfig{
		StorageClass: csiTestClassConfig{
			FromExistingClassName: kustomizeTestClassName,
		},
		SnapshotClass: csiTestClassConfig{
			FromExistingClassName: kustomizeTestClassName,
		},
		DriverInfo: csiTestDriverInfo{
			Name: kustomizeTestDriverName,
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
