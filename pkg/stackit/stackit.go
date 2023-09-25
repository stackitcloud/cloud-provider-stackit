package stackit

import (
	"io"

	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

const (
	// ProviderName is the name of the stackit provider
	ProviderName = "stackit"
)

type Stackit struct{}

// Config is used to read and store information from the cloud configuration file
type Config struct{}

func init() {
	cloudprovider.RegisterCloudProvider(ProviderName, func(config io.Reader) (cloudprovider.Interface, error) {
		cfg, err := ReadConfig(config)
		if err != nil {
			klog.Warningf("failed to read config: %v", err)
			return nil, err
		}
		cloud, err := NewStackit(cfg)
		if err != nil {
			klog.Warningf("New openstack client created failed with config: %v", err)
		}
		return cloud, err
	})
}

//nolint:golint,all // should be implemented
func ReadConfig(config io.Reader) (Config, error) {
	return Config{}, nil
}

// NewStackit creates a new instance of the stackit struct from a config struct
//
//nolint:golint,all // should be implemented
func NewStackit(cfg Config) (*Stackit, error) {
	stackit := Stackit{}
	return &stackit, nil
}

//nolint:golint,all // should be implemented
func (stackit *Stackit) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
}

func (stackit *Stackit) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return &LoadBalancer{}, true
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
