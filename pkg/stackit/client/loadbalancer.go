package client

import (
	"context"

	"github.com/google/uuid"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"
)

type LoadBalancingClient interface {
	CreateLoadBalancer(ctx context.Context, payload *loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	GetLoadBalancer(ctx context.Context, id string) (*loadbalancer.LoadBalancer, error)
	UpdateLoadBalancer(ctx context.Context, lbName string, updates *loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	DeleteLoadBalancer(ctx context.Context, lbName string) error
	UpdateTargetPool(ctx context.Context, name, targetPoolName string, payload loadbalancer.UpdateTargetPoolPayload) error

	CreateCredentials(ctx context.Context, payload loadbalancer.CreateCredentialsPayload) (*loadbalancer.CreateCredentialsResponse, error)
	ListCredentials(ctx context.Context) (*loadbalancer.ListCredentialsResponse, error)
	UpdateCredentials(ctx context.Context, credentialsRef string, payload loadbalancer.UpdateCredentialsPayload) error
	DeleteCredentials(ctx context.Context, credentialsRef string) error
}

type loadBalancingClient struct {
	Client    loadbalancer.DefaultAPI
	projectID string
	region    string
}

func NewLoadBalancingClient(region, projectID string, options []sdkconfig.ConfigurationOption) (LoadBalancingClient, error) {
	apiClient, err := loadbalancer.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}
	return &loadBalancingClient{
		Client:    apiClient.DefaultAPI,
		projectID: projectID,
		region:    region,
	}, nil
}

func (l *loadBalancingClient) CreateLoadBalancer(ctx context.Context, payload *loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	return withResponseID(ctx, func(ctx context.Context) (*loadbalancer.LoadBalancer, error) {
		return l.Client.
			CreateLoadBalancer(ctx, l.projectID, l.region).
			CreateLoadBalancerPayload(*payload).
			XRequestID(uuid.NewString()).
			Execute()
	})
}

func (l *loadBalancingClient) DeleteLoadBalancer(ctx context.Context, lbName string) error {
	_, err := withResponseID(ctx, func(ctx context.Context) (map[string]any, error) {
		return l.Client.
			DeleteLoadBalancer(ctx, l.projectID, l.region, lbName).
			Execute()
	})
	return err
}

func (l *loadBalancingClient) GetLoadBalancer(ctx context.Context, lbName string) (*loadbalancer.LoadBalancer, error) {
	return withResponseID(ctx, func(ctx context.Context) (*loadbalancer.LoadBalancer, error) {
		return l.Client.
			GetLoadBalancer(ctx, l.projectID, l.region, lbName).
			Execute()
	})
}

func (l *loadBalancingClient) UpdateLoadBalancer(ctx context.Context, lbName string, updates *loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	return withResponseID(ctx, func(ctx context.Context) (*loadbalancer.LoadBalancer, error) {
		return l.Client.
			UpdateLoadBalancer(ctx, l.projectID, l.region, lbName).
			UpdateLoadBalancerPayload(*updates).
			Execute()
	})
}

func (l *loadBalancingClient) UpdateTargetPool(ctx context.Context, name, targetPoolName string, payload loadbalancer.UpdateTargetPoolPayload) error {
	_, err := withResponseID(ctx, func(ctx context.Context) (*loadbalancer.TargetPool, error) {
		return l.Client.
			UpdateTargetPool(ctx, l.projectID, l.region, name, targetPoolName).
			UpdateTargetPoolPayload(payload).
			Execute()
	})
	return err
}

func (l *loadBalancingClient) CreateCredentials(ctx context.Context, payload loadbalancer.CreateCredentialsPayload) (*loadbalancer.CreateCredentialsResponse, error) {
	return withResponseID(ctx, func(ctx context.Context) (*loadbalancer.CreateCredentialsResponse, error) {
		return l.Client.
			CreateCredentials(ctx, l.projectID, l.region).
			CreateCredentialsPayload(payload).
			XRequestID(uuid.NewString()).
			Execute()
	})
}

func (l *loadBalancingClient) ListCredentials(ctx context.Context) (*loadbalancer.ListCredentialsResponse, error) {
	return withResponseID(ctx, func(ctx context.Context) (*loadbalancer.ListCredentialsResponse, error) {
		return l.Client.
			ListCredentials(ctx, l.projectID, l.region).
			Execute()
	})
}

func (l *loadBalancingClient) UpdateCredentials(ctx context.Context, credentialsRef string, payload loadbalancer.UpdateCredentialsPayload) error {
	_, err := withResponseID(ctx, func(ctx context.Context) (*loadbalancer.UpdateCredentialsResponse, error) {
		return l.Client.
			UpdateCredentials(ctx, l.projectID, l.region, credentialsRef).
			UpdateCredentialsPayload(payload).
			Execute()
	})
	return err
}

func (l *loadBalancingClient) DeleteCredentials(ctx context.Context, credentialsRef string) error {
	_, err := withResponseID(ctx, func(ctx context.Context) (map[string]any, error) {
		return l.Client.
			DeleteCredentials(ctx, l.projectID, l.region, credentialsRef).
			Execute()
	})
	return err
}
