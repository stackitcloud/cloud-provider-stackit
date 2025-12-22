package ccm

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stackitcloud/stackit-sdk-go/services/loadbalancer"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider/api"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/cmp"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
)

const (
	retryDuration = 10 * time.Second

	// EventReasonSelectedPlanID is a reason for sending an event when plan ID is selected via a flavor
	EventReasonSelectedPlanID = "SelectedPlanID"
)

type Event struct {
	Type    string
	Message string
	Reason  string
}

type MetricsRemoteWrite struct {
	endpoint string
	username string
	password string
}

// LoadBalancer is used for creating and maintaining load balancers.
type LoadBalancer struct {
	client    stackit.LoadbalancerClient
	recorder  record.EventRecorder // set in CloudControllerManager.Initialize
	projectID string
	opts      LoadBalancerOpts
	// metricsRemoteWrite setting this enables remote writing of metrics and nil means it is disabled
	metricsRemoteWrite *MetricsRemoteWrite
}

type LoadBalancerOpts struct {
	API         string            `yaml:"api"`
	NetworkID   string            `yaml:"networkId"`
	ExtraLabels map[string]string `yaml:"extraLabels"`
}

var _ cloudprovider.LoadBalancer = (*LoadBalancer)(nil)

func NewLoadBalancer(client stackit.LoadbalancerClient, projectID string, opts LoadBalancerOpts, metricsRemoteWrite *MetricsRemoteWrite) (*LoadBalancer, error) { //nolint:lll // looks weird when shortened
	// LoadBalancer.recorder is set in CloudControllerManager.Initialize
	return &LoadBalancer{
		client:             client,
		projectID:          projectID,
		opts:               opts,
		metricsRemoteWrite: metricsRemoteWrite,
	}, nil
}

