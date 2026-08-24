package kubetest2

import (
	"flag"

	"github.com/spf13/pflag"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/version"
	"k8s.io/klog/v2"
	"sigs.k8s.io/kubetest2/pkg/types"
)

const (
	Name = "stackit"

	defaultRegion                     = "eu01"
	defaultAvailabilityZone           = "eu01-1"
	defaultNodeCount            int64 = 2
	defaultNodepoolName               = "default"
	defaultVolumeSizeGiB        int64 = 100
	defaultKubeconfigExpiration int64 = 6 * 60 * 60        // 6 hours
	minKubeconfigExpiration     int64 = 10 * 60            // 10 minutes
	maxKubeconfigExpiration     int64 = 180 * 24 * 60 * 60 // 180 days
)

type Deployer struct {
	options types.Options

	region              string
	kubernetesVersion   string
	availabilityZone    string
	machineType         string
	nodeImageName       string
	nodeImageVersion    string
	nodeCount           int64
	nodepoolName        string
	volumeSizeGiB       int64
	volumeType          string
	kubeconfigExpiresIn int64

	csiImageName string
	csiImageTag  string

	projectID             string
	serviceAccount        string
	parentContainerID     string
	projectMemberEmail    string
	kubeconfigPath        string
	serviceAccountKeyPath string

	resourceManagerEndpoint   string
	serviceAccountEndpoint    string
	authorizationEndpoint     string
	serviceEnablementEndpoint string
	skeEndpoint               string
	iaasEndpoint              string

	projectClient           projectClient
	serviceAccountClient    serviceAccountClient
	authorizationClient     authorizationClient
	serviceEnablementClient serviceEnablementClient
	skeClient               skeClient
	skeClientFactory        func(region, serviceAccount, endpoint string) (skeClient, error)
	csiApplier              csiApplier
}

var _ types.NewDeployer = New
var _ types.Deployer = &Deployer{}
var _ types.DeployerWithInit = &Deployer{}
var _ types.DeployerWithKubeconfig = &Deployer{}
var _ types.DeployerWithProvider = &Deployer{}
var _ types.DeployerWithVersion = &Deployer{}

func New(opts types.Options) (types.Deployer, *pflag.FlagSet) {
	d := &Deployer{
		options:             opts,
		region:              defaultRegion,
		availabilityZone:    defaultAvailabilityZone,
		nodeCount:           defaultNodeCount,
		nodepoolName:        defaultNodepoolName,
		volumeSizeGiB:       defaultVolumeSizeGiB,
		kubeconfigExpiresIn: defaultKubeconfigExpiration,
		skeClientFactory:    newSKEClient,
		csiApplier:          applyCSIManifestsNative,
	}

	fs := pflag.NewFlagSet(Name, pflag.ContinueOnError)
	bindFlags(fs, d)

	klog.InitFlags(nil)
	fs.AddGoFlagSet(flag.CommandLine)

	return d, fs
}

func (d *Deployer) Provider() string {
	return Name
}

func (d *Deployer) Version() string {
	return version.Version
}

func (d *Deployer) Build() error {
	return nil
}

func (d *Deployer) DumpClusterLogs() error {
	return nil
}
