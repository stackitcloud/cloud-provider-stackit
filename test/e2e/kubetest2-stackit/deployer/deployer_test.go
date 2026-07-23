package deployer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	client "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/client"
	clientmock "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/client/mock"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	oapierror "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"
	"go.uber.org/mock/gomock"
	"sigs.k8s.io/kubetest2/pkg/types"
)

type fakeOptions struct {
	runID  string
	runDir string
}

func (f fakeOptions) HelpRequested() bool       { return false }
func (f fakeOptions) ShouldBuild() bool         { return false }
func (f fakeOptions) ShouldUp() bool            { return false }
func (f fakeOptions) ShouldDown() bool          { return false }
func (f fakeOptions) ShouldTest() bool          { return false }
func (f fakeOptions) SkipTestJUnitReport() bool { return false }
func (f fakeOptions) RunID() string             { return f.runID }
func (f fakeOptions) RunDir() string            { return f.runDir }
func (f fakeOptions) RundirInArtifacts() bool   { return false }
func (f fakeOptions) PostTestCmd() []string     { return nil }
func (f fakeOptions) PreTestCmd() []string      { return nil }

func TestNewDefaults(t *testing.T) {
	opts := fakeOptions{runID: "1234567890abcdef", runDir: t.TempDir()}

	d := NewDeployer(opts)
	if d.ClusterName != "kt2-1234567" {
		t.Fatalf("unexpected default cluster name: %q", d.ClusterName)
	}
	if d.NodepoolName != defaultNodepoolName {
		t.Fatalf("unexpected default nodepool name: %q", d.NodepoolName)
	}
	if d.Nodes != defaultNodes {
		t.Fatalf("unexpected default nodes: %d", d.Nodes)
	}
	if d.VolumeSize != defaultVolumeSize {
		t.Fatalf("unexpected default volume size: %d", d.VolumeSize)
	}
	if d.KubeconfigExpirationSeconds != defaultKubeconfigExpiration {
		t.Fatalf("unexpected kubeconfig expiration: %d", d.KubeconfigExpirationSeconds)
	}
	if d.CSIDriverName != defaultCSIDriverName {
		t.Fatalf("unexpected default CSI driver name: %q", d.CSIDriverName)
	}
	if d.CSIStorageClassName != defaultCSIStorageClassName {
		t.Fatalf("unexpected default CSI storage class name: %q", d.CSIStorageClassName)
	}
	if d.CSISnapshotClassName != defaultCSISnapshotClassName {
		t.Fatalf("unexpected default CSI snapshot class name: %q", d.CSISnapshotClassName)
	}
	if d.CSIImageName != defaultCSIImageName || d.CSIImageTag != defaultCSIImageTag {
		t.Fatalf("unexpected default CSI image override: %s:%s", d.CSIImageName, d.CSIImageTag)
	}

	_, fs := New(opts)
	for _, name := range []string{
		"project-id",
		"cluster-name",
		"kubeconfig-expiration-seconds",
		"csi-driver-name",
		"csi-storage-class-name",
		"csi-snapshot-class-name",
		"csi-image-name",
		"csi-image-tag",
	} {
		if fs.Lookup(name) == nil {
			t.Fatalf("expected flag %q to be registered", name)
		}
	}
}

func TestInitRequiresServiceAccountEnv(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockFactory := clientmock.NewMockFactory(ctrl)

	d := newConfiguredDeployer(t)
	d.clientFactory = mockFactory
	d.lookupEnv = func(string) (string, bool) {
		return "", false
	}

	err := d.Init()
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(types.IncorrectUsage); !ok {
		t.Fatalf("expected IncorrectUsage, got %T", err)
	}
	if !strings.Contains(err.Error(), stackitServiceAccountEnvVar) {
		t.Fatalf("expected error to mention %s, got %q", stackitServiceAccountEnvVar, err.Error())
	}
}

