package stackit

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	oapiError "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
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
	lb, err := cl.client.GetLoadBalancerExecute(ctx, projectID, region, name)
	if isOpenAPINotFound(err) {
		return lb, ErrorNotFound
	}
	return lb, err
}

// DeleteLoadBalancer returns no error if the load balancer doesn't exist.
func (cl client) DeleteLoadBalancer(ctx context.Context, projectID, region, name string) error {
	_, err := cl.client.DeleteLoadBalancerExecute(ctx, projectID, region, name)
	return err
}

// CreateLoadBalancer returns ErrorNotFound if the project is not enabled.
func (cl client) CreateLoadBalancer(ctx context.Context, projectID, region string, create *albsdk.CreateLoadBalancerPayload) (*albsdk.LoadBalancer, error) {
	lb, err := cl.client.CreateLoadBalancer(ctx, projectID, region).CreateLoadBalancerPayload(*create).XRequestID(uuid.NewString()).Execute()
	if isOpenAPINotFound(err) {
		return lb, ErrorNotFound
	}
	return lb, err
}

func (cl client) UpdateLoadBalancer(ctx context.Context, projectID, region, name string, update *albsdk.UpdateLoadBalancerPayload) (
	*albsdk.LoadBalancer, error,
) {
	return cl.client.UpdateLoadBalancer(ctx, projectID, region, name).UpdateLoadBalancerPayload(*update).Execute()
}

func (cl client) UpdateTargetPool(ctx context.Context, projectID, region, name, targetPoolName string, payload albsdk.UpdateTargetPoolPayload) error {
	_, err := cl.client.UpdateTargetPool(ctx, projectID, region, name, targetPoolName).UpdateTargetPoolPayload(payload).Execute()
	return err
}

func (cl client) CreateCredentials(
	ctx context.Context,
	projectID string,
	region string,
	payload albsdk.CreateCredentialsPayload,
) (*albsdk.CreateCredentialsResponse, error) {
	return cl.client.CreateCredentials(ctx, projectID, region).CreateCredentialsPayload(payload).XRequestID(uuid.NewString()).Execute()
}

func (cl client) ListCredentials(ctx context.Context, projectID, region string) (*albsdk.ListCredentialsResponse, error) {
	return cl.client.ListCredentialsExecute(ctx, projectID, region)
}

func (cl client) GetCredentials(ctx context.Context, projectID, region, credentialsRef string) (*albsdk.GetCredentialsResponse, error) {
	return cl.client.GetCredentialsExecute(ctx, projectID, region, credentialsRef)
}

func (cl client) UpdateCredentials(ctx context.Context, projectID, region, credentialsRef string, payload albsdk.UpdateCredentialsPayload) error {
	_, err := cl.client.UpdateCredentials(ctx, projectID, region, credentialsRef).UpdateCredentialsPayload(payload).Execute()
	if err != nil {
		return err
	}
	return nil
}

func (cl client) DeleteCredentials(ctx context.Context, projectID, region, credentialsRef string) error {
	_, err := cl.client.DeleteCredentials(ctx, projectID, region, credentialsRef).Execute()
	if err != nil {
		return err
	}
	return nil
}

func isOpenAPINotFound(err error) bool {
	apiErr := &oapiError.GenericOpenAPIError{}
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusNotFound
}

func IsNotFound(err error) bool {
	return errors.Is(err, ErrorNotFound)
}
