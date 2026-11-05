package kubetest2

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/stackiterrors"
	resourcemanager "github.com/stackitcloud/stackit-sdk-go/services/resourcemanager/v0api"
	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"
	"k8s.io/klog/v2"
)

func (d *Deployer) Init() error {
	klog.Infof("Initializing STACKIT kubetest2 deployer")

	if err := d.validate(); err != nil {
		return err
	}
	if err := d.loadEnvironment(); err != nil {
		return err
	}

	if err := d.initializeBootstrapClients(); err != nil {
		return err
	}

	klog.Infof("STACKIT kubetest2 deployer initialized successfully")

	return nil
}

func (d *Deployer) Up() error {
	klog.Infof("Starting cluster up flow for cluster=%q", d.clusterName())

	ctx := context.Background()
	if err := d.ensureProject(ctx); err != nil {
		return fmt.Errorf("bootstrap project: %w", err)
	}
	if err := d.ensureServiceAccount(ctx); err != nil {
		return fmt.Errorf("bootstrap service account: %w", err)
	}
	if err := d.ensureCluster(ctx); err != nil {
		return fmt.Errorf("bootstrap cluster: %w", err)
	}
	if err := d.ensureCSI(ctx); err != nil {
		return fmt.Errorf("bootstrap csi: %w", err)
	}

	klog.Infof("Cluster up flow completed successfully for cluster=%q", d.clusterName())

	return nil
}

func (d *Deployer) Down() error {
	ctx := context.Background()

	klog.Infof("Starting cluster down flow for cluster=%q", d.clusterName())

	project, err := d.findManagedProject(ctx)
	if err != nil {
		return err
	}
	if project == nil {
		klog.Infof("Managed project for run_id=%q is already absent", d.options.RunID())
		return nil
	}

	d.projectID = project.ProjectID
	if project.LifecycleState != resourcemanager.LIFECYCLESTATE_DELETING {
		if err := d.projectClient.DeleteProject(ctx, project.ProjectID); err != nil {
			if !stackiterrors.IsNotFound(err) {
				return fmt.Errorf("delete STACKIT project %q: %w", project.ProjectID, err)
			}
			klog.Infof("Project=%q already absent, treating delete as success", project.ProjectID)
			return nil
		}
	}

	if err := d.projectClient.WaitForProjectDeleted(ctx, project.ProjectID); err != nil {
		return fmt.Errorf("wait for STACKIT project %q deletion: %w", project.ProjectID, err)
	}

	klog.Infof("Cluster down flow completed successfully for project=%q", project.ProjectID)

	return nil
}

func (d *Deployer) IsUp() (bool, error) {
	ctx := context.Background()
	project, err := d.findManagedProject(ctx)
	if err != nil {
		return false, err
	}
	if project == nil || project.LifecycleState == resourcemanager.LIFECYCLESTATE_DELETING {
		klog.Infof("Managed project for run_id=%q not found or terminating during IsUp check", d.options.RunID())
		return false, nil
	}

	d.projectID = project.ProjectID
	serviceAccountKey, ok, err := d.readCachedChildServiceAccountKey()
	if err != nil {
		return false, fmt.Errorf("read cached child service-account key %q: %w", d.serviceAccountKeyPath, err)
	}
	if !ok {
		return false, fmt.Errorf(
			"managed project %q exists but child service-account key cache %q is missing",
			project.ProjectID,
			d.serviceAccountKeyPath,
		)
	}
	if err := d.initializeSKEClient(serviceAccountKey); err != nil {
		return false, err
	}

	klog.Infof("Checking cluster state for cluster=%q", d.clusterName())
	cluster, err := d.skeClient.GetCluster(ctx, d.projectID, d.region, d.clusterName())
	if err != nil {
		if stackiterrors.IsNotFound(err) {
			klog.Infof("Cluster=%q not found during IsUp check", d.clusterName())
			return false, nil
		}
		return false, fmt.Errorf("get SKE cluster %q: %w", d.clusterName(), err)
	}

	if cluster.Status == nil || cluster.Status.Aggregated == nil {
		klog.Infof("Cluster=%q has no aggregated status yet", d.clusterName())
		return false, nil
	}

	state := cluster.Status.GetAggregated()
	klog.Infof("Cluster=%q current aggregated state=%q", d.clusterName(), state)
	return state == ske.CLUSTERSTATUSSTATE_STATE_HEALTHY || state == ske.CLUSTERSTATUSSTATE_STATE_HIBERNATED, nil
}