func TestInitBuildsSKEClientWithServiceAccount(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockFactory := clientmock.NewMockFactory(ctrl)
	mockSKE := clientmock.NewMockSKEClient(ctrl)

	d := newConfiguredDeployer(t)
	d.clientFactory = mockFactory
	d.lookupEnv = func(string) (string, bool) {
		return `{"credentials":{"private_key":"key"}}`, true
	}

	mockFactory.EXPECT().
		SKE(gomock.Any()).
		DoAndReturn(func(options []sdkconfig.ConfigurationOption) (client.SKEClient, error) {
			if len(options) != 1 {
				t.Fatalf("expected one sdk option, got %d", len(options))
			}
			return mockSKE, nil
		})

	if err := d.Init(); err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}
	if d.skeClient == nil {
		t.Fatal("expected SKE client to be initialized")
	}
}

func TestPersistClusterStateRoundTrip(t *testing.T) {
	d := newConfiguredDeployer(t)
	payload := d.buildClusterPayload()
	d.currentClusterName = "kt2-test123"
	d.lastRequestPayload = &payload
	d.lastObservedStatus = string(ske.CLUSTERSTATUSSTATE_STATE_HEALTHY)

	if err := d.persistClusterState(); err != nil {
		t.Fatalf("persistClusterState() returned error: %v", err)
	}

	state, err := d.loadClusterState()
	if err != nil {
		t.Fatalf("loadClusterState() returned error: %v", err)
	}
	if state.ClusterName != "kt2-test123" {
		t.Fatalf("unexpected cluster name: %q", state.ClusterName)
	}
	if state.ProjectID != d.ProjectID {
		t.Fatalf("unexpected project ID: %q", state.ProjectID)
	}
	if state.Region != d.Region {
		t.Fatalf("unexpected region: %q", state.Region)
	}
	if state.LastObservedStatus != "STATE_HEALTHY" {
		t.Fatalf("unexpected status: %q", state.LastObservedStatus)
	}
	if state.RequestPayload == nil || state.RequestPayload.Kubernetes.Version != "1.32.1" {
		t.Fatalf("unexpected request payload: %#v", state.RequestPayload)
	}
}

func TestLoadClusterStateMissing(t *testing.T) {
	d := newConfiguredDeployer(t)

	_, err := d.loadClusterState()
	if !errors.Is(err, errClusterStateNotFound) {
		t.Fatalf("expected errClusterStateNotFound, got %v", err)
	}
}

func TestLoadClusterStateMalformed(t *testing.T) {
	d := newConfiguredDeployer(t)
	if err := d.initPaths(); err != nil {
		t.Fatalf("initPaths() returned error: %v", err)
	}
	if err := os.WriteFile(d.clusterStatePath, []byte("{"), 0o600); err != nil {
		t.Fatalf("failed writing malformed state: %v", err)
	}

	_, err := d.loadClusterState()
	if err == nil || !strings.Contains(err.Error(), "failed to parse persisted cluster state") {
		t.Fatalf("expected malformed state error, got %v", err)
	}
}

func TestUpRequiresFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(*Deployer)
		contains string
	}{
		{
			name:     "project id",
			mutate:   func(d *Deployer) { d.ProjectID = "" },
			contains: "--project-id is required",
		},
		{
			name:     "region",
			mutate:   func(d *Deployer) { d.Region = "" },
			contains: "--region is required",
		},
		{
			name:     "kubernetes version",
			mutate:   func(d *Deployer) { d.KubernetesVersion = "" },
			contains: "--kubernetes-version is required",
		},
		{
			name:     "node count",
			mutate:   func(d *Deployer) { d.Nodes = 0 },
			contains: "--nodes must be at least 1",
		},
		{
			name:     "kubeconfig expiration",
			mutate:   func(d *Deployer) { d.KubeconfigExpirationSeconds = 42 },
			contains: "--kubeconfig-expiration-seconds must be between",
		},
		{
			name:     "cluster name too long",
			mutate:   func(d *Deployer) { d.ClusterName = "kt2-name-too-long" },
			contains: "--cluster-name must be at most 11 characters",
		},
		{
			name:     "csi driver name",
			mutate:   func(d *Deployer) { d.CSIDriverName = "" },
			contains: "--csi-driver-name must not be empty",
		},
		{
			name:     "csi image tag",
			mutate:   func(d *Deployer) { d.CSIImageTag = "" },
			contains: "--csi-image-tag must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newConfiguredDeployer(t)
			tt.mutate(d)

			err := d.Up()
			if err == nil {
				t.Fatal("expected error")
			}
			if _, ok := err.(types.IncorrectUsage); !ok {
				t.Fatalf("expected IncorrectUsage, got %T", err)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error to contain %q, got %q", tt.contains, err.Error())
			}
		})
	}
}

func TestProviderOptionValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		options  *ske.ProviderOptions
		contains string
	}{
		{
			name:     "kubernetes version",
			options:  baseProviderOptions("1.31.6", "eu01-1", "g1.2", "flatcar", "3033.2.4", "storage_premium_perf0"),
			contains: `kubernetes version "1.32.1" is not available`,
		},
		{
			name:     "availability zone",
			options:  baseProviderOptions("1.32.1", "eu01-2", "g1.2", "flatcar", "3033.2.4", "storage_premium_perf0"),
			contains: `availability zone "eu01-1" is not available`,
		},
		{
			name:     "machine type",
			options:  baseProviderOptions("1.32.1", "eu01-1", "g1.4", "flatcar", "3033.2.4", "storage_premium_perf0"),
			contains: `machine type "g1.2" is not available`,
		},
		{
			name:     "image version",
			options:  baseProviderOptions("1.32.1", "eu01-1", "g1.2", "flatcar", "9999.0.0", "storage_premium_perf0"),
			contains: `node image "flatcar" version "3033.2.4" is not available`,
		},
		{
			name:     "volume type",
			options:  baseProviderOptions("1.32.1", "eu01-1", "g1.2", "flatcar", "3033.2.4", "storage_premium_perf4"),
			contains: `volume type "storage_premium_perf0" is not available`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockSKE := clientmock.NewMockSKEClient(ctrl)

			d := newConfiguredDeployer(t)
			d.skeClient = mockSKE

			mockSKE.EXPECT().ListProviderOptions(gomock.Any()).Return(tt.options, nil)

			err := d.Up()
			if err == nil {
				t.Fatal("expected error")
			}
			if _, ok := err.(types.IncorrectUsage); !ok {
				t.Fatalf("expected IncorrectUsage, got %T", err)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error to contain %q, got %q", tt.contains, err.Error())
			}
		})
	}
}

