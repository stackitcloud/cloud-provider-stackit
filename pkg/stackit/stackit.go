package stackit

import (
	"errors"
	"fmt"
	"io"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/lbapi"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	"github.com/stackitcloud/stackit-sdk-go/services/loadbalancer"
	yaml "gopkg.in/yaml.v3"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

const (
	// ProviderName is the name of the stackit provider
	ProviderName = "stackit"
)

type Stackit struct {
	loadBalancer *LoadBalancer
}

// Config is used to read and store information from the cloud configuration file
type Config struct {
	ProjectID            string `yaml:"projectId"`
	NetworkID            string `yaml:"networkId"`
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
		cloud, err := NewStackit(cfg)
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

// NewStackit creates a new instance of the stackit struct from a config struct
func NewStackit(cfg Config) (*Stackit, error) {
	innerClient, err := loadbalancer.NewAPIClient(
		sdkconfig.WithEndpoint(cfg.LoadBalancerAPI.URL),
	)
	if err != nil {
		return nil, err
	}
	client, err := lbapi.NewClient(innerClient)
	if err != nil {
		return nil, err
	}

	lb, err := NewLoadBalancer(client, cfg.ProjectID, cfg.NetworkID, cfg.NonStackitClassNames)
	if err != nil {
		return nil, err
	}

	stackit := Stackit{
		loadBalancer: lb,
	}
	return &stackit, nil
}

//nolint:golint,all // should be implemented
func (stackit *Stackit) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
}

func (stackit *Stackit) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return stackit.loadBalancer, true
}

func (stackit *Stackit) Instances() (cloudprovider.Instances, bool) {
	return nil, false
}

func (stackit *Stackit) InstancesV2() (cloudprovider.InstancesV2, bool) {
	return nil, false
}

func (stackit *Stackit) Zones() (cloudprovider.Zones, bool) {
	return nil, false
}

func (stackit *Stackit) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}

func (stackit *Stackit) Routes() (cloudprovider.Routes, bool) {
	return nil, false
}

func (stackit *Stackit) ProviderName() string {
	return ProviderName
}

func (stackit *Stackit) HasClusterID() bool {
	return false
}
