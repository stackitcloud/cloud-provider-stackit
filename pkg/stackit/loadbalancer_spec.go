package stackit

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	"github.com/stackitcloud/stackit-sdk-go/services/loadbalancer"
	corev1 "k8s.io/api/core/v1"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/cmp"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/lbapi"
)

const (
	// internalLBAnnotation defines whether the load balancer should be exposed via a public IP.
	// Default is false (i.e. exposed).
	internalLBAnnotation = "lb.stackit.cloud/internal-lb"
	// externalIPAnnotation references an OpenStack floating IP that should be used for the load balancer.
	// If set it will be used instead of an ephemeral IP.
	// The IP must be created by the user.
	// When the service is deleted, the floating IP will not be deleted.
	// The IP is ignored if the load balancer internal.
	externalIPAnnotation = "lb.stackit.cloud/external-address"
	// tcpProxyProtocolEnabledAnnotation enables the TCP proxy protocol for TCP ports.
	tcpProxyProtocolEnabledAnnotation = "lb.stackit.cloud/tcp-proxy-protocol"
	// tcpProxyProtocolPortFilterAnnotation defines which port use the TCP proxy protocol.
	// Only takes effect if TCP proxy protocol is enabled.
	// If the annotation is not present then all TCP ports use the TCP proxy protocol.
	// Has no effect on UDP ports.
	tcpProxyProtocolPortFilterAnnotation = "lb.stackit.cloud/tcp-proxy-protocol-ports-filter"
)

// proxyProtocolEnableForPort determines whether portNumber should use the TCP proxy protocol (instead of TCP).
func proxyProtocolEnableForPort(tcpProxyProtocolEnabled bool, tcpProxyProtocolPortFilter []uint16, portNumber int32) bool {
	if !tcpProxyProtocolEnabled {
		return false
	}
	if tcpProxyProtocolPortFilter != nil {
		for _, port := range tcpProxyProtocolPortFilter {
			if int32(port) == portNumber {
				return true
			}
		}
		return false
	}
	return true
}