func TestUpBuildsPayloadAndWritesArtifacts(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockSKE := clientmock.NewMockSKEClient(ctrl)

	d := newConfiguredDeployer(t)
	reconciler := d.csiReconciler.(*recordingCSIReconciler)
	d.skeClient = mockSKE

	mockSKE.EXPECT().ListProviderOptions(gomock.Any()).Return(
		baseProviderOptions("1.32.1", "eu01-1", "g1.2", "flatcar", "3033.2.4", "storage_premium_perf0"),
		nil,
	)

	mockSKE.EXPECT().
		CreateOrUpdateCluster(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateOrUpdateClusterPayload{})).
		DoAndReturn(func(_ context.Context, _ string, payload ske.CreateOrUpdateClusterPayload) (*ske.Cluster, error) {
			if payload.Kubernetes.Version != "1.32.1" {
				t.Fatalf("unexpected kubernetes version: %q", payload.Kubernetes.Version)
			}
			if len(payload.Nodepools) != 1 {
				t.Fatalf("expected one nodepool, got %d", len(payload.Nodepools))
			}

			nodepool := payload.Nodepools[0]
			if nodepool.Name != "np-default" {
				t.Fatalf("unexpected nodepool name: %q", nodepool.Name)
			}
			if nodepool.Minimum != 2 || nodepool.Maximum != 2 {
				t.Fatalf("unexpected node bounds: min=%d max=%d", nodepool.Minimum, nodepool.Maximum)
			}
			if !nodepool.GetAllowSystemComponents() {
				t.Fatal("expected allowSystemComponents to be true")
			}
			if nodepool.Machine.Type != "g1.2" {
				t.Fatalf("unexpected machine type: %q", nodepool.Machine.Type)
			}
			if nodepool.Machine.Image.Name != "flatcar" || nodepool.Machine.Image.Version != "3033.2.4" {
				t.Fatalf("unexpected image: %#v", nodepool.Machine.Image)
			}
			if len(nodepool.AvailabilityZones) != 1 || nodepool.AvailabilityZones[0] != "eu01-1" {
				t.Fatalf("unexpected availability zones: %#v", nodepool.AvailabilityZones)
			}
			if nodepool.Volume.Size != 150 {
				t.Fatalf("unexpected volume size: %d", nodepool.Volume.Size)
			}
			if nodepool.Volume.GetType() != "storage_premium_perf0" {
				t.Fatalf("unexpected volume type: %q", nodepool.Volume.GetType())
			}

			return newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_CREATING), nil
		})

	mockSKE.EXPECT().
		WaitClusterReady(gomock.Any(), "kt2-test123").
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_HEALTHY), nil)

	mockSKE.EXPECT().
		CreateKubeconfig(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateKubeconfigPayload{})).
		DoAndReturn(func(_ context.Context, _ string, payload ske.CreateKubeconfigPayload) (*ske.Kubeconfig, error) {
			if payload.GetExpirationSeconds() != "7200" {
				t.Fatalf("unexpected kubeconfig expiration: %q", payload.GetExpirationSeconds())
			}
			content := "apiVersion: v1\nclusters: []\n"
			return &ske.Kubeconfig{Kubeconfig: &content}, nil
		})

	if err := d.Up(); err != nil {
		t.Fatalf("Up() returned error: %v", err)
	}

	kubeconfigPath, err := d.Kubeconfig()
	if err != nil {
		t.Fatalf("Kubeconfig() returned error: %v", err)
	}
	if !filepath.IsAbs(kubeconfigPath) {
		t.Fatalf("expected absolute kubeconfig path, got %q", kubeconfigPath)
	}

	kubeconfigContent, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		t.Fatalf("failed reading kubeconfig: %v", err)
	}
	if !strings.Contains(string(kubeconfigContent), "apiVersion: v1") {
		t.Fatalf("unexpected kubeconfig content: %q", string(kubeconfigContent))
	}

	clusterStateContent, err := os.ReadFile(d.clusterStatePath)
	if err != nil {
		t.Fatalf("failed reading persisted cluster state: %v", err)
	}
	if !strings.Contains(string(clusterStateContent), `"projectId": "project-id"`) {
		t.Fatalf("persisted state missing project ID: %s", clusterStateContent)
	}
	if !strings.Contains(string(clusterStateContent), `"region": "eu01"`) {
		t.Fatalf("persisted state missing region: %s", clusterStateContent)
	}
	if len(reconciler.calls) != 1 {
		t.Fatalf("expected one CSI reconcile call, got %d", len(reconciler.calls))
	}
	if reconciler.calls[0].DriverName != d.CSIDriverName {
		t.Fatalf("unexpected CSI driver name: %q", reconciler.calls[0].DriverName)
	}
	testDriverContent, err := os.ReadFile(d.csiTestDriverPath)
	if err != nil {
		t.Fatalf("failed reading generated CSI testdriver config: %v", err)
	}
	if !strings.Contains(string(testDriverContent), "FromExistingClassName: "+d.CSIStorageClassName) {
		t.Fatalf("generated testdriver config missing storage class: %s", testDriverContent)
	}
	if !strings.Contains(string(testDriverContent), "Name: "+d.CSIDriverName) {
		t.Fatalf("generated testdriver config missing driver name: %s", testDriverContent)
	}
}

