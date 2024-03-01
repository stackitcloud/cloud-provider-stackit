package stackit

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider/api"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/lbapi"
	"github.com/stackitcloud/stackit-sdk-go/services/loadbalancer"
)

const (
	retryDuration = 10 * time.Second

	// stackitClassName defines the class name that deploys a STACKIT load balancer using the cloud controller manager.
	// Other classes are ignored by the cloud controller manager.
	classNameStackit = "stackit"
)

// LoadBalancer is used for creating and maintaining load balancers.
type LoadBalancer struct {
	client    lbapi.Client
	projectID string
	networkID string
}

var _ cloudprovider.LoadBalancer = (*LoadBalancer)(nil)

func NewLoadBalancer(client lbapi.Client, projectID, networkID string) (*LoadBalancer, error) {
	return &LoadBalancer{
		client:    client,
		projectID: projectID,
		networkID: networkID,
	}, nil
}

// GetLoadBalancer returns whether the specified load balancer exists, and
// if so, what its status is.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager.
func (l *LoadBalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (
	status *corev1.LoadBalancerStatus, exists bool, err error) {
	if isImplementedElsewhere(service) {
		// If the load balancer is implemented elsewhere we report it as not found, so that the finalizer can be removed.
		return nil, false, nil
	}

	lb, err := l.client.GetLoadBalancer(ctx, l.projectID, l.GetLoadBalancerName(ctx, clusterName, service))
	switch {
	case lbapi.IsNotFound(err):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	return loadBalancerStatus(lb), true, nil
}

// GetLoadBalancerName returns the name of the load balancer. Implementations must treat the
// *v1.Service parameter as read-only and not modify it.
func (l *LoadBalancer) GetLoadBalancerName(_ context.Context, _ string, service *corev1.Service) string {
	name := fmt.Sprintf("k8s-svc-%s-", service.UID)
	avail := 63 - len(name)
	if len(service.Name) <= avail {
		name += service.Name
	} else {
		name += service.Name[:avail]
		// Load balancer names must be DNS-compatible, which disallows trailing dashes.
		// By cutting the name in the middle, we might have a trailing dash.
		// By trimming it, we still produce a non-empty valid name.
		name = strings.TrimRight(name, "-")
	}
	return name
}

// EnsureLoadBalancer creates a new load balancer 'name', or updates the existing one. Returns the status of the balancer
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager.
//
// Implementations may return a (possibly wrapped) api.RetryError to enforce
// backing off at a fixed duration. This can be used for cases like when the
// load balancer is not ready yet (e.g., it is still being provisioned) and
// polling at a fixed rate is preferred over backing off exponentially in
// order to minimize latency.
func (l *LoadBalancer) EnsureLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) (
	*corev1.LoadBalancerStatus, error,
) {
	if isImplementedElsewhere(service) {
		return nil, cloudprovider.ImplementedElsewhere
	}

	name := l.GetLoadBalancerName(ctx, clusterName, service)

	lb, err := l.client.GetLoadBalancer(ctx, l.projectID, name)

	if err != nil && !lbapi.IsNotFound(err) {
		return nil, err
	}
	if lbapi.IsNotFound(err) {
		return l.createLoadBalancer(ctx, clusterName, service, nodes)
	}

	spec, err := lbSpecFromService(service, nodes, l.networkID)
	if err != nil {
		return nil, fmt.Errorf("invalid load balancer specification: %w", err)
	}

	fulfills, immutableChanged := compareLBwithSpec(lb, spec)
	if immutableChanged != nil {
		return nil, fmt.Errorf("updated to load balancer cannot be fulfilled. Load balancer API doesn't support changing %q", immutableChanged.field)
	}
	if !fulfills {
		// We create the update payload from a new spec.
		// However, we need to copy over the version because it is required on every update.
		spec.Version = lb.Version
		spec.Name = &name
		lb, err = l.client.UpdateLoadBalancer(ctx, l.projectID, name, (*loadbalancer.UpdateLoadBalancerPayload)(spec))
		if err != nil {
			return nil, fmt.Errorf("failed to update load balancer: %w", err)
		}
	}

	if *lb.Status == lbapi.LBStatusError {
		return nil, fmt.Errorf("the load balancer is in an error state")
	}
	if *lb.Status != lbapi.LBStatusReady {
		return nil, api.NewRetryError("waiting for load balancer to become ready", retryDuration)
	}

	return loadBalancerStatus(lb), nil
}

func (l *LoadBalancer) createLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) (
	*corev1.LoadBalancerStatus, error,
) {
	spec, err := lbSpecFromService(service, nodes, l.networkID)
	if err != nil {
		return nil, fmt.Errorf("invalid load balancer specification: %w", err)
	}

	name := l.GetLoadBalancerName(ctx, clusterName, service)
	spec.Name = &name

	lb, createErr := l.client.CreateLoadBalancer(ctx, l.projectID, spec)
	if createErr != nil && !lbapi.IsNotFound(createErr) {
		return nil, createErr
	}
	if lbapi.IsNotFound(createErr) {
		// If the project is disabled, load balancer creation returns a 404.
		// In this case we will enable the project, if this actually was the reason for the 404.
		status, err := l.client.GetServiceStatus(ctx, l.projectID)
		if err != nil {
			return nil, fmt.Errorf("failed to get project status: %w", err)
		}
		if status != lbapi.ProjectStatusDisabled {
			return nil, fmt.Errorf("failed to create load balancer while the project has status %q: %w", status, createErr)
		}
		err = l.client.EnableService(ctx, l.projectID)
		if err != nil {
			return nil, fmt.Errorf("failed to enable project: %w", err)
		}
		return nil, api.NewRetryError("waiting for project to become ready after enabling", retryDuration)
	}

	if lb.Status == nil || *lb.Status != lbapi.LBStatusReady {
		return nil, api.NewRetryError("waiting for load balancer to become ready", retryDuration)
	}

	return loadBalancerStatus(lb), nil
}

