package ccm

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"github.com/stackitcloud/stackit-sdk-go/services/loadbalancer"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

const (
	// ProviderName is the name of the stackit provider
	ProviderName = "stackit"
	// TODO: remove old provider after migration
	oldProviderName = "openstack"

	// metricsRemoteWrite ENVs for metrics shipping to argus using basic auth
	stackitRemoteWriteEndpointKey = "STACKIT_REMOTEWRITE_ENDPOINT"
	stackitRemoteWriteUserKey     = "STACKIT_REMOTEWRITE_USER"
	stackitRemoteWritePasswordKey = "STACKIT_REMOTEWRITE_PASSWORD"

	// stackitLoadBalancerEmergencyAPIToken ENV to use a static JWT token, used for emergency access
	stackitLoadBalancerEmergencyAPIToken = "STACKIT_LB_API_EMERGENCY_TOKEN" //nolint:gosec // this is just the env var name
)

type CloudControllerManager struct {
	loadBalancer *LoadBalancer
	instances    *Instances
}

// Config is used to read and store information from the cloud configuration file
type Config struct {
	ProjectID            string `yaml:"projectId"`
	NetworkID            string `yaml:"networkId"`
	Region               string `yaml:"region"`
	NonStackitClassNames string `yaml:"nonStackitClassNames"`
	LoadBalancerAPI      struct {
		URL string `yaml:"url"`
	} `yaml:"loadBalancerApi"`
}

func init() {
	cloudprovider.RegisterCloudProvider(ProviderName, func(config io.Reader) (cloudprovider.Interface, error) {
		cfg, err := ReadConfig(config)
		if err != nil {
			klog.Warningf("failed to read config: %v", err)
			return nil, err
		}
		obs, err := BuildObservability()
		if err != nil {
			klog.Warningf("failed to build metricsRemoteWrite: %v", err)
			return nil, err
		}
		cloud, err := NewCloudControllerManager(&cfg, obs)
		if err != nil {
			klog.Warningf("failed to create STACKIT cloud provider: %v", err)
		}
		return cloud, err
	})
}

func ReadConfig(configReader io.Reader) (Config, error) {
	if configReader == nil {
		return Config{}, errors.New("cloud config is missing")
	}
	configBytes, err := io.ReadAll(configReader)
	if err != nil {
		return Config{}, err
	}
	config := Config{}
	err = yaml.Unmarshal(configBytes, &config)
	if err != nil {
		return Config{}, err
	}
	if config.ProjectID == "" {
		return Config{}, errors.New("projectId must be set")
	}
	if config.NetworkID == "" {
		return Config{}, errors.New("networkId must be set")
	}
	if config.Region == "" {
		return Config{}, errors.New("region must be set")
	}

	switch config.NonStackitClassNames {
	case nonStackitClassNameModeIgnore, nonStackitClassNameModeUpdate, nonStackitClassNameModeUpdateAndCreate:
		// NonStackitClassNames is valid input
	case "":
		// Apply default
		config.NonStackitClassNames = nonStackitClassNameModeUpdateAndCreate
	default:
		// return error if invalid input
		return Config{}, fmt.Errorf(
			"nonStackitClassNames %q must be set to %s, %s or %s",
			config.NonStackitClassNames,
			nonStackitClassNameModeIgnore,
			nonStackitClassNameModeUpdate,
			nonStackitClassNameModeUpdateAndCreate,
		)
	}

	if config.LoadBalancerAPI.URL == "" {
		config.LoadBalancerAPI.URL = "https://load-balancer.api.eu01.stackit.cloud"
	}

	return config, nil
}

func BuildObservability() (*MetricsRemoteWrite, error) {
	e := os.Getenv(stackitRemoteWriteEndpointKey)
	u := os.Getenv(stackitRemoteWriteUserKey)
	p := os.Getenv(stackitRemoteWritePasswordKey)
	if e == "" && u == "" && p == "" {
		return nil, nil
	}
	if e != "" && u != "" && p != "" {
		return &MetricsRemoteWrite{
			endpoint: e,
			username: u,
			password: p,
		}, nil
	}
	missingKeys := []string{}
	if e == "" {
		missingKeys = append(missingKeys, stackitRemoteWriteEndpointKey)
	}
	if u == "" {
		missingKeys = append(missingKeys, stackitRemoteWriteUserKey)
	}
	if p == "" {
		missingKeys = append(missingKeys, stackitRemoteWritePasswordKey)
	}
	return nil, fmt.Errorf("missing from env: %q", missingKeys)
}

// NewCloudControllerManager creates a new instance of the stackit struct from a config struct
func NewCloudControllerManager(cfg *Config, obs *MetricsRemoteWrite) (*CloudControllerManager, error) {
	lbOpts := []sdkconfig.ConfigurationOption{
		sdkconfig.WithEndpoint(cfg.LoadBalancerAPI.URL),
	}

	// The token is only provided by the 'gardener-extension-provider-stackit' in case of emergency access.
	// In those cases, the [cfg.LoadBalancerAPI.URL] will also be different (direct API URL instead of the API Gateway)
	lbEmergencyAPIToken := os.Getenv(stackitLoadBalancerEmergencyAPIToken)
	if lbEmergencyAPIToken != "" {
		klog.Warningf("using emergency token for loadbalancer api on host: %s", cfg.LoadBalancerAPI.URL)
		lbOpts = append(lbOpts, sdkconfig.WithToken(lbEmergencyAPIToken))
	}

	innerClient, err := loadbalancer.NewAPIClient(lbOpts...)
	if err != nil {
		return nil, err
	}
	client, err := stackit.NewLoadbalancerClient(innerClient, cfg.Region)
	if err != nil {
		return nil, err
	}

	iaasInnerClient, err := iaas.NewAPIClient(
		sdkconfig.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create IaaS client: %v", err)
	}
	nodeClient, err := stackit.NewNodeClient(iaasInnerClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create Node client: %v", err)
	}
	instances, err := NewInstance(nodeClient, cfg.ProjectID, cfg.Region)
	if err != nil {
		return nil, err
	}

	lb, err := NewLoadBalancer(client, cfg.ProjectID, cfg.NetworkID, cfg.NonStackitClassNames, obs)
	if err != nil {
		return nil, err
	}

	ccm := CloudControllerManager{
		loadBalancer: lb,
		instances:    instances,
	}
	return &ccm, nil
}

func (ccm *CloudControllerManager) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, _ <-chan struct{}) {
	// create an EventRecorder
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: clientBuilder.ClientOrDie("cloud-controller-manager").CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "stackit-cloud-controller-manager"})
	ccm.loadBalancer.recorder = recorder
}

func (ccm *CloudControllerManager) InstancesV2() (cloudprovider.InstancesV2, bool) {
	return ccm.instances, true
}

func (ccm *CloudControllerManager) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return ccm.loadBalancer, true
}

func (ccm *CloudControllerManager) Instances() (cloudprovider.Instances, bool) {
	return nil, false
}

func (ccm *CloudControllerManager) Zones() (cloudprovider.Zones, bool) {
	return nil, false
}

func (ccm *CloudControllerManager) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}

func (ccm *CloudControllerManager) Routes() (cloudprovider.Routes, bool) {
	return nil, false
}

func (ccm *CloudControllerManager) ProviderName() string {
	return ProviderName
}

func (ccm *CloudControllerManager) HasClusterID() bool {
	return false
}