func TestUpReusesHealthyClusterFromPersistedState(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockSKE := clientmock.NewMockSKEClient(ctrl)

	d := newConfiguredDeployer(t)
	reconciler := d.csiReconciler.(*recordingCSIReconciler)
	d.skeClient = mockSKE
	writeClusterState(t, d, clusterState{
		ClusterName:        "kt2-test123",
		ProjectID:          "project-id",
		Region:             "eu01",
		LastObservedStatus: "STATE_HEALTHY",
	})
	if err := os.WriteFile(filepath.Join(d.Options.RunDir(), kubeconfigFileName), []byte("stale"), 0o600); err != nil {
		t.Fatalf("failed writing stale kubeconfig: %v", err)
	}

	mockSKE.EXPECT().ListProviderOptions(gomock.Any()).Return(
		baseProviderOptions("1.32.1", "eu01-1", "g1.2", "flatcar", "3033.2.4", "storage_premium_perf0"),
		nil,
	)
	mockSKE.EXPECT().
		CreateOrUpdateCluster(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateOrUpdateClusterPayload{})).
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_HEALTHY), nil)
	mockSKE.EXPECT().
		WaitClusterReady(gomock.Any(), "kt2-test123").
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_HEALTHY), nil)
	mockSKE.EXPECT().
		CreateKubeconfig(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateKubeconfigPayload{})).
		DoAndReturn(func(_ context.Context, _ string, payload ske.CreateKubeconfigPayload) (*ske.Kubeconfig, error) {
			if payload.GetExpirationSeconds() != "7200" {
				t.Fatalf("unexpected kubeconfig expiration: %q", payload.GetExpirationSeconds())
			}
			content := "apiVersion: v1\nclusters:\n- reused\n"
			return &ske.Kubeconfig{Kubeconfig: &content}, nil
		})

	if err := d.Up(); err != nil {
		t.Fatalf("Up() returned error: %v", err)
	}

	content, err := os.ReadFile(d.kubeconfigPath)
	if err != nil {
		t.Fatalf("failed reading kubeconfig: %v", err)
	}
	if !strings.Contains(string(content), "reused") {
		t.Fatalf("expected fresh kubeconfig content, got %q", string(content))
	}
	if len(reconciler.calls) != 1 {
		t.Fatalf("expected one CSI reconcile call on reuse, got %d", len(reconciler.calls))
	}
}

func TestUpReusesHibernatedClusterFromPersistedState(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockSKE := clientmock.NewMockSKEClient(ctrl)

	d := newConfiguredDeployer(t)
	d.skeClient = mockSKE
	writeClusterState(t, d, clusterState{
		ClusterName: "kt2-test123",
		ProjectID:   "project-id",
		Region:      "eu01",
	})

	mockSKE.EXPECT().ListProviderOptions(gomock.Any()).Return(
		baseProviderOptions("1.32.1", "eu01-1", "g1.2", "flatcar", "3033.2.4", "storage_premium_perf0"),
		nil,
	)
	mockSKE.EXPECT().
		CreateOrUpdateCluster(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateOrUpdateClusterPayload{})).
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_HIBERNATED), nil)
	mockSKE.EXPECT().
		WaitClusterReady(gomock.Any(), "kt2-test123").
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_HIBERNATED), nil)
	mockSKE.EXPECT().
		CreateKubeconfig(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateKubeconfigPayload{})).
		Return(&ske.Kubeconfig{Kubeconfig: stringPtr("apiVersion: v1\n")}, nil)

	if err := d.Up(); err != nil {
		t.Fatalf("Up() returned error: %v", err)
	}
}

func TestUpIgnoresStalePersistedStateAndCreatesCluster(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockSKE := clientmock.NewMockSKEClient(ctrl)

	d := newConfiguredDeployer(t)
	d.skeClient = mockSKE
	writeClusterState(t, d, clusterState{
		ClusterName: "kt2-test123",
		ProjectID:   "project-id",
		Region:      "eu01",
	})

	mockSKE.EXPECT().ListProviderOptions(gomock.Any()).Return(
		baseProviderOptions("1.32.1", "eu01-1", "g1.2", "flatcar", "3033.2.4", "storage_premium_perf0"),
		nil,
	)
	mockSKE.EXPECT().
		CreateOrUpdateCluster(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateOrUpdateClusterPayload{})).
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_CREATING), nil)
	mockSKE.EXPECT().
		WaitClusterReady(gomock.Any(), "kt2-test123").
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_HEALTHY), nil)
	mockSKE.EXPECT().
		CreateKubeconfig(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateKubeconfigPayload{})).
		Return(&ske.Kubeconfig{Kubeconfig: stringPtr("apiVersion: v1\nclusters:\n- recreated\n")}, nil)

	if err := d.Up(); err != nil {
		t.Fatalf("Up() returned error: %v", err)
	}
}