func (d *Deployer) Kubeconfig() (string, error) {
	if d.kubeconfigPath == "" {
		d.kubeconfigPath = filepath.Join(d.options.RunDir(), "kubeconfig")
	}
	klog.Infof("Returning kubeconfig path %q", d.kubeconfigPath)
	return d.kubeconfigPath, nil
}

const defaultTestPackageURL = "https://dl.k8s.io"

func acquireGinkgoTestPackage(ctx context.Context, kubernetesVersion, runDir string) error {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("get user cache directory: %w", err)
	}
	return acquireTestPackageFromURL(ctx, defaultTestPackageURL, kubernetesVersion, runDir, cacheDir)
}

func acquireTestPackageFromURL(ctx context.Context, baseURL, kubernetesVersion, runDir, cacheDir string) error {
	pkgVersion := "v" + strings.TrimPrefix(strings.TrimSpace(kubernetesVersion), "v")
	klog.Infof("Acquiring Kubernetes e2e test package version=%q into run_dir=%q", pkgVersion, runDir)

	if err := os.MkdirAll(runDir, 0o750); err != nil {
		return fmt.Errorf("create run directory %q: %w", runDir, err)
	}
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return fmt.Errorf("create cache directory %q: %w", cacheDir, err)
	}

	releaseTar := fmt.Sprintf("kubernetes-test-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	tarURL := fmt.Sprintf("%s/release/%s/%s", strings.TrimRight(baseURL, "/"), pkgVersion, releaseTar)
	tarPath := filepath.Join(cacheDir, fmt.Sprintf("%s-%s", pkgVersion, releaseTar))

	if err := ensureVerifiedDownload(ctx, tarURL, tarPath, 0o600); err != nil {
		return fmt.Errorf("download test package tarball: %w", err)
	}
	if err := extractTestBinaries(tarPath, runDir); err != nil {
		return fmt.Errorf("extract test package binaries: %w", err)
	}

	kubectlURL := fmt.Sprintf("%s/release/%s/bin/%s/%s/kubectl", strings.TrimRight(baseURL, "/"), pkgVersion, runtime.GOOS, runtime.GOARCH)
	kubectlPath := filepath.Join(runDir, "kubectl")
	if err := ensureVerifiedDownload(ctx, kubectlURL, kubectlPath, 0o700); err != nil {
		return fmt.Errorf("download kubectl: %w", err)
	}

	return nil
}

func ensureVerifiedDownload(ctx context.Context, fileURL, destPath string, mode os.FileMode) error {
	if _, err := os.Stat(destPath); err == nil {
		if err := verifyFileSHA256(ctx, destPath, fileURL+".sha256"); err == nil {
			return nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned status %d", fileURL, resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(destPath), ".download-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := verifyFileSHA256(ctx, tmpPath, fileURL+".sha256"); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}
	return os.Rename(tmpPath, destPath)
}

func verifyFileSHA256(ctx context.Context, filePath, shaURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shaURL, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned status %d", shaURL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	expected := strings.TrimSpace(string(body))

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("sha256 mismatch for %q: got %s, want %s", filePath, actual, expected)
	}
	return nil
}

func extractTestBinaries(tarPath, runDir string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gzr.Close() }()

	targets := map[string]string{
		"kubernetes/test/bin/e2e.test": filepath.Join(runDir, "e2e.test"),
		"kubernetes/test/bin/ginkgo":   filepath.Join(runDir, "ginkgo"),
	}
	extracted := make(map[string]bool, len(targets))

	tr := tar.NewReader(gzr)
	for len(extracted) < len(targets) {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		dest, ok := targets[hdr.Name]
		if !ok {
			continue
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil { //nolint:gosec // archive is SHA256-verified from dl.k8s.io
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		extracted[hdr.Name] = true
	}

	for name := range targets {
		if !extracted[name] {
			return fmt.Errorf("binary %q not found in %q", name, tarPath)
		}
	}
	return nil
}
