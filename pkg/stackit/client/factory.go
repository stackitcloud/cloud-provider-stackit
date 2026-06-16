package client

import sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"

// Factory produces clients for various STACKIT services.
type Factory interface {
	// LoadBalancing returns a STACKIT load balancing service client.
	LoadBalancing(options []sdkconfig.ConfigurationOption) (LoadBalancingClient, error)

	// IaaS returns a STACKIT IaaS service client.
	IaaS(options []sdkconfig.ConfigurationOption) (IaaSClient, error)
}

type factory struct {
	StackitRegion    string
	StackitProjectID string
}

func New(region, projectID string) Factory {
	return &factory{
		StackitRegion:    region,
		StackitProjectID: projectID,
	}
}

func (f factory) LoadBalancing(options []sdkconfig.ConfigurationOption) (LoadBalancingClient, error) {
	return NewLoadBalancingClient(f.StackitRegion, f.StackitProjectID, options)
}

func (f factory) IaaS(options []sdkconfig.ConfigurationOption) (IaaSClient, error) {
	return NewIaaSClient(f.StackitRegion, f.StackitProjectID, options)
}