func TestUpIsIdempotentWithPersistedState(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockSKE := clientmock.NewMockSKEClient(ctrl)

	d := newConfiguredDeployer(t)
	d.skeClient = mockSKE
	writeClusterState(t, d, clusterState{
		ClusterName: "kt2-test123",
		ProjectID:   "project-id",
		Region:      "eu01",
	})

	mockSKE.EXPECT().ListProviderOptions(gomock.Any()).Return(
		baseProviderOptions("1.32.1", "eu01-1", "g1.2", "flatcar", "3033.2.4", "storage_premium_perf0"),
		nil,
	)
	mockSKE.EXPECT().
		CreateOrUpdateCluster(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateOrUpdateClusterPayload{})).
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_CREATING), nil)
	mockSKE.EXPECT().
		WaitClusterReady(gomock.Any(), "kt2-test123").
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_HEALTHY), nil)
	mockSKE.EXPECT().
		CreateKubeconfig(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateKubeconfigPayload{})).
		Return(&ske.Kubeconfig{Kubeconfig: stringPtr("apiVersion: v1\nclusters:\n- converged\n")}, nil)

	if err := d.Up(); err != nil {
		t.Fatalf("Up() returned error: %v", err)
	}
}

func TestUpFailsWhenCSIReconciliationFailsAfterClusterReady(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockSKE := clientmock.NewMockSKEClient(ctrl)

	d := newConfiguredDeployer(t)
	reconciler := d.csiReconciler.(*recordingCSIReconciler)
	reconciler.err = errors.New("csi install failed")
	reconciler.writeTestDriver = false
	d.skeClient = mockSKE

	mockSKE.EXPECT().ListProviderOptions(gomock.Any()).Return(
		baseProviderOptions("1.32.1", "eu01-1", "g1.2", "flatcar", "3033.2.4", "storage_premium_perf0"),
		nil,
	)
	mockSKE.EXPECT().
		CreateOrUpdateCluster(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateOrUpdateClusterPayload{})).
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_CREATING), nil)
	mockSKE.EXPECT().
		WaitClusterReady(gomock.Any(), "kt2-test123").
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_HEALTHY), nil)
	mockSKE.EXPECT().
		CreateKubeconfig(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateKubeconfigPayload{})).
		Return(&ske.Kubeconfig{Kubeconfig: stringPtr("apiVersion: v1\nclusters:\n- ready\n")}, nil)

	err := d.Up()
	if err == nil || !strings.Contains(err.Error(), "csi install failed") {
		t.Fatalf("expected CSI reconciliation error, got %v", err)
	}
	if len(reconciler.calls) != 1 {
		t.Fatalf("expected one CSI reconcile call, got %d", len(reconciler.calls))
	}
	if _, statErr := os.Stat(d.clusterStatePath); statErr != nil {
		t.Fatalf("expected cluster state to remain after CSI failure, got %v", statErr)
	}
}

func TestIsUp(t *testing.T) {
	tests := []struct {
		name   string
		status ske.ClusterStatusState
		up     bool
	}{
		{name: "healthy", status: ske.CLUSTERSTATUSSTATE_STATE_HEALTHY, up: true},
		{name: "hibernated", status: ske.CLUSTERSTATUSSTATE_STATE_HIBERNATED, up: true},
		{name: "creating", status: ske.CLUSTERSTATUSSTATE_STATE_CREATING, up: false},
		{name: "deleting", status: ske.CLUSTERSTATUSSTATE_STATE_DELETING, up: false},
		{name: "unhealthy", status: ske.CLUSTERSTATUSSTATE_STATE_UNHEALTHY, up: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockSKE := clientmock.NewMockSKEClient(ctrl)

			d := newConfiguredDeployer(t)
			d.skeClient = mockSKE

			mockSKE.EXPECT().
				GetCluster(gomock.Any(), "kt2-test123").
				Return(newCluster("kt2-test123", tt.status), nil)

			up, err := d.IsUp()
			if err != nil {
				t.Fatalf("IsUp() returned error: %v", err)
			}
			if up != tt.up {
				t.Fatalf("expected up=%t, got %t", tt.up, up)
			}
		})
	}
}

func TestDownRequiresPersistedState(t *testing.T) {
	d := newConfiguredDeployer(t)

	err := d.Down()
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(types.IncorrectUsage); !ok {
		t.Fatalf("expected IncorrectUsage, got %T", err)
	}
	if !strings.Contains(err.Error(), clusterStateFileName) {
		t.Fatalf("expected error to mention persisted state file, got %q", err.Error())
	}
}

