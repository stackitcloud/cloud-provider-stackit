package client

import (
	"context"
	"errors"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
)

const (
	UserAgent = "cloud-provider-stackit"
)

var (
	ErrorNotFound = errors.New("not found")
)

// Factory produces clients for various STACKIT services.
type Factory interface {
	// LoadBalancing returns a STACKIT load balancing service client.
	LoadBalancing(context.Context) (LoadBalancingClient, error)

	// IaaS returns a STACKIT IaaS service client.
	IaaS() (IaaSClient, error)
}

type factory struct {
	StackitRegion       string
	StackitProjectID    string
	StackitAPIEndpoints config.APIEndpoints
}

func New(region, projectID string, apiEndpoints config.APIEndpoints) Factory {
	return &factory{
		StackitRegion:       region,
		StackitProjectID:    projectID,
		StackitAPIEndpoints: apiEndpoints,
	}
}

func (f factory) LoadBalancing(ctx context.Context) (LoadBalancingClient, error) {
	return NewLoadBalancingClient(ctx, f.StackitRegion, f.StackitProjectID, f.StackitAPIEndpoints)
}

func (f factory) IaaS() (IaaSClient, error) {
	return NewIaaSClient(f.StackitRegion, f.StackitAPIEndpoints)
}
