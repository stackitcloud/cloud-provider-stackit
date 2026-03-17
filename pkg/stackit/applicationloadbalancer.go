package stackit

import (
	"context"

	"github.com/google/uuid"

	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
)

type ProjectStatus string

const (
	LBStatusReady       = "STATUS_READY"
	LBStatusTerminating = "STATUS_TERMINATING"
	LBStatusError       = "STATUS_ERROR"

	ProtocolHTTP  = "PROTOCOL_HTTP"
	ProtocolHTTPS = "PROTOCOL_HTTPS"

	ProjectStatusDisabled ProjectStatus = "STATUS_DISABLED"
)

type Client interface {
	GetLoadBalancer(ctx context.Context, projectID, region, name string) (*albsdk.LoadBalancer, error)
	DeleteLoadBalancer(ctx context.Context, projectID, region, name string) error
	CreateLoadBalancer(ctx context.Context, projectID, region string, albsdk *albsdk.CreateLoadBalancerPayload) (*albsdk.LoadBalancer, error)
	UpdateLoadBalancer(ctx context.Context, projectID, region, name string, update *albsdk.UpdateLoadBalancerPayload) (*albsdk.LoadBalancer, error)
	UpdateTargetPool(ctx context.Context, projectID, region, name string, targetPoolName string, payload albsdk.UpdateTargetPoolPayload) error
	CreateCredentials(ctx context.Context, projectID, region string, payload albsdk.CreateCredentialsPayload) (*albsdk.CreateCredentialsResponse, error)
	ListCredentials(ctx context.Context, projectID, region string) (*albsdk.ListCredentialsResponse, error)
	GetCredentials(ctx context.Context, projectID, region, credentialRef string) (*albsdk.GetCredentialsResponse, error)
	UpdateCredentials(ctx context.Context, projectID, region, credentialRef string, payload albsdk.UpdateCredentialsPayload) error
	DeleteCredentials(ctx context.Context, projectID, region, credentialRef string) error
}

type client struct {
	client *albsdk.APIClient
}

var _ Client = (*client)(nil)

func NewClient(cl *albsdk.APIClient) (Client, error) {
	return &client{client: cl}, nil
}

func (cl client) GetLoadBalancer(ctx context.Context, projectID, region, name string) (*albsdk.LoadBalancer, error) {
	lb, err := cl.client.DefaultAPI.GetLoadBalancer(ctx, projectID, region, name).Execute()
	if isOpenAPINotFound(err) {
		return lb, ErrorNotFound
	}
	return lb, err
}

// DeleteLoadBalancer returns no error if the load balancer doesn't exist.
func (cl client) DeleteLoadBalancer(ctx context.Context, projectID, region, name string) error {
	_, err := cl.client.DefaultAPI.DeleteLoadBalancer(ctx, projectID, region, name).Execute()
	return err
}

// CreateLoadBalancer returns ErrorNotFound if the project is not enabled.
func (cl client) CreateLoadBalancer(ctx context.Context, projectID, region string, create *albsdk.CreateLoadBalancerPayload) (*albsdk.LoadBalancer, error) {
	lb, err := cl.client.DefaultAPI.CreateLoadBalancer(ctx, projectID, region).CreateLoadBalancerPayload(*create).XRequestID(uuid.NewString()).Execute()
	if isOpenAPINotFound(err) {
		return lb, ErrorNotFound
	}
	return lb, err
}

func (cl client) UpdateLoadBalancer(ctx context.Context, projectID, region, name string, update *albsdk.UpdateLoadBalancerPayload) (
	*albsdk.LoadBalancer, error,
) {
	return cl.client.DefaultAPI.UpdateLoadBalancer(ctx, projectID, region, name).UpdateLoadBalancerPayload(*update).Execute()
}

func (cl client) UpdateTargetPool(ctx context.Context, projectID, region, name, targetPoolName string, payload albsdk.UpdateTargetPoolPayload) error {
	_, err := cl.client.DefaultAPI.UpdateTargetPool(ctx, projectID, region, name, targetPoolName).UpdateTargetPoolPayload(payload).Execute()
	return err
}

func (cl client) CreateCredentials(
	ctx context.Context,
	projectID string,
	region string,
	payload albsdk.CreateCredentialsPayload,
) (*albsdk.CreateCredentialsResponse, error) {
	return cl.client.DefaultAPI.CreateCredentials(ctx, projectID, region).CreateCredentialsPayload(payload).XRequestID(uuid.NewString()).Execute()
}

func (cl client) ListCredentials(ctx context.Context, projectID, region string) (*albsdk.ListCredentialsResponse, error) {
	return cl.client.DefaultAPI.ListCredentials(ctx, projectID, region).Execute()
}

func (cl client) GetCredentials(ctx context.Context, projectID, region, credentialsRef string) (*albsdk.GetCredentialsResponse, error) {
	return cl.client.DefaultAPI.GetCredentials(ctx, projectID, region, credentialsRef).Execute()
}

func (cl client) UpdateCredentials(ctx context.Context, projectID, region, credentialsRef string, payload albsdk.UpdateCredentialsPayload) error {
	_, err := cl.client.DefaultAPI.UpdateCredentials(ctx, projectID, region, credentialsRef).UpdateCredentialsPayload(payload).Execute()
	if err != nil {
		return err
	}
	return nil
}

func (cl client) DeleteCredentials(ctx context.Context, projectID, region, credentialsRef string) error {
	_, err := cl.client.DefaultAPI.DeleteCredentials(ctx, projectID, region, credentialsRef).Execute()
	if err != nil {
		return err
	}
	return nil
}