// UpdateLoadBalancer updates hosts under the specified load balancer.
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager.
//
// It is not called on controller start-up. EnsureLoadBalancer must also ensure to update targets.
func (l *LoadBalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	if isImplementedElsewhere(service) {
		return cloudprovider.ImplementedElsewhere
	}

	spec, err := lbSpecFromService(service, nodes, l.networkID)
	if err != nil {
		return fmt.Errorf("invalid service: %w", err)
	}

	for _, pool := range *spec.TargetPools {
		err := l.client.UpdateTargetPool(ctx, l.projectID, l.GetLoadBalancerName(ctx, clusterName, service), *pool.Name, loadbalancer.UpdateTargetPoolPayload(pool))
		if err != nil {
			return fmt.Errorf("failed to update target pool %q: %w", *pool.Name, err)
		}
	}

	return nil
}

// EnsureLoadBalancerDeleted deletes the specified load balancer if it
// exists, returning nil if the load balancer specified either didn't exist or
// was successfully deleted.
// This construction is useful because many cloud providers' load balancers
// have multiple underlying components, meaning a Get could say that the LB
// doesn't exist even if some part of it is still laying around.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (l *LoadBalancer) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	if isImplementedElsewhere(service) {
		return cloudprovider.ImplementedElsewhere
	}

	name := l.GetLoadBalancerName(ctx, clusterName, service)

	lb, err := l.client.GetLoadBalancer(ctx, l.projectID, name)
	switch {
	case lbapi.IsNotFound(err):
		return nil
	case err != nil:
		return err
	case lb.Status != nil && *lb.Status == lbapi.LBStatusTerminating:
		return nil
	}

	err = l.client.DeleteLoadBalancer(ctx, l.projectID, name)
	// Deleting a load balancer doesn't return an error if the load balancer cannot be found.
	if err != nil {
		return err
	}

	return nil
}

func loadBalancerStatus(lb *loadbalancer.LoadBalancer) *corev1.LoadBalancerStatus {
	var ip string
	if lb.Options != nil && lb.Options.PrivateNetworkOnly != nil && *lb.Options.PrivateNetworkOnly {
		ip = *lb.PrivateAddress
	} else {
		ip = *lb.ExternalAddress
	}
	return &corev1.LoadBalancerStatus{
		Ingress: []corev1.LoadBalancerIngress{
			{
				IP: ip,
			},
		},
	}
}

func isImplementedElsewhere(service *corev1.Service) bool {
	return service.Annotations[yawolClassNameAnnotation] != classNameStackit
}