func TestDownDeleteAndWait(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockSKE := clientmock.NewMockSKEClient(ctrl)

	d := newConfiguredDeployer(t)
	d.skeClient = mockSKE
	d.currentClusterName = "kt2-test123"

	mockSKE.EXPECT().DeleteCluster(gomock.Any(), "kt2-test123").Return(nil)
	mockSKE.EXPECT().WaitClusterDeleted(gomock.Any(), "kt2-test123").Return(nil)

	if err := d.Down(); err != nil {
		t.Fatalf("Down() returned error: %v", err)
	}
	if d.currentClusterName != "" {
		t.Fatalf("expected current cluster name to be cleared, got %q", d.currentClusterName)
	}
}

func TestDownDeleteAndWaitWithPersistedState(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockSKE := clientmock.NewMockSKEClient(ctrl)

	d := newConfiguredDeployer(t)
	d.skeClient = mockSKE
	writeClusterState(t, d, clusterState{
		ClusterName: "kt2-test123",
		ProjectID:   "project-id",
		Region:      "eu01",
	})

	mockSKE.EXPECT().DeleteCluster(gomock.Any(), "kt2-test123").Return(nil)
	mockSKE.EXPECT().WaitClusterDeleted(gomock.Any(), "kt2-test123").Return(nil)

	if err := d.Down(); err != nil {
		t.Fatalf("Down() returned error: %v", err)
	}
	if _, err := os.Stat(d.clusterStatePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected persisted state to be removed, got %v", err)
	}
}

func TestDownAlreadyGoneWithPersistedState(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockSKE := clientmock.NewMockSKEClient(ctrl)

	d := newConfiguredDeployer(t)
	d.skeClient = mockSKE
	writeClusterState(t, d, clusterState{
		ClusterName: "kt2-test123",
		ProjectID:   "project-id",
		Region:      "eu01",
	})

	mockSKE.EXPECT().
		DeleteCluster(gomock.Any(), "kt2-test123").
		Return(notFoundError())
	mockSKE.EXPECT().WaitClusterDeleted(gomock.Any(), "kt2-test123").Return(nil)

	if err := d.Down(); err != nil {
		t.Fatalf("Down() returned error: %v", err)
	}
}

func TestDownWaitError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockSKE := clientmock.NewMockSKEClient(ctrl)

	d := newConfiguredDeployer(t)
	d.skeClient = mockSKE
	d.currentClusterName = "kt2-test123"

	mockSKE.EXPECT().DeleteCluster(gomock.Any(), "kt2-test123").Return(nil)
	mockSKE.EXPECT().WaitClusterDeleted(gomock.Any(), "kt2-test123").Return(errors.New("timeout"))

	err := d.Down()
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestRunIDReuseAcrossDeployers(t *testing.T) {
	runDir := t.TempDir()
	opts := fakeOptions{
		runID:  "1234567890abcdef",
		runDir: runDir,
	}

	createCtrl := gomock.NewController(t)
	createSKE := clientmock.NewMockSKEClient(createCtrl)
	upDeployer := newConfiguredDeployerWithOptions(opts)
	upDeployer.skeClient = createSKE

	createSKE.EXPECT().ListProviderOptions(gomock.Any()).Return(
		baseProviderOptions("1.32.1", "eu01-1", "g1.2", "flatcar", "3033.2.4", "storage_premium_perf0"),
		nil,
	)
	createSKE.EXPECT().
		CreateOrUpdateCluster(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateOrUpdateClusterPayload{})).
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_CREATING), nil)
	createSKE.EXPECT().
		WaitClusterReady(gomock.Any(), "kt2-test123").
		Return(newCluster("kt2-test123", ske.CLUSTERSTATUSSTATE_STATE_HEALTHY), nil)
	createSKE.EXPECT().
		CreateKubeconfig(gomock.Any(), "kt2-test123", gomock.AssignableToTypeOf(ske.CreateKubeconfigPayload{})).
		Return(&ske.Kubeconfig{Kubeconfig: stringPtr("apiVersion: v1\n")}, nil)

	if err := upDeployer.Up(); err != nil {
		t.Fatalf("Up() returned error: %v", err)
	}

	deleteCtrl := gomock.NewController(t)
	deleteSKE := clientmock.NewMockSKEClient(deleteCtrl)
	downDeployer := newConfiguredDeployerWithOptions(opts)
	downDeployer.skeClient = deleteSKE

	if err := upDeployer.initPaths(); err != nil {
		t.Fatalf("initPaths() returned error: %v", err)
	}
	if err := downDeployer.initPaths(); err != nil {
		t.Fatalf("initPaths() returned error: %v", err)
	}
	if upDeployer.clusterStatePath != downDeployer.clusterStatePath {
		t.Fatalf("expected same cluster state path, got %q and %q", upDeployer.clusterStatePath, downDeployer.clusterStatePath)
	}

	deleteSKE.EXPECT().DeleteCluster(gomock.Any(), "kt2-test123").Return(nil)
	deleteSKE.EXPECT().WaitClusterDeleted(gomock.Any(), "kt2-test123").Return(nil)

	if err := downDeployer.Down(); err != nil {
		t.Fatalf("Down() returned error: %v", err)
	}
}