// lbSpecFromService returns a load balancer specification in the form of a create payload matching the specification of the service, nodes and network.
// The property name will be empty and must be set by the caller to produce a valid payload for the API.
// An error is returned if the service has invalid options.
func lbSpecFromService(service *corev1.Service, nodes []*corev1.Node, networkID string) ( //nolint:funlen,gocyclo // It is long but not complex.
	*loadbalancer.CreateLoadBalancerPayload, error,
) {
	lb := &loadbalancer.CreateLoadBalancerPayload{
		Options: &loadbalancer.LoadBalancerOptions{},
		Networks: &[]loadbalancer.Network{
			{
				Role:      utils.Ptr("ROLE_LISTENERS_AND_TARGETS"),
				NetworkId: &networkID,
			},
		},
	}

	lb.Options.PrivateNetworkOnly = utils.Ptr(false)
	var internal *bool
	var yawolInternal *bool
	if internalStr, found := service.Annotations[internalLBAnnotation]; found {
		var err error
		i, err := strconv.ParseBool(internalStr)
		internal = &i
		if err != nil {
			return nil, fmt.Errorf("invalid bool value %q for annotation %q: %w", internalStr, internalLBAnnotation, err)
		}
		lb.Options.PrivateNetworkOnly = internal
	}
	if _, found := service.Annotations[yawolInternalLBAnnotation]; found {
		yawolInternal = utils.Ptr(true)
		lb.Options.PrivateNetworkOnly = yawolInternal
	}
	if yawolInternal != nil && internal != nil && *yawolInternal == *internal {
		return nil, fmt.Errorf("incompatible values for annotations %s and %s", yawolInternalLBAnnotation, internalLBAnnotation)
	}

	externalIP, found := service.Annotations[externalIPAnnotation]
	yawolExternalIP, yawolFound := service.Annotations[yawolExistingFloatingIPAnnotation]
	if found && yawolFound && externalIP != yawolExternalIP {
		return nil, fmt.Errorf(
			"incompatible values for annotations %s and %s", yawolExistingFloatingIPAnnotation, externalIPAnnotation,
		)
	}
	if !found && !yawolFound && !*lb.Options.PrivateNetworkOnly {
		// TODO: make this optional once the load balancer API supports ephemeral IPs.
		return nil, fmt.Errorf(
			"service is missing annotation %s or the deprecated annotation %s", externalIPAnnotation, yawolExternalIP,
		)
	}
	if !found && yawolFound {
		externalIP = yawolExternalIP
	}
	if !*lb.Options.PrivateNetworkOnly {
		ip, err := netip.ParseAddr(externalIP)
		if err != nil {
			return nil, fmt.Errorf("invalid format for external IP: %w", err)
		}
		if ip.Is6() {
			return nil, fmt.Errorf("external IP must be an IPv4 address")
		}
		lb.ExternalAddress = &externalIP
	}

	tcpProxyProtocolEnabled := false
	yawolTCPProxyProtocolEnabled := false
	// tcpProxyProtocolPortFilter allows all ports if nil.
	var tcpProxyProtocolPortFilter []uint16
	_, found = service.Annotations[tcpProxyProtocolEnabledAnnotation]
	_, yawolFound = service.Annotations[yawolTCPProxyProtocolEnabledAnnotation]
	if found {
		var err error
		tcpProxyProtocolEnabled, err = strconv.ParseBool(service.Annotations[tcpProxyProtocolEnabledAnnotation])
		if err != nil {
			return nil, fmt.Errorf("invalid bool value for annotation %s: %w", tcpProxyProtocolEnabledAnnotation, err)
		}
	}
	if yawolFound {
		// For backwards-compatibility, we don't error on non-boolean values.
		e, _ := strconv.ParseBool(service.Annotations[yawolTCPProxyProtocolEnabledAnnotation])
		yawolTCPProxyProtocolEnabled = e
	}
	if found && yawolFound && yawolTCPProxyProtocolEnabled != tcpProxyProtocolEnabled {
		return nil, fmt.Errorf(
			"incompatible values for annotations %s and %s", yawolTCPProxyProtocolEnabledAnnotation, tcpProxyProtocolEnabledAnnotation,
		)
	}
	if yawolFound && !found {
		tcpProxyProtocolEnabled = yawolTCPProxyProtocolEnabled
	}
	if tcpProxyProtocolEnabled {
		proxyPorts, found := service.Annotations[tcpProxyProtocolPortFilterAnnotation]
		yawolProxyPorts, yawolFound := service.Annotations[yawolTCPProxyProtocolPortFilterAnnotation]
		// We compare the ports string-based for simplicity.
		if found && yawolFound && proxyPorts != yawolProxyPorts {
			return nil, fmt.Errorf(
				"incompatible values for annotations %s and %s", yawolTCPProxyProtocolPortFilterAnnotation, tcpProxyProtocolPortFilterAnnotation,
			)
		}
		if yawolFound && !found {
			proxyPorts = yawolProxyPorts
		}
		if found || yawolFound {
			tcpProxyProtocolPortFilter = []uint16{}
			if strings.TrimSpace(proxyPorts) != "" {
				for i, portStr := range strings.Split(proxyPorts, ",") {
					port, err := strconv.ParseUint(strings.TrimSpace(portStr), 10, 16)
					if err != nil {
						return nil, fmt.Errorf(
							"invalid port %q at position %d in annotation %q: %w", portStr, i, tcpProxyProtocolPortFilterAnnotation, err,
						)
					}
					tcpProxyProtocolPortFilter = append(tcpProxyProtocolPortFilter, uint16(port))
				}
			}
		}
	}

	targets := []loadbalancer.Target{}
	for i := range nodes {
		node := nodes[i]
		for j := range node.Status.Addresses {
			address := node.Status.Addresses[j]
			if address.Type == corev1.NodeInternalIP {
				targets = append(targets, loadbalancer.Target{
					DisplayName: &node.Name,
					Ip:          &address.Address,
				})
				break
			}
			// If a node doesn't have an internal IP it is ignored as a target.
		}
	}

	listeners := []loadbalancer.Listener{}
	targetPools := []loadbalancer.TargetPool{}
	for i := range service.Spec.Ports {
		port := service.Spec.Ports[i]
		name := port.Name
		if name == "" {
			// Technically, only port-0 will be set here.
			// A service with more than one port must have names set for all ports.
			name = fmt.Sprintf("port-%d", i)
		}

		protocol := ""
		switch port.Protocol { //nolint:exhaustive // There are protocols that we do not support.
		case corev1.ProtocolTCP:
			if proxyProtocolEnableForPort(tcpProxyProtocolEnabled, tcpProxyProtocolPortFilter, port.Port) {
				protocol = lbapi.ProtocolTCPProxy
			} else {
				protocol = lbapi.ProtocolTCP
			}
		case corev1.ProtocolUDP:
			protocol = lbapi.ProtocolUDP
		default:
			return nil, fmt.Errorf("unsupported protocol %q for port %q", port.Protocol, port.Name)
		}
		listeners = append(listeners, loadbalancer.Listener{
			DisplayName: &name,
			Port:        utils.Ptr(int64(port.Port)),
			TargetPool:  &port.Name,
			Protocol:    &protocol,
		})

		targetPools = append(targetPools, loadbalancer.TargetPool{
			Name:       &name,
			TargetPort: utils.Ptr(int64(port.NodePort)),
			Targets:    &targets,
		})
	}
	lb.Listeners = &listeners
	lb.TargetPools = &targetPools

	lb.Options.AccessControl = &loadbalancer.LoadbalancerOptionAccessControl{}
	// For backwards-compatibility, the spec takes precedence over the annotation.
	if sourceRanges, found := service.Annotations[yawolLoadBalancerSourceRangesAnnotation]; found {
		r := strings.Split(sourceRanges, ",")
		lb.Options.AccessControl.AllowedSourceRanges = &r
	}
	if len(service.Spec.LoadBalancerSourceRanges) > 0 {
		lb.Options.AccessControl.AllowedSourceRanges = &service.Spec.LoadBalancerSourceRanges
	}

	if err := checkUnsupportedAnnotations(service); err != nil {
		return nil, err
	}

	return lb, nil
}

