package deployer

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/octago/sflags/gen/gpflag"
	"github.com/spf13/pflag"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/client"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/stackiterrors"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/version"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"
	"k8s.io/klog/v2"
	"sigs.k8s.io/kubetest2/pkg/types"
)

const (
	Name                        = "stackit"
	stackitServiceAccountEnvVar = "STACKIT_SERVICE_ACCOUNT"
	maxClusterNameLength        = 11
	defaultNodepoolName         = "default"
	defaultNodes                = 1
	defaultVolumeSize           = 100
	defaultKubeconfigExpiration = 6 * 60 * 60 // 6 hours
	defaultCSIDriverName        = "kubetest2.csi.stackit.cloud"
	defaultCSIStorageClassName  = "kubetest2-stackit"
	defaultCSIStorageClassType  = "storage_premium_perf4"
	defaultCSISnapshotClassName = "kubetest2-stackit"
	defaultCSISnapshotType      = "snapshot"
	defaultCSIImageName         = "ghcr.io/stackitcloud/cloud-provider-stackit/stackit-csi-plugin"
	defaultCSIImageTag          = "1.35.5"
	clusterStateFileName        = "cluster-state.json"
	kubeconfigFileName          = "kubeconfig"
	kubeconfigExpirationMin     = 600
	kubeconfigExpirationMax     = 15552000
)

type factoryBuilder func(region, projectID string) client.Factory

type Deployer struct {
	Options types.Options `flag:"-"`

	ProjectID                   string `flag:"~project-id" desc:"STACKIT project ID for the SKE cluster."`
	Region                      string `flag:"~region" desc:"STACKIT region for the SKE cluster."`
	KubernetesVersion           string `flag:"~kubernetes-version" desc:"Kubernetes version to provision."`
	AvailabilityZone            string `flag:"~availability-zone" desc:"Availability zone for the nodepool."`
	MachineType                 string `flag:"~machine-type" desc:"Machine type for the nodepool."`
	NodeImageName               string `flag:"~node-image-name" desc:"Node image name for the nodepool."`
	NodeImageVersion            string `flag:"~node-image-version" desc:"Node image version for the nodepool."`
	ClusterName                 string `flag:"~cluster-name" desc:"Cluster name. Defaults to kt2-<first12(run-id)>."`
	NodepoolName                string `flag:"~nodepool-name" desc:"Nodepool name."`
	Nodes                       int    `flag:"~nodes" desc:"Fixed node count for the single v1 nodepool."`
	VolumeSize                  int    `flag:"~volume-size" desc:"Root volume size in GiB for the nodepool."`
	VolumeType                  string `flag:"~volume-type" desc:"Optional root volume type for the nodepool."`
	KubeconfigExpirationSeconds int    `flag:"~kubeconfig-expiration-seconds" desc:"Expiration for the admin kubeconfig in seconds."`
	CSIDriverName               string `flag:"~csi-driver-name" desc:"CSI driver identity to deploy into the cluster."`
	CSIStorageClassName         string `flag:"~csi-storage-class-name" desc:"Primary StorageClass name to reconcile for the STACKIT CSI driver."`
	CSIStorageClassType         string `flag:"~csi-storage-class-type" desc:"STACKIT block storage performance class for the reconciled StorageClass."`
	CSISnapshotClassName        string `flag:"~csi-snapshot-class-name" desc:"VolumeSnapshotClass name to reconcile for the STACKIT CSI driver."`
	CSISnapshotType             string `flag:"~csi-snapshot-type" desc:"STACKIT snapshot type to configure for the reconciled VolumeSnapshotClass."`
	CSIImageName                string `flag:"~csi-image-name" desc:"Container image repository for the STACKIT CSI plugin workload containers."`
	CSIImageTag                 string `flag:"~csi-image-tag" desc:"Container image tag for the STACKIT CSI plugin workload containers."`

	newFactory         factoryBuilder
	lookupEnv          func(string) (string, bool)
	csiReconciler      csiReconciler
	clientFactory      client.Factory
	skeClient          client.SKEClient
	runDir             string
	kubeconfigPath     string
	csiTestDriverPath  string
	clusterStatePath   string
	currentClusterName string
	lastObservedStatus string
	lastRequestPayload *ske.CreateOrUpdateClusterPayload
}

