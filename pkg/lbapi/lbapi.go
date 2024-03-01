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

const (
	LBStatusReady       = "STATUS_READY"
	LBStatusTerminating = "STATUS_TERMINATING"
	LBStatusError       = "STATUS_ERROR"

	ProtocolTCP      = "PROTOCOL_TCP"
	ProtocolTCPProxy = "PROTOCOL_TCP_PROXY"
	ProtocolUDP      = "PROTOCOL_UDP"

	ProjectStatusDisabled ProjectStatus = "STATUS_DISABLED"
)

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
	EnableService(ctx context.Context, projectID string) error
	GetServiceStatus(ctx context.Context, projectID string) (ProjectStatus, error)
}

type client struct {
	client *loadbalancer.APIClient
}

var _ Client = (*client)(nil)

func NewClient(cl *loadbalancer.APIClient) (Client, error) {
	return &client{client: cl}, nil
}

func (cl client) GetLoadBalancer(ctx context.Context, projectID, name string) (*loadbalancer.LoadBalancer, error) {
	lb, err := cl.client.GetLoadBalancerExecute(ctx, projectID, name)
	if isOpenAPINotFound(err) {
		return lb, ErrorNotFound
	}
	return lb, err
}

// DeleteLoadBalancer returns no error if the load balancer doesn't exist.
func (cl client) DeleteLoadBalancer(ctx context.Context, projectID, name string) error {
	_, err := cl.client.DeleteLoadBalancerExecute(ctx, projectID, name)
	return err
}

// CreateLoadBalancer returns ErrorNotFound if the project is not enabled.
func (cl client) CreateLoadBalancer(ctx context.Context, projectID string, create *loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	lb, err := cl.client.CreateLoadBalancer(ctx, projectID).CreateLoadBalancerPayload(*create).XRequestID(uuid.NewString()).Execute()
	if isOpenAPINotFound(err) {
		return lb, ErrorNotFound
	}
	return lb, err
}

func (cl client) UpdateLoadBalancer(ctx context.Context, projectID, name string, update *loadbalancer.UpdateLoadBalancerPayload) (
	*loadbalancer.LoadBalancer, error,
) {
	return cl.client.UpdateLoadBalancer(ctx, projectID, name).UpdateLoadBalancerPayload(*update).Execute()
}

func (cl client) UpdateTargetPool(ctx context.Context, projectID, name, targetPoolName string, payload loadbalancer.UpdateTargetPoolPayload) error {
	_, err := cl.client.UpdateTargetPool(ctx, projectID, name, targetPoolName).UpdateTargetPoolPayload(payload).Execute()
	return err
}

func (cl client) EnableService(ctx context.Context, projectID string) error {
	_, err := cl.client.EnableLoadBalancing(ctx, projectID).XRequestID(uuid.NewString()).Execute()
	return err
}

func (cl client) GetServiceStatus(ctx context.Context, projectID string) (ProjectStatus, error) {
	res, err := cl.client.GetStatusExecute(ctx, projectID)
	if res.Status == nil {
		return "", errors.New("server response is missing project status")
	}
	return ProjectStatus(*res.Status), err
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