func checkUnsupportedAnnotations(service *corev1.Service) error {
	for _, a := range yawolUnsupportedAnnotations {
		if _, found := service.Annotations[a]; found {
			return fmt.Errorf("unsupported annotation %s", a)
		}
	}
	return nil
}

var errorTargetPoolChanged = errors.New("one or multiple target pools have changed")

type errorImmutableFieldChanged struct {
	field string
}

var _ error = errorImmutableFieldChanged{}

func (err errorImmutableFieldChanged) Error() string {
	return fmt.Sprintf("%q has changed", err.field)
}

// lbFulfillsSpec checks whether the load balancer fulfills the specification.
// If error is nil, then the load balancer fulfills the specification and no update in necessary.
// Otherwise error is either errorTargetPoolChanged or errorImmutableFieldChanged.
// When both errorTargetPoolChanged and errorImmutableFieldChanged could be returned, either of the two is returned.
func lbFulfillsSpec(lb *loadbalancer.LoadBalancer, spec *loadbalancer.CreateLoadBalancerPayload) error { //nolint:gocyclo // It is long but not complex.
	if !cmp.PtrValEqual(lb.ExternalAddress, spec.ExternalAddress) {
		return errorImmutableFieldChanged{field: ".ExternalAddress"}
	}

	if cmp.LenSlicePtr(lb.Listeners) != cmp.LenSlicePtr(spec.Listeners) {
		return errorImmutableFieldChanged{field: "len(.Listeners)"}
	}
	if cmp.LenSlicePtr(lb.Listeners) > 0 {
		for i, x := range *lb.Listeners {
			y := (*spec.Listeners)[i]
			if !cmp.PtrValEqual(x.DisplayName, y.DisplayName) {
				return errorImmutableFieldChanged{field: fmt.Sprintf(".Listeners[%d].DisplayName", i)}
			}
			if !cmp.PtrValEqual(x.Port, y.Port) {
				return errorImmutableFieldChanged{field: fmt.Sprintf(".Listeners[%d].Port", i)}
			}
			if !cmp.PtrValEqual(x.Protocol, y.Protocol) {
				return errorImmutableFieldChanged{field: fmt.Sprintf(".Listeners[%d].Protocol", i)}
			}
			if !cmp.PtrValEqual(x.TargetPool, y.TargetPool) {
				return errorImmutableFieldChanged{field: fmt.Sprintf(".Listeners[%d].TargetPool", i)}
			}
		}
	}

	if cmp.LenSlicePtr(lb.Networks) != cmp.LenSlicePtr(spec.Networks) {
		return errorImmutableFieldChanged{field: "len(.Networks)"}
	}
	if cmp.LenSlicePtr(lb.Networks) > 0 {
		for i, x := range *lb.Networks {
			y := (*spec.Networks)[i]
			if !cmp.PtrValEqual(x.NetworkId, y.NetworkId) {
				return errorImmutableFieldChanged{field: fmt.Sprintf(".Networks[%d].NetworkId", i)}
			}
			if !cmp.PtrValEqual(x.Role, y.Role) {
				return errorImmutableFieldChanged{field: fmt.Sprintf(".Networks[%d].Role", i)}
			}
		}
	}

	if cmp.LenSlicePtr(lb.TargetPools) != cmp.LenSlicePtr(spec.TargetPools) {
		return errorImmutableFieldChanged{field: "len(.TargetPools)"}
	}
	if cmp.LenSlicePtr(lb.TargetPools) > 0 {
		for i, x := range *lb.TargetPools {
			y := (*spec.TargetPools)[i]
			if !cmp.PtrValEqual(x.Name, y.Name) {
				return errorImmutableFieldChanged{field: fmt.Sprintf(".TargetPools[%d].Name", i)}
			}
			if !cmp.PtrValEqual(x.TargetPort, y.TargetPort) {
				return errorTargetPoolChanged
			}
			if !cmp.PtrValEqualFn(x.ActiveHealthCheck, y.ActiveHealthCheck, func(a, b loadbalancer.ActiveHealthCheck) bool {
				if !cmp.PtrValEqual(a.HealthyThreshold, b.HealthyThreshold) {
					return false
				}
				if !cmp.PtrValEqual(a.Interval, b.Interval) {
					return false
				}
				if !cmp.PtrValEqual(a.IntervalJitter, b.IntervalJitter) {
					return false
				}
				if !cmp.PtrValEqual(a.Timeout, b.Timeout) {
					return false
				}
				if !cmp.PtrValEqual(a.UnhealthyThreshold, b.UnhealthyThreshold) {
					return false
				}
				return true
			}) {
				return errorTargetPoolChanged
			}
			if !cmp.SliceEqualUnordered(*x.Targets, *y.Targets, func(a, b loadbalancer.Target) bool {
				if !cmp.PtrValEqual(a.DisplayName, b.DisplayName) {
					return false
				}
				if !cmp.PtrValEqual(a.Ip, b.Ip) {
					return false
				}
				return true
			}) {
				return errorTargetPoolChanged
			}
		}
	}

	if cmp.UnpackPtr(cmp.UnpackPtr(lb.Options).PrivateNetworkOnly) != cmp.UnpackPtr(cmp.UnpackPtr(spec.Options).PrivateNetworkOnly) {
		return errorImmutableFieldChanged{field: ".Options.PrivateNetworkOnly"}
	}

	if !cmp.SliceEqual(
		cmp.UnpackPtr(cmp.UnpackPtr(cmp.UnpackPtr(lb.Options).AccessControl).AllowedSourceRanges),
		cmp.UnpackPtr(cmp.UnpackPtr(cmp.UnpackPtr(spec.Options).AccessControl).AllowedSourceRanges),
	) {
		return errorImmutableFieldChanged{field: ".Options.AccessControl"}
	}

	return nil
}