type clusterState struct {
	ClusterName        string                            `json:"clusterName"`
	ProjectID          string                            `json:"projectId"`
	Region             string                            `json:"region"`
	RequestPayload     *ske.CreateOrUpdateClusterPayload `json:"requestPayload,omitempty"`
	LastObservedStatus string                            `json:"lastObservedStatus,omitempty"`
}

var errClusterStateNotFound = errors.New("persisted cluster state not found")

var _ types.NewDeployer = New
var _ types.Deployer = &Deployer{}
var _ types.DeployerWithInit = &Deployer{}
var _ types.DeployerWithKubeconfig = &Deployer{}
var _ types.DeployerWithProvider = &Deployer{}
var _ types.DeployerWithVersion = &Deployer{}

func New(opts types.Options) (types.Deployer, *pflag.FlagSet) {
	d := NewDeployer(opts)

	klog.InitFlags(nil)
	fs := bindFlags(d)
	fs.AddGoFlagSet(flag.CommandLine)
	return d, fs
}

func NewDeployer(opts types.Options) *Deployer {
	return &Deployer{
		Options:                     opts,
		ClusterName:                 defaultClusterName(opts.RunID()),
		NodepoolName:                defaultNodepoolName,
		Nodes:                       defaultNodes,
		VolumeSize:                  defaultVolumeSize,
		KubeconfigExpirationSeconds: defaultKubeconfigExpiration,
		CSIDriverName:               defaultCSIDriverName,
		CSIStorageClassName:         defaultCSIStorageClassName,
		CSIStorageClassType:         defaultCSIStorageClassType,
		CSISnapshotClassName:        defaultCSISnapshotClassName,
		CSISnapshotType:             defaultCSISnapshotType,
		CSIImageName:                defaultCSIImageName,
		CSIImageTag:                 defaultCSIImageTag,
		newFactory:                  client.New,
		lookupEnv:                   os.LookupEnv,
		csiReconciler:               newManifestCSIReconciler(),
	}
}

func bindFlags(d *Deployer) *pflag.FlagSet {
	flags, err := gpflag.Parse(d)
	if err != nil {
		klog.Fatalf("unable to generate flags from deployer: %v", err)
	}

	return flags
}

func (d *Deployer) Provider() string {
	return Name
}

func (d *Deployer) Version() string {
	return version.Version
}

func (d *Deployer) Init() error {
	klog.Infof("Initializing %s deployer", Name)

	if err := d.initPaths(); err != nil {
		return err
	}
	if d.skeClient != nil {
		return nil
	}

	if d.clientFactory == nil {
		d.clientFactory = d.newFactory(d.Region, d.ProjectID)
	}

	skeOptions, err := d.skeOptions()
	if err != nil {
		return err
	}

	klog.Infof("Creating STACKIT SKE client for project %q in region %q", d.ProjectID, d.Region)
	d.skeClient, err = d.clientFactory.SKE(skeOptions)
	return err
}

func (d *Deployer) initPaths() error {
	if d.runDir == "" {
		d.runDir = d.Options.RunDir()
	}

	var err error
	d.kubeconfigPath, err = filepath.Abs(filepath.Join(d.runDir, kubeconfigFileName))
	if err != nil {
		return err
	}

	d.clusterStatePath, err = filepath.Abs(filepath.Join(d.runDir, clusterStateFileName))
	if err != nil {
		return err
	}

	d.csiTestDriverPath, err = filepath.Abs(filepath.Join(d.runDir, csiTestDriverFileName))
	if err != nil {
		return err
	}

	return nil
}

func (d *Deployer) Build() error {
	return nil
}