func newConfiguredDeployer(t *testing.T) *Deployer {
	t.Helper()
	return newConfiguredDeployerWithOptions(fakeOptions{
		runID:  "1234567890abcdef",
		runDir: t.TempDir(),
	})
}

func newConfiguredDeployerWithOptions(opts fakeOptions) *Deployer {
	d := NewDeployer(opts)
	d.ProjectID = "project-id"
	d.Region = "eu01"
	d.ClusterName = "kt2-test123"
	d.KubernetesVersion = "1.32.1"
	d.AvailabilityZone = "eu01-1"
	d.MachineType = "g1.2"
	d.NodeImageName = "flatcar"
	d.NodeImageVersion = "3033.2.4"
	d.NodepoolName = "np-default"
	d.Nodes = 2
	d.VolumeSize = 150
	d.VolumeType = "storage_premium_perf0"
	d.KubeconfigExpirationSeconds = 7200
	d.csiReconciler = &recordingCSIReconciler{writeTestDriver: true}
	d.lookupEnv = func(string) (string, bool) {
		return `{"credentials":{"private_key":"key"}}`, true
	}
	return d
}

type recordingCSIReconciler struct {
	calls           []csiInstallConfig
	err             error
	writeTestDriver bool
}

func (r *recordingCSIReconciler) Reconcile(_ context.Context, cfg csiInstallConfig) error {
	r.calls = append(r.calls, cfg)
	if r.err != nil {
		return r.err
	}
	if r.writeTestDriver {
		return writeCSITestDriverConfig(cfg.TestDriverOutputPath, cfg)
	}
	return nil
}

func writeClusterState(t *testing.T, d *Deployer, state clusterState) {
	t.Helper()

	if err := d.initPaths(); err != nil {
		t.Fatalf("initPaths() returned error: %v", err)
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("failed marshalling cluster state: %v", err)
	}
	if err := os.WriteFile(d.clusterStatePath, content, 0o600); err != nil {
		t.Fatalf("failed writing cluster state: %v", err)
	}
}

func baseProviderOptions(version, az, machineType, imageName, imageVersion, volumeType string) *ske.ProviderOptions {
	return &ske.ProviderOptions{
		KubernetesVersions: []ske.KubernetesVersion{
			{Version: &version},
		},
		AvailabilityZones: []ske.AvailabilityZone{
			{Name: &az},
		},
		MachineTypes: []ske.MachineType{
			{Name: &machineType},
		},
		MachineImages: []ske.MachineImage{
			{
				Name: &imageName,
				Versions: []ske.MachineImageVersion{
					{Version: &imageVersion},
				},
			},
		},
		VolumeTypes: []ske.VolumeType{
			{Name: &volumeType},
		},
	}
}

func newCluster(name string, state ske.ClusterStatusState) *ske.Cluster {
	return &ske.Cluster{
		Name: &name,
		Status: &ske.ClusterStatus{
			Aggregated: &state,
		},
		Kubernetes: *ske.NewKubernetes("1.32.1"),
		Nodepools:  []ske.Nodepool{},
	}
}

func notFoundError() error {
	return &oapierror.GenericOpenAPIError{StatusCode: http.StatusNotFound}
}

func stringPtr(value string) *string {
	return &value
}
