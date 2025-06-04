package lbapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	oapiError "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	"github.com/stackitcloud/stackit-sdk-go/services/loadbalancer"
)

type ProjectStatus string

var (
	ErrorNotFound = errors.New("not found")
)

type Client interface {
	GetLoadBalancer(ctx context.Context, projectID string, name string) (*loadbalancer.LoadBalancer, error)
	// DeleteLoadBalancer returns no error if the load balancer doesn't exist.
	DeleteLoadBalancer(ctx context.Context, projectID string, name string) error
	// CreateLoadBalancer returns ErrorNotFound if the project is not enabled.
	CreateLoadBalancer(ctx context.Context, projectID string, loadbalancer *loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	UpdateLoadBalancer(ctx context.Context, projectID, name string, update *loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	UpdateTargetPool(ctx context.Context, projectID string, name string, targetPoolName string, payload loadbalancer.UpdateTargetPoolPayload) error
	CreateCredentials(ctx context.Context, projectID string, payload loadbalancer.CreateCredentialsPayload) (*loadbalancer.CreateCredentialsResponse, error)
	ListCredentials(ctx context.Context, projectID string) (*loadbalancer.ListCredentialsResponse, error)
	GetCredentials(ctx context.Context, projectID string, credentialRef string) (*loadbalancer.GetCredentialsResponse, error)
	UpdateCredentials(ctx context.Context, projectID, credentialRef string, payload loadbalancer.UpdateCredentialsPayload) error
	DeleteCredentials(ctx context.Context, projectID string, credentialRef string) error
}

type client struct {
	client loadbalancer.DefaultApi
	region string
}

var _ Client = (*client)(nil)

func NewClient(cl loadbalancer.DefaultApi, region string) (Client, error) {
	return &client{
		client: cl,
		region: region,
	}, nil
}

func (cl client) GetLoadBalancer(ctx context.Context, projectID, name string) (*loadbalancer.LoadBalancer, error) {
	lb, err := cl.client.GetLoadBalancerExecute(ctx, projectID, cl.region, name)
	if isOpenAPINotFound(err) {
		return lb, ErrorNotFound
	}
	return lb, err
}

// DeleteLoadBalancer returns no error if the load balancer doesn't exist.
func (cl client) DeleteLoadBalancer(ctx context.Context, projectID, name string) error {
	_, err := cl.client.DeleteLoadBalancerExecute(ctx, projectID, cl.region, name)
	return err
}

// CreateLoadBalancer returns ErrorNotFound if the project is not enabled.
func (cl client) CreateLoadBalancer(ctx context.Context, projectID string, create *loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	lb, err := cl.client.CreateLoadBalancer(ctx, projectID, cl.region).CreateLoadBalancerPayload(*create).XRequestID(uuid.NewString()).Execute()
	if isOpenAPINotFound(err) {
		return lb, ErrorNotFound
	}
	return lb, err
}

func (cl client) UpdateLoadBalancer(ctx context.Context, projectID, name string, update *loadbalancer.UpdateLoadBalancerPayload) (
	*loadbalancer.LoadBalancer, error,
) {
	return cl.client.UpdateLoadBalancer(ctx, projectID, cl.region, name).UpdateLoadBalancerPayload(*update).Execute()
}

func (cl client) UpdateTargetPool(ctx context.Context, projectID, name, targetPoolName string, payload loadbalancer.UpdateTargetPoolPayload) error {
	_, err := cl.client.UpdateTargetPool(ctx, projectID, cl.region, name, targetPoolName).UpdateTargetPoolPayload(payload).Execute()
	return err
}

func (cl client) CreateCredentials(
	ctx context.Context,
	projectID string,
	payload loadbalancer.CreateCredentialsPayload,
) (*loadbalancer.CreateCredentialsResponse, error) {
	return cl.client.CreateCredentials(ctx, projectID, cl.region).CreateCredentialsPayload(payload).XRequestID(uuid.NewString()).Execute()
}

func (cl client) ListCredentials(ctx context.Context, projectID string) (*loadbalancer.ListCredentialsResponse, error) {
	return cl.client.ListCredentialsExecute(ctx, projectID, cl.region)
}

func (cl client) GetCredentials(ctx context.Context, projectID, credentialsRef string) (*loadbalancer.GetCredentialsResponse, error) {
	return cl.client.GetCredentialsExecute(ctx, projectID, cl.region, credentialsRef)
}

func (cl client) UpdateCredentials(ctx context.Context, projectID, credentialsRef string, payload loadbalancer.UpdateCredentialsPayload) error {
	_, err := cl.client.UpdateCredentials(ctx, projectID, cl.region, credentialsRef).UpdateCredentialsPayload(payload).Execute()
	if err != nil {
		return err
	}
	return nil
}

func (cl client) DeleteCredentials(ctx context.Context, projectID, credentialsRef string) error {
	_, err := cl.client.DeleteCredentials(ctx, projectID, cl.region, credentialsRef).Execute()
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