func (d *Deployer) Up() error {
	if err := d.validateUpFlags(); err != nil {
		return err
	}

	if _, err := d.restoreClusterStateIfPresent(); err != nil {
		return err
	}
	if err := d.Init(); err != nil {
		return err
	}

	ctx := context.Background()
	klog.Infof("Fetching SKE provider options for region %q", d.Region)
	providerOptions, err := d.skeClient.ListProviderOptions(ctx)
	if err != nil {
		return err
	}
	if err := d.validateProviderOptions(providerOptions); err != nil {
		return err
	}
	klog.Infof("Provider options validated for kubernetes version %q, zone %q, machine type %q", d.KubernetesVersion, d.AvailabilityZone, d.MachineType)

	payload := d.buildClusterPayload()
	d.currentClusterName = d.lookupClusterName()
	d.lastRequestPayload = &payload

	klog.Infof("Reconciling SKE cluster %q", d.currentClusterName)
	cluster, err := d.skeClient.CreateOrUpdateCluster(ctx, d.currentClusterName, payload)
	d.observeCluster(cluster)
	if err != nil {
		return err
	}

	klog.Infof("Waiting for cluster %q to become ready", d.currentClusterName)
	cluster, err = d.skeClient.WaitClusterReady(ctx, d.currentClusterName)
	d.observeCluster(cluster)
	if err != nil {
		return err
	}
	klog.Infof("Cluster %q is ready with state %q", d.ClusterName, d.lastObservedStatus)

	if err := d.refreshKubeconfig(ctx, d.currentClusterName); err != nil {
		return err
	}
	if err := d.persistClusterState(); err != nil {
		return err
	}
	if err := d.reconcileCSI(ctx); err != nil {
		return err
	}
	return nil
}

func (d *Deployer) Down() error {
	if d.currentClusterName == "" {
		foundState, err := d.restoreClusterStateIfPresent()
		if err != nil {
			return err
		}
		if !foundState {
			if err := d.initPaths(); err != nil {
				return err
			}
			return types.NewIncorrectUsage(fmt.Sprintf(
				"--down requires persisted cluster state in %q for run-id %q",
				d.clusterStatePath,
				d.Options.RunID(),
			))
		}
	}
	if err := d.Init(); err != nil {
		return err
	}

	ctx := context.Background()
	klog.Infof("Deleting SKE cluster %q", d.currentClusterName)
	if err := d.skeClient.DeleteCluster(ctx, d.currentClusterName); err != nil && !stackiterrors.IsNotFound(err) {
		return err
	}
	klog.Infof("Waiting for cluster %q to be deleted", d.currentClusterName)
	if err := d.skeClient.WaitClusterDeleted(ctx, d.currentClusterName); err != nil {
		return err
	}

	d.lastObservedStatus = string(ske.CLUSTERSTATUSSTATE_STATE_DELETING)
	d.currentClusterName = ""
	if err := d.removeClusterState(); err != nil {
		return err
	}
	klog.Infof("Cluster deletion completed")
	return nil
}

func (d *Deployer) IsUp() (bool, error) {
	if _, err := d.restoreClusterStateIfPresent(); err != nil {
		return false, err
	}

	clusterName := d.lookupClusterName()
	if clusterName == "" {
		return false, nil
	}
	if err := d.Init(); err != nil {
		return false, err
	}

	cluster, err := d.skeClient.GetCluster(context.Background(), clusterName)
	if err != nil {
		if stackiterrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	d.observeCluster(cluster)
	return clusterReady(cluster), nil
}

func (d *Deployer) DumpClusterLogs() error {
	return nil
}

func (d *Deployer) Kubeconfig() (string, error) {
	if d.kubeconfigPath == "" {
		if err := d.initPaths(); err != nil {
			return "", err
		}
	}
	if d.kubeconfigPath == "" {
		return "", errors.New("kubeconfig has not been created yet")
	}
	if _, err := os.Stat(d.kubeconfigPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("kubeconfig has not been created yet")
		}
		return "", err
	}

	return d.kubeconfigPath, nil
}

func (d *Deployer) skeOptions() ([]sdkconfig.ConfigurationOption, error) {
	serviceAccount, err := d.stackitServiceAccount()
	if err != nil {
		return nil, err
	}
	return []sdkconfig.ConfigurationOption{
		sdkconfig.WithServiceAccountKey(serviceAccount),
	}, nil
}

