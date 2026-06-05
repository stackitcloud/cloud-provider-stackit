package client

import (
	"context"

	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"
)

type LoadBalancingClient interface {
	CreateLoadBalancer(ctx context.Context, payload *loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	ListLoadBalancers(ctx context.Context) ([]loadbalancer.LoadBalancer, error)
	DeleteLoadBalancer(ctx context.Context, lbName string) error
	GetLoadBalancer(ctx context.Context, id string) (*loadbalancer.LoadBalancer, error)
	UpdateLoadBalancer(ctx context.Context, lbName string, updates *loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	UpdateTargetPool(ctx context.Context, name, targetPoolName string, payload loadbalancer.UpdateTargetPoolPayload) error

	CreateCredentials(ctx context.Context, payload loadbalancer.CreateCredentialsPayload) (*loadbalancer.CreateCredentialsResponse, error)
	GetCredentials(ctx context.Context, credentialsRef string) (*loadbalancer.GetCredentialsResponse, error)
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

func (l loadBalancingClient) CreateLoadBalancer(ctx context.Context, payload *loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	lb, err := l.Client.CreateLoadBalancer(ctx, l.projectID, l.region).CreateLoadBalancerPayload(*payload).Execute()
	return lb, err
}

func (l loadBalancingClient) ListLoadBalancers(ctx context.Context) ([]loadbalancer.LoadBalancer, error) {
	lbResponse, err := l.Client.ListLoadBalancers(ctx, l.projectID, l.region).Execute()
	if err != nil {
		return nil, err
	}
	return lbResponse.GetLoadBalancers(), nil
}

func (l loadBalancingClient) DeleteLoadBalancer(ctx context.Context, lbName string) error {
	_, err := l.Client.DeleteLoadBalancer(ctx, l.projectID, l.region, lbName).Execute()
	return err
}

func (l loadBalancingClient) GetLoadBalancer(ctx context.Context, lbName string) (*loadbalancer.LoadBalancer, error) {
	return l.Client.GetLoadBalancer(ctx, l.projectID, l.region, lbName).Execute()
}

func (l loadBalancingClient) UpdateLoadBalancer(ctx context.Context, lbName string, updates *loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	lb, err := l.Client.UpdateLoadBalancer(ctx, l.projectID, l.region, lbName).UpdateLoadBalancerPayload(*updates).Execute()
	return lb, err
}

func (l loadBalancingClient) UpdateTargetPool(ctx context.Context, name, targetPoolName string, payload loadbalancer.UpdateTargetPoolPayload) error {
	_, err := l.Client.UpdateTargetPool(ctx, l.projectID, l.region, name, targetPoolName).UpdateTargetPoolPayload(payload).Execute()
	return err
}

func (l loadBalancingClient) UpdateTargatPool(ctx context.Context, lbName, targetPoolName string, payload loadbalancer.UpdateTargetPoolPayload) error {
	_, err := l.Client.UpdateTargetPool(ctx, l.projectID, l.region, lbName, targetPoolName).UpdateTargetPoolPayload(payload).Execute()
	return err
}

func (l loadBalancingClient) CreateCredentials(ctx context.Context, payload loadbalancer.CreateCredentialsPayload) (*loadbalancer.CreateCredentialsResponse, error) {
	resp, err := l.Client.CreateCredentials(ctx, l.projectID, l.region).CreateCredentialsPayload(payload).Execute()
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (l loadBalancingClient) GetCredentials(ctx context.Context, credentialsRef string) (*loadbalancer.GetCredentialsResponse, error) {
	resp, err := l.Client.GetCredentials(ctx, l.projectID, l.region, credentialsRef).Execute()
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (l loadBalancingClient) ListCredentials(ctx context.Context) (*loadbalancer.ListCredentialsResponse, error) {
	resp, err := l.Client.ListCredentials(ctx, l.projectID, l.region).Execute()
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (l loadBalancingClient) UpdateCredentials(ctx context.Context, credentialsRef string, payload loadbalancer.UpdateCredentialsPayload) error {
	_, err := l.Client.UpdateCredentials(ctx, l.projectID, l.region, credentialsRef).UpdateCredentialsPayload(payload).Execute()
	return err
}

func (l loadBalancingClient) DeleteCredentials(ctx context.Context, credentialsRef string) error {
	_, err := l.Client.DeleteCredentials(ctx, l.projectID, l.region, credentialsRef).Execute()
	return err
}
