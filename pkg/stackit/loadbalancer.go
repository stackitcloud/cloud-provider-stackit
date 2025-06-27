package stackit

import (
	"context"

	"github.com/google/uuid"
	"github.com/stackitcloud/stackit-sdk-go/services/loadbalancer"
)

func (cl lbClient) GetLoadBalancer(ctx context.Context, projectID, name string) (*loadbalancer.LoadBalancer, error) {
	lb, err := cl.client.GetLoadBalancerExecute(ctx, projectID, cl.region, name)
	if isOpenAPINotFound(err) {
		return lb, ErrorNotFound
	}
	return lb, err
}

// DeleteLoadBalancer returns no error if the load balancer doesn't exist.
func (cl lbClient) DeleteLoadBalancer(ctx context.Context, projectID, name string) error {
	_, err := cl.client.DeleteLoadBalancerExecute(ctx, projectID, cl.region, name)
	return err
}

// CreateLoadBalancer returns ErrorNotFound if the project is not enabled.
func (cl lbClient) CreateLoadBalancer(ctx context.Context, projectID string, create *loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	lb, err := cl.client.CreateLoadBalancer(ctx, projectID, cl.region).CreateLoadBalancerPayload(*create).XRequestID(uuid.NewString()).Execute()
	if isOpenAPINotFound(err) {
		return lb, ErrorNotFound
	}
	return lb, err
}

func (cl lbClient) UpdateLoadBalancer(ctx context.Context, projectID, name string, update *loadbalancer.UpdateLoadBalancerPayload) (
	*loadbalancer.LoadBalancer, error,
) {
	return cl.client.UpdateLoadBalancer(ctx, projectID, cl.region, name).UpdateLoadBalancerPayload(*update).Execute()
}

func (cl lbClient) UpdateTargetPool(ctx context.Context, projectID, name, targetPoolName string, payload loadbalancer.UpdateTargetPoolPayload) error {
	_, err := cl.client.UpdateTargetPool(ctx, projectID, cl.region, name, targetPoolName).UpdateTargetPoolPayload(payload).Execute()
	return err
}

func (cl lbClient) ListCredentials(ctx context.Context, projectID string) (*loadbalancer.ListCredentialsResponse, error) {
	return cl.client.ListCredentialsExecute(ctx, projectID, cl.region)
}

func (cl lbClient) GetCredentials(ctx context.Context, projectID, credentialsRef string) (*loadbalancer.GetCredentialsResponse, error) {
	return cl.client.GetCredentialsExecute(ctx, projectID, cl.region, credentialsRef)
}

func (cl lbClient) CreateCredentials(ctx context.Context, projectID string, payload loadbalancer.CreateCredentialsPayload) (*loadbalancer.CreateCredentialsResponse, error) { //nolint:lll // looks weird when shortened
	return cl.client.CreateCredentials(ctx, projectID, cl.region).CreateCredentialsPayload(payload).XRequestID(uuid.NewString()).Execute()
}

func (cl lbClient) UpdateCredentials(ctx context.Context, projectID, credentialsRef string, payload loadbalancer.UpdateCredentialsPayload) error {
	_, err := cl.client.UpdateCredentials(ctx, projectID, cl.region, credentialsRef).UpdateCredentialsPayload(payload).Execute()
	if err != nil {
		return err
	}
	return nil
}

func (cl lbClient) DeleteCredentials(ctx context.Context, projectID, credentialsRef string) error {
	_, err := cl.client.DeleteCredentials(ctx, projectID, cl.region, credentialsRef).Execute()
	if err != nil {
		return err
	}
	return nil
}