func (d *Deployer) validateUpFlags() error {
	required := map[string]string{
		"project-id":         d.ProjectID,
		"region":             d.Region,
		"kubernetes-version": d.KubernetesVersion,
		"availability-zone":  d.AvailabilityZone,
		"machine-type":       d.MachineType,
		"node-image-name":    d.NodeImageName,
		"node-image-version": d.NodeImageVersion,
	}
	for flagName, value := range required {
		if strings.TrimSpace(value) == "" {
			return types.NewIncorrectUsage(fmt.Sprintf("--%s is required", flagName))
		}
	}

	if strings.TrimSpace(d.ClusterName) == "" {
		return types.NewIncorrectUsage("--cluster-name must not be empty")
	}
	if len([]rune(d.ClusterName)) > maxClusterNameLength {
		return types.NewIncorrectUsage(fmt.Sprintf("--cluster-name must be at most %d characters", maxClusterNameLength))
	}
	if strings.TrimSpace(d.NodepoolName) == "" {
		return types.NewIncorrectUsage("--nodepool-name must not be empty")
	}
	if d.Nodes < 1 {
		return types.NewIncorrectUsage("--nodes must be at least 1")
	}
	if d.VolumeSize < 1 {
		return types.NewIncorrectUsage("--volume-size must be at least 1")
	}
	if d.KubeconfigExpirationSeconds < kubeconfigExpirationMin || d.KubeconfigExpirationSeconds > kubeconfigExpirationMax {
		return types.NewIncorrectUsage(fmt.Sprintf(
			"--kubeconfig-expiration-seconds must be between %d and %d",
			kubeconfigExpirationMin,
			kubeconfigExpirationMax,
		))
	}
	csiRequired := map[string]string{
		"csi-driver-name":         d.CSIDriverName,
		"csi-storage-class-name":  d.CSIStorageClassName,
		"csi-storage-class-type":  d.CSIStorageClassType,
		"csi-snapshot-class-name": d.CSISnapshotClassName,
		"csi-snapshot-type":       d.CSISnapshotType,
		"csi-image-name":          d.CSIImageName,
		"csi-image-tag":           d.CSIImageTag,
	}
	for flagName, value := range csiRequired {
		if strings.TrimSpace(value) == "" {
			return types.NewIncorrectUsage(fmt.Sprintf("--%s must not be empty", flagName))
		}
	}

	return nil
}

func (d *Deployer) validateProviderOptions(providerOptions *ske.ProviderOptions) error {
	if providerOptions == nil {
		return errors.New("received empty provider options from SKE")
	}

	if !containsKubernetesVersion(providerOptions.KubernetesVersions, d.KubernetesVersion) {
		return types.NewIncorrectUsage(fmt.Sprintf(
			"kubernetes version %q is not available in region %q",
			d.KubernetesVersion,
			d.Region,
		))
	}
	if !containsAvailabilityZone(providerOptions.AvailabilityZones, d.AvailabilityZone) {
		return types.NewIncorrectUsage(fmt.Sprintf(
			"availability zone %q is not available in region %q",
			d.AvailabilityZone,
			d.Region,
		))
	}
	if !containsMachineType(providerOptions.MachineTypes, d.MachineType) {
		return types.NewIncorrectUsage(fmt.Sprintf(
			"machine type %q is not available in region %q",
			d.MachineType,
			d.Region,
		))
	}
	if !containsMachineImageVersion(providerOptions.MachineImages, d.NodeImageName, d.NodeImageVersion) {
		return types.NewIncorrectUsage(fmt.Sprintf(
			"node image %q version %q is not available in region %q",
			d.NodeImageName,
			d.NodeImageVersion,
			d.Region,
		))
	}
	if d.VolumeType != "" && !containsVolumeType(providerOptions.VolumeTypes, d.VolumeType) {
		return types.NewIncorrectUsage(fmt.Sprintf(
			"volume type %q is not available in region %q",
			d.VolumeType,
			d.Region,
		))
	}

	return nil
}