// GetLoadBalancer returns whether the specified load balancer exists, and
// if so, what its status is.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager.
func (l *LoadBalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (
	status *corev1.LoadBalancerStatus, exists bool, err error,
) {
	lb, err := l.client.GetLoadBalancer(ctx, l.projectID, l.GetLoadBalancerName(ctx, clusterName, service))
	switch {
	case stackit.IsNotFound(err):
		// Also for non-STACKIT load balancers in "update" & "updateAndCreate" mode return with no error if not found.
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	return loadBalancerStatus(lb, service), true, nil
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
func (l *LoadBalancer) EnsureLoadBalancer( //nolint:gocyclo // not really complex
	ctx context.Context,
	clusterName string,
	service *corev1.Service,
	nodes []*corev1.Node,
) (*corev1.LoadBalancerStatus, error) {
	name := l.GetLoadBalancerName(ctx, clusterName, service)
	lb, err := l.client.GetLoadBalancer(ctx, l.projectID, name)
	if err != nil && !stackit.IsNotFound(err) {
		return nil, err
	}
	if stackit.IsNotFound(err) {
		return l.createLoadBalancer(ctx, clusterName, service, nodes)
	}

	observabilityOptions, err := l.reconcileObservabilityCredentials(ctx, lb, name)
	if err != nil {
		return nil, fmt.Errorf("reconcile metricsRemoteWrite: %w", err)
	}

	spec, events, err := lbSpecFromService(service, nodes, l.opts, observabilityOptions)
	if err != nil {
		return nil, fmt.Errorf("invalid load balancer specification: %w", err)
	}

	for _, event := range events {
		l.recorder.Event(service, event.Type, event.Reason, event.Message)
	}

	fulfills, immutableChanged := compareLBwithSpec(lb, spec)
	if immutableChanged != nil {
		changeStr := fmt.Sprintf("%q", immutableChanged.field)
		if immutableChanged.annotation != "" {
			changeStr += fmt.Sprintf(" (%q)", immutableChanged.annotation)
		}
		return nil, fmt.Errorf("update to load balancer cannot be fulfilled: API doesn't support changing %s", changeStr)
	}
	if !fulfills {
		credentialsRefBeforeUpdate := getMetricsRemoteWriteRef(lb)
		// We create the update payload from a new spec.
		// However, we need to copy over the version because it is required on every update.
		spec.Version = lb.Version
		spec.Name = &name
		updatePayload := &loadbalancer.UpdateLoadBalancerPayload{
			Errors:          spec.Errors,
			ExternalAddress: spec.ExternalAddress,
			Listeners:       spec.Listeners,
			Name:            spec.Name,
			Networks:        spec.Networks,
			Options:         spec.Options,
			PlanId:          spec.PlanId,
			PrivateAddress:  spec.PrivateAddress,
			Region:          spec.Region,
			Labels:          spec.Labels,
			Status:          loadbalancer.UpdateLoadBalancerPayloadGetStatusAttributeType(spec.Status),
			TargetPools:     spec.TargetPools,
			Version:         spec.Version,
		}
		lb, err = l.client.UpdateLoadBalancer(ctx, l.projectID, name, updatePayload)
		if err != nil {
			return nil, fmt.Errorf("failed to update load balancer: %w", err)
		}
		// Clean up observability credentials if Argus extension is enabled.
		// If the update to the load balancer succeeds but an error is returned (e.g. timeout) we miss our chance to clean up the credentials.
		// At the latest, they will be removed when the service is deleted or Argus is enabled again.
		// This is preferred over listing all credentials in the project on each reconciliation.
		if l.metricsRemoteWrite == nil && credentialsRefBeforeUpdate != nil {
			err = l.client.DeleteCredentials(ctx, l.projectID, *credentialsRefBeforeUpdate)
			if err != nil {
				return nil, fmt.Errorf("delete metricsRemoteWrite credentials %q: %w", *credentialsRefBeforeUpdate, err)
			}
		}
	}

	if *lb.Status == loadbalancer.LOADBALANCERSTATUS_ERROR {
		return nil, fmt.Errorf("the load balancer is in an error state")
	}
	if *lb.Status != loadbalancer.LOADBALANCERSTATUS_READY {
		return nil, api.NewRetryError("waiting for load balancer to become ready. This error is normal while the load balancer starts.", retryDuration)
	}

	return loadBalancerStatus(lb, service), nil
}

func getMetricsRemoteWriteRef(lb *loadbalancer.LoadBalancer) *string {
	if lb.Options != nil && lb.Options.Observability != nil && lb.Options.Observability.Metrics != nil && lb.Options.Observability.Metrics.CredentialsRef != nil {
		return lb.Options.Observability.Metrics.CredentialsRef
	}
	return nil
}

func (l *LoadBalancer) createLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) (
	*corev1.LoadBalancerStatus, error,
) {
	name := l.GetLoadBalancerName(ctx, clusterName, service)
	metricsRemoteWrite, err := l.reconcileObservabilityCredentials(ctx, nil, name)
	if err != nil {
		return nil, fmt.Errorf("reconcile metricsRemoteWrite: %w", err)
	}

	spec, events, err := lbSpecFromService(service, nodes, l.opts, metricsRemoteWrite)
	if err != nil {
		return nil, fmt.Errorf("invalid load balancer specification: %w", err)
	}
	if l.opts.ExtraLabels != nil {
		spec.Labels = ptr.To(l.opts.ExtraLabels)
	}
	for _, event := range events {
		l.recorder.Event(service, event.Type, event.Reason, event.Message)
	}
	spec.Name = &name

	lb, createErr := l.client.CreateLoadBalancer(ctx, l.projectID, spec)
	if createErr != nil {
		return nil, createErr
	}

	if lb.Status == nil || *lb.Status != loadbalancer.LOADBALANCERSTATUS_READY {
		return nil, api.NewRetryError("waiting for load balancer to become ready. This error is normal while the load balancer starts.", retryDuration)
	}

	return loadBalancerStatus(lb, service), nil
}

// UpdateLoadBalancer updates hosts under the specified load balancer.
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager.
//
// It is not called on controller start-up. EnsureLoadBalancer must also ensure to update targets.
func (l *LoadBalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	// only TargetPools are used from spec
	spec, events, err := lbSpecFromService(service, nodes, l.opts, nil)
	if err != nil {
		return fmt.Errorf("invalid service: %w", err)
	}

	for _, event := range events {
		l.recorder.Event(service, event.Type, event.Reason, event.Message)
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
// doesn't exist even if some part of it is still lying around.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (l *LoadBalancer) EnsureLoadBalancerDeleted(
	ctx context.Context, clusterName string, service *corev1.Service,
) error {
	name := l.GetLoadBalancerName(ctx, clusterName, service)

	lb, err := l.client.GetLoadBalancer(ctx, l.projectID, name)
	switch {
	case stackit.IsNotFound(err):
		return nil
	case err != nil:
		return err
	case lb.Status != nil && *lb.Status == loadbalancer.LOADBALANCERSTATUS_TERMINATING:
		return nil
	}

	credentialsRef := getMetricsRemoteWriteRef(lb)
	if credentialsRef != nil {
		// The load balancer is updated to remove the credentials reference and hence enable their deletion.
		for i := range *lb.Listeners {
			// Name is an output only field.
			(*lb.Listeners)[i].Name = nil
		}
		externalAddress := lb.ExternalAddress
		if cmp.UnpackPtr(cmp.UnpackPtr(lb.Options).EphemeralAddress) {
			// An ephemeral external addresses cannot be set during an update (although it is returned by the API).
			externalAddress = nil
		}
		// We can't use lbSpecFromService here because we are lacking the list of nodes.
		// Therefore, we create the update payload "by hand".
		payload := &loadbalancer.UpdateLoadBalancerPayload{
			ExternalAddress: externalAddress,
			Listeners:       lb.Listeners,
			Name:            &name,
			Networks:        lb.Networks,
			Options: &loadbalancer.LoadBalancerOptions{
				AccessControl:      lb.Options.AccessControl,
				EphemeralAddress:   lb.Options.EphemeralAddress,
				Observability:      nil,
				PrivateNetworkOnly: lb.Options.PrivateNetworkOnly,
			},
			TargetPools: lb.TargetPools,
			Version:     lb.Version,
			PlanId:      lb.PlanId,
			Labels:      lb.Labels,
		}
		_, err = l.client.UpdateLoadBalancer(ctx, l.projectID, name, payload)
		if err != nil {
			return fmt.Errorf("failed to update load balancer: %w", err)
		}
		if err = l.client.DeleteCredentials(ctx, l.projectID, *credentialsRef); err != nil {
			return fmt.Errorf("delete metricsRemoteWrite credentials %q: %w", *credentialsRef, err)
		}
	}

	// Delete any observability credentials that are associated with this load balancer but are orphaned.
	// If the load balancer was never created then EnsureLoadBalancerDeleted is never called,
	// in which case we miss the chance to clean up.
	// This is preferred over listing observability credentials in GetLoadBalancer.
	// We perform this list after removing the credentials that are referenced by the load balancer,
	// because they cannot be deleted until they are unreferenced.
	err = l.cleanUpCredentials(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to clean up orphaned observability credentials: %w", err)
	}

	err = l.client.DeleteLoadBalancer(ctx, l.projectID, name)
	// Deleting a load balancer doesn't return an error if the load balancer cannot be found.
	if err != nil {
		return err
	}

	return nil
}

// reconcileObservabilityCredentials update observability credentials if lb has metrics shipping enabled.
// Otherwise it creates new credentials and returns the observability options that must be injected into the load balancer by the caller.
//
// lb can be nil to signal that the load balancer does not exist yet.
func (l *LoadBalancer) reconcileObservabilityCredentials(
	ctx context.Context,
	lb *loadbalancer.LoadBalancer,
	lbName string,
) (*loadbalancer.LoadbalancerOptionObservability, error) {
	if l.metricsRemoteWrite == nil {
		return nil, nil
	}
	var credentialsRef *string
	if lb != nil && lb.Options != nil && lb.Options.Observability != nil && lb.Options.Observability.Metrics != nil {
		credentialsRef = lb.Options.Observability.Metrics.CredentialsRef
	}
	if credentialsRef == nil {
		// If previous reconciliation left credentials behind that are not referenced, we delete them and start fresh.
		err := l.cleanUpCredentials(ctx, lbName)
		if err != nil {
			return nil, fmt.Errorf("failed to clean up orphaned observability credentials: %w", err)
		}

		// create
		payload := loadbalancer.CreateCredentialsPayload{
			DisplayName: &lbName,
			Username:    &l.metricsRemoteWrite.username,
			Password:    &l.metricsRemoteWrite.password,
		}
		c, err := l.client.CreateCredentials(ctx, l.projectID, payload)
		if err != nil {
			return nil, fmt.Errorf("create credentials: %w", err)
		}
		return &loadbalancer.LoadbalancerOptionObservability{
			Metrics: &loadbalancer.LoadbalancerOptionMetrics{
				CredentialsRef: c.Credential.CredentialsRef,
				PushUrl:        &l.metricsRemoteWrite.endpoint,
			},
		}, nil
	}

	// update
	payload := loadbalancer.UpdateCredentialsPayload{
		DisplayName: lb.Name,
		Username:    &l.metricsRemoteWrite.username,
		Password:    &l.metricsRemoteWrite.password,
	}
	if err := l.client.UpdateCredentials(ctx, l.projectID, *credentialsRef, payload); err != nil {
		return nil, fmt.Errorf("update credentials %q: %w", *credentialsRef, err)
	}
	return &loadbalancer.LoadbalancerOptionObservability{
		Metrics: &loadbalancer.LoadbalancerOptionMetrics{
			CredentialsRef: credentialsRef,
			PushUrl:        &l.metricsRemoteWrite.endpoint,
		},
	}, nil
}

// cleanUpCredentials removes all credentials from then API whose displayName matches name.
// This call is expensive.
// Make sure that no credentials are referenced, otherwise the deletion fails.
func (l *LoadBalancer) cleanUpCredentials(ctx context.Context, name string) error {
	res, err := l.client.ListCredentials(ctx, l.projectID)
	if err != nil {
		return fmt.Errorf("failed to list credentials: %w", err)
	}
	if res.Credentials != nil {
		for _, credentials := range *res.Credentials {
			if credentials.DisplayName != nil && *credentials.DisplayName == name {
				err = l.client.DeleteCredentials(ctx, l.projectID, *credentials.CredentialsRef)
				if err != nil {
					return fmt.Errorf("failed to delete credentials %q: %w", *credentials.CredentialsRef, err)
				}
			}
		}
	}
	return nil
}

func loadBalancerStatus(lb *loadbalancer.LoadBalancer, svc *corev1.Service) *corev1.LoadBalancerStatus {
	var ip *string
	if lb.Options != nil && lb.Options.PrivateNetworkOnly != nil && *lb.Options.PrivateNetworkOnly {
		ip = lb.PrivateAddress
	} else {
		ip = lb.ExternalAddress
	}
	var ingresses []corev1.LoadBalancerIngress
	if ip != nil {
		ingress := corev1.LoadBalancerIngress{IP: *ip}
		if ipModeProxy, _ := strconv.ParseBool(svc.Annotations[ipModeProxyAnnotation]); ipModeProxy {
			ingress.IPMode = ptr.To(corev1.LoadBalancerIPModeProxy)
		}
		ingresses = []corev1.LoadBalancerIngress{ingress}
	}
	return &corev1.LoadBalancerStatus{
		Ingress: ingresses,
	}
}