func (d *Deployer) buildClusterPayload() ske.CreateOrUpdateClusterPayload {
	image := ske.NewImage(d.NodeImageName, d.NodeImageVersion)
	machine := ske.NewMachine(*image, d.MachineType)
	volume := ske.NewVolume(int32(d.VolumeSize))
	if d.VolumeType != "" {
		volume.SetType(d.VolumeType)
	}

	nodepool := ske.NewNodepool(
		[]string{d.AvailabilityZone},
		*machine,
		int32(d.Nodes),
		int32(d.Nodes),
		d.NodepoolName,
		*volume,
	)
	nodepool.SetAllowSystemComponents(true)

	return *ske.NewCreateOrUpdateClusterPayload(
		*ske.NewKubernetes(d.KubernetesVersion),
		[]ske.Nodepool{*nodepool},
	)
}

func (d *Deployer) persistClusterState() error {
	if err := d.initPaths(); err != nil {
		return err
	}

	state := clusterState{
		ClusterName:        d.lookupClusterName(),
		ProjectID:          d.ProjectID,
		Region:             d.Region,
		RequestPayload:     d.lastRequestPayload,
		LastObservedStatus: d.lastObservedStatus,
	}

	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(d.clusterStatePath, content, 0o600)
}

func (d *Deployer) loadClusterState() (*clusterState, error) {
	if err := d.initPaths(); err != nil {
		return nil, err
	}

	content, err := os.ReadFile(d.clusterStatePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errClusterStateNotFound
		}
		return nil, err
	}

	var state clusterState
	if err := json.Unmarshal(content, &state); err != nil {
		return nil, fmt.Errorf("failed to parse persisted cluster state %q: %w", d.clusterStatePath, err)
	}
	if strings.TrimSpace(state.ClusterName) == "" {
		return nil, fmt.Errorf("persisted cluster state %q is missing clusterName", d.clusterStatePath)
	}
	if strings.TrimSpace(state.ProjectID) == "" {
		return nil, fmt.Errorf("persisted cluster state %q is missing projectId", d.clusterStatePath)
	}
	if strings.TrimSpace(state.Region) == "" {
		return nil, fmt.Errorf("persisted cluster state %q is missing region", d.clusterStatePath)
	}

	return &state, nil
}

func (d *Deployer) restoreClusterStateIfPresent() (bool, error) {
	if d.currentClusterName != "" {
		return true, nil
	}

	state, err := d.loadClusterState()
	if err != nil {
		if errors.Is(err, errClusterStateNotFound) {
			return false, nil
		}
		return false, err
	}

	d.ClusterName = state.ClusterName
	d.ProjectID = state.ProjectID
	d.Region = state.Region
	d.currentClusterName = state.ClusterName
	d.lastRequestPayload = state.RequestPayload
	d.lastObservedStatus = state.LastObservedStatus
	return true, nil
}

func (d *Deployer) removeClusterState() error {
	if err := d.initPaths(); err != nil {
		return err
	}

	if err := os.Remove(d.clusterStatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (d *Deployer) refreshKubeconfig(ctx context.Context, clusterName string) error {
	kubeconfigPayload := ske.NewCreateKubeconfigPayload()
	kubeconfigPayload.SetExpirationSeconds(strconv.Itoa(d.KubeconfigExpirationSeconds))
	klog.Infof("Creating admin kubeconfig for cluster %q", clusterName)
	kubeconfig, err := d.skeClient.CreateKubeconfig(ctx, clusterName, *kubeconfigPayload)
	if err != nil {
		return err
	}
	if kubeconfig == nil || kubeconfig.Kubeconfig == nil {
		return errors.New("received empty kubeconfig payload from SKE")
	}

	if err := os.WriteFile(d.kubeconfigPath, []byte(*kubeconfig.Kubeconfig), 0o600); err != nil {
		return err
	}
	klog.Infof("Wrote kubeconfig for cluster %q to %q", clusterName, d.kubeconfigPath)
	return nil
}

func (d *Deployer) stackitServiceAccount() (string, error) {
	lookupEnv := d.lookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}

	serviceAccount, ok := lookupEnv(stackitServiceAccountEnvVar)
	if !ok || strings.TrimSpace(serviceAccount) == "" {
		klog.Errorf("Missing required environment variable %s", stackitServiceAccountEnvVar)
		return "", types.NewIncorrectUsage(fmt.Sprintf("--%s requires %s to be set", Name, stackitServiceAccountEnvVar))
	}

	klog.Infof("Using STACKIT service account from %s", stackitServiceAccountEnvVar)
	return serviceAccount, nil
}

func (d *Deployer) reconcileCSI(ctx context.Context) error {
	serviceAccount, err := d.stackitServiceAccount()
	if err != nil {
		return err
	}
	if d.csiReconciler == nil {
		return errors.New("CSI reconciler is not configured")
	}

	klog.Infof("Reconciling STACKIT CSI stack into cluster %q", d.lookupClusterName())
	return d.csiReconciler.Reconcile(ctx, csiInstallConfig{
		KubeconfigPath:       d.kubeconfigPath,
		ProjectID:            d.ProjectID,
		Region:               d.Region,
		ServiceAccountJSON:   serviceAccount,
		DriverName:           d.CSIDriverName,
		StorageClassName:     d.CSIStorageClassName,
		StorageClassType:     d.CSIStorageClassType,
		SnapshotClassName:    d.CSISnapshotClassName,
		SnapshotType:         d.CSISnapshotType,
		ImageName:            d.CSIImageName,
		ImageTag:             d.CSIImageTag,
		RescanOnResize:       defaultBlockStorageRescanOnResize,
		TestDriverOutputPath: d.csiTestDriverPath,
	})
}

func (d *Deployer) lookupClusterName() string {
	if d.currentClusterName != "" {
		return d.currentClusterName
	}

	return d.ClusterName
}

func (d *Deployer) observeCluster(cluster *ske.Cluster) {
	if cluster == nil {
		return
	}

	d.lastObservedStatus = clusterStatus(cluster)
}

func clusterReady(cluster *ske.Cluster) bool {
	status := clusterStatus(cluster)
	return status == string(ske.CLUSTERSTATUSSTATE_STATE_HEALTHY) || status == string(ske.CLUSTERSTATUSSTATE_STATE_HIBERNATED)
}

func clusterStatus(cluster *ske.Cluster) string {
	if cluster == nil || cluster.Status == nil || cluster.Status.Aggregated == nil {
		return ""
	}

	return string(*cluster.Status.Aggregated)
}

func containsKubernetesVersion(versions []ske.KubernetesVersion, expected string) bool {
	for i := range versions {
		if versions[i].GetVersion() == expected {
			return true
		}
	}

	return false
}

func containsAvailabilityZone(zones []ske.AvailabilityZone, expected string) bool {
	for i := range zones {
		if zones[i].GetName() == expected {
			return true
		}
	}

	return false
}

func containsMachineType(machineTypes []ske.MachineType, expected string) bool {
	for i := range machineTypes {
		if machineTypes[i].GetName() == expected {
			return true
		}
	}

	return false
}

func containsMachineImageVersion(images []ske.MachineImage, imageName, imageVersion string) bool {
	for i := range images {
		if images[i].GetName() != imageName {
			continue
		}
		for j := range images[i].Versions {
			if images[i].Versions[j].GetVersion() == imageVersion {
				return true
			}
		}
	}

	return false
}

func containsVolumeType(volumeTypes []ske.VolumeType, expected string) bool {
	for i := range volumeTypes {
		if volumeTypes[i].GetName() == expected {
			return true
		}
	}

	return false
}

func defaultClusterName(runID string) string {
	name := strings.ToLower(runID)
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-':
			return r
		default:
			return '-'
		}
	}, name)
	name = strings.Trim(name, "-")
	if name == "" {
		name = "run"
	}

	maxSuffixLength := maxClusterNameLength - len("kt2-")
	if len(name) > maxSuffixLength {
		name = name[:maxSuffixLength]
	}

	return "kt2-" + name
}
