package stackit

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

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
	// tcpIdleTimeoutAnnotation defines the idle timeout for all TCP ports (including ports with the PROXY protocol).
	tcpIdleTimeoutAnnotation = "lb.stackit.cloud/tcp-idle-timeout"
	// udpIdleTimeoutAnnotation defines the idle timeout for all UDP ports.
	udpIdleTimeoutAnnotation = "lb.stackit.cloud/udp-idle-timeout"
)

const (
	// defaultTCPIdleTimeout is used if the service has no annotation to set the timeout explicitly.
	// This is defined by the CCM and might differ from the default of STACKIT load balancers.
	// For backwards compatibility this is the same as in SKE yawol.
	defaultTCPIdleTimeout = 60 * time.Minute
	// defaultUDPIdleTimeout is used if the service has no annotation to set the timeout explicitly.
	// This is defined by the CCM and might differ from the default of STACKIT load balancers.
	// For backwards compatibility this is the same as in SKE yawol.
	defaultUDPIdleTimeout = 2 * time.Minute
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

	// Parse private network from annotations.
	// TODO: Split into separate function.
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

	// Parse external from annotations.
	// TODO: Split into separate function.
	externalIP, found := service.Annotations[externalIPAnnotation]
	yawolExternalIP, yawolFound := service.Annotations[yawolExistingFloatingIPAnnotation]
	if found && yawolFound && externalIP != yawolExternalIP {
		return nil, fmt.Errorf(
			"incompatible values for annotations %s and %s", yawolExistingFloatingIPAnnotation, externalIPAnnotation,
		)
	}
	lb.Options.EphemeralAddress = utils.Ptr(false)
	if !found && !yawolFound && !*lb.Options.PrivateNetworkOnly {
		lb.Options.EphemeralAddress = utils.Ptr(true)
	}
	if !found && yawolFound {
		externalIP = yawolExternalIP
	}
	if !*lb.Options.PrivateNetworkOnly && !*lb.Options.EphemeralAddress {
		ip, err := netip.ParseAddr(externalIP)
		if err != nil {
			return nil, fmt.Errorf("invalid format for external IP: %w", err)
		}
		if ip.Is6() {
			return nil, fmt.Errorf("external IP must be an IPv4 address")
		}
		lb.ExternalAddress = &externalIP
	}

	// Parse TCP idle timeout from annotations.
	// TODO: Split into separate function.
	tcpIdleTimeout := defaultTCPIdleTimeout
	var yawolTCPIdleTimeout time.Duration
	_, found = service.Annotations[tcpIdleTimeoutAnnotation]
	_, yawolFound = service.Annotations[yawolTCPIdleTimeoutAnnotation]
	if found {
		var err error
		tcpIdleTimeout, err = time.ParseDuration(service.Annotations[tcpIdleTimeoutAnnotation])
		if err != nil {
			return nil, fmt.Errorf("invalid format for annotation %s: %w", tcpIdleTimeoutAnnotation, err)
		}
	}
	if yawolFound {
		var err error
		yawolTCPIdleTimeout, err = time.ParseDuration(service.Annotations[yawolTCPIdleTimeoutAnnotation])
		// Ignore error for backwards-compatibility with the yawol cloud controller.
		if err == nil && !found {
			tcpIdleTimeout = yawolTCPIdleTimeout
		}
	}
	if found && yawolFound && tcpIdleTimeout != yawolTCPIdleTimeout {
		return nil, fmt.Errorf("incompatible values for annotations %s and %s", tcpIdleTimeoutAnnotation, yawolTCPIdleTimeoutAnnotation)
	}

	// Parse UDP idle timeout from annotations.
	// TODO: Split into separate function.
	udpIdleTimeout := defaultUDPIdleTimeout
	var yawolUDPIdleTimeout time.Duration
	_, found = service.Annotations[udpIdleTimeoutAnnotation]
	_, yawolFound = service.Annotations[yawolUDPIdleTimeoutAnnotation]
	if found {
		var err error
		udpIdleTimeout, err = time.ParseDuration(service.Annotations[udpIdleTimeoutAnnotation])
		if err != nil {
			return nil, fmt.Errorf("invalid format for annotation %s: %w", udpIdleTimeoutAnnotation, err)
		}
	}
	if yawolFound {
		var err error
		yawolUDPIdleTimeout, err = time.ParseDuration(service.Annotations[yawolUDPIdleTimeoutAnnotation])
		// Ignore error for backwards-compatibility with the yawol cloud controller.
		if err == nil && !found {
			udpIdleTimeout = yawolUDPIdleTimeout
		}
	}
	if found && yawolFound && udpIdleTimeout != yawolUDPIdleTimeout {
		return nil, fmt.Errorf("incompatible values for annotations %s and %s", udpIdleTimeoutAnnotation, yawolUDPIdleTimeoutAnnotation)
	}

	// Parse PROXY protocol from annotations.
	// TODO: Split into separate function.
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
			// Use a descriptive name for a port without name. This only applies for
			// services with a single port. A service with more than one port must
			// have names set for all ports.
			name = fmt.Sprintf("port-%s-%d", port.Protocol, port.Port)
		}

		protocol := ""
		var tcpOptions *loadbalancer.OptionsTCP
		var udpOptions *loadbalancer.OptionsUDP

		switch port.Protocol { //nolint:exhaustive // There are protocols that we do not support.
		case corev1.ProtocolTCP:
			if proxyProtocolEnableForPort(tcpProxyProtocolEnabled, tcpProxyProtocolPortFilter, port.Port) {
				protocol = lbapi.ProtocolTCPProxy
			} else {
				protocol = lbapi.ProtocolTCP
			}
			tcpOptions = &loadbalancer.OptionsTCP{
				IdleTimeout: utils.Ptr(fmt.Sprintf("%.0fs", tcpIdleTimeout.Seconds())),
			}
		case corev1.ProtocolUDP:
			protocol = lbapi.ProtocolUDP
			udpOptions = &loadbalancer.OptionsUDP{
				IdleTimeout: utils.Ptr(fmt.Sprintf("%.0fs", udpIdleTimeout.Seconds())),
			}
		default:
			return nil, fmt.Errorf("unsupported protocol %q for port %q", port.Protocol, port.Name)
		}

		listeners = append(listeners, loadbalancer.Listener{
			DisplayName: &name,
			Port:        utils.Ptr(int64(port.Port)),
			TargetPool:  &name,
			Protocol:    &protocol,
			Tcp:         tcpOptions,
			Udp:         udpOptions,
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

// resultImmutableChanged denotes that at least one property that cannot be changed did change.
// Attempting an update will fail.
type resultImmutableChanged struct {
	field string
}

// compareLBwithSpec checks whether the load balancer fulfills the specification.
// If immutableChanged is not nil then spec differs from lb such that an update will fail.
// Otherwise fulfills will indicate whether an update is necessary.
func compareLBwithSpec(lb *loadbalancer.LoadBalancer, spec *loadbalancer.CreateLoadBalancerPayload) (fulfills bool, immutableChanged *resultImmutableChanged) { //nolint:gocyclo,funlen,lll // It is long but not complex.
	// If a mutable property has changed we must still check the rest of the object because if there is an immutable change it must always be returned.
	fulfills = true

	if cmp.UnpackPtr(cmp.UnpackPtr(lb.Options).PrivateNetworkOnly) != cmp.UnpackPtr(cmp.UnpackPtr(spec.Options).PrivateNetworkOnly) {
		return false, &resultImmutableChanged{field: ".options.privateNetworkOnly"}
	}

	if cmp.UnpackPtr(spec.ExternalAddress) != "" {
		// lb.ExternalAddress is set to the ephemeral IP if the load balancer is ephemeral, while spec will never contain an ephemeral IP.
		// So we only compare them if the spec has a static IP.
		if !cmp.PtrValEqual(lb.ExternalAddress, spec.ExternalAddress) {
			return false, &resultImmutableChanged{field: ".externalAddress"}
		}
		if cmp.UnpackPtr(cmp.UnpackPtr(lb.Options).EphemeralAddress) {
			// Promote an ephemeral IP to a static IP.
			fulfills = false
		}
	} else if !cmp.UnpackPtr(cmp.UnpackPtr(lb.Options).PrivateNetworkOnly) &&
		!cmp.UnpackPtr(cmp.UnpackPtr(lb.Options).EphemeralAddress) {
		// Demotion is not allowed by the load balancer API.
		return false, &resultImmutableChanged{field: ".options.ephemeralAddress"}
	}

	if cmp.LenSlicePtr(lb.Listeners) != cmp.LenSlicePtr(spec.Listeners) {
		fulfills = false
	} else if lb.Listeners != nil && spec.Listeners != nil {
		for i, x := range *lb.Listeners {
			y := (*spec.Listeners)[i]
			if !cmp.PtrValEqual(x.DisplayName, y.DisplayName) {
				fulfills = false
			}
			if !cmp.PtrValEqual(x.Port, y.Port) {
				fulfills = false
			}
			if !cmp.PtrValEqual(x.Protocol, y.Protocol) {
				fulfills = false
			}
			if !cmp.PtrValEqual(x.TargetPool, y.TargetPool) {
				fulfills = false
			}
			if (cmp.UnpackPtr(x.Protocol) == lbapi.ProtocolTCP || cmp.UnpackPtr(x.Protocol) == lbapi.ProtocolTCPProxy) &&
				!cmp.PtrValEqualFn(x.Tcp, y.Tcp, func(a, b loadbalancer.OptionsTCP) bool {
					return cmp.PtrValEqual(a.IdleTimeout, b.IdleTimeout)
				}) {
				fulfills = false
			}
			if cmp.UnpackPtr(x.Protocol) == lbapi.ProtocolUDP && !cmp.PtrValEqualFn(x.Udp, y.Udp, func(a, b loadbalancer.OptionsUDP) bool {
				return cmp.PtrValEqual(a.IdleTimeout, b.IdleTimeout)
			}) {
				fulfills = false
			}
		}
	}

	if cmp.LenSlicePtr(lb.Networks) != cmp.LenSlicePtr(spec.Networks) {
		return false, &resultImmutableChanged{field: "len(.networks)"}
	}
	if cmp.LenSlicePtr(lb.Networks) > 0 {
		for i, x := range *lb.Networks {
			y := (*spec.Networks)[i]
			if !cmp.PtrValEqual(x.NetworkId, y.NetworkId) {
				return false, &resultImmutableChanged{field: fmt.Sprintf(".networks[%d].networkId", i)}
			}
			if !cmp.PtrValEqual(x.Role, y.Role) {
				return false, &resultImmutableChanged{field: fmt.Sprintf(".networks[%d].role", i)}
			}
		}
	}

	if cmp.LenSlicePtr(lb.TargetPools) != cmp.LenSlicePtr(spec.TargetPools) {
		fulfills = false
	} else if lb.TargetPools != nil && spec.TargetPools != nil {
		for i, x := range *lb.TargetPools {
			y := (*spec.TargetPools)[i]
			if !cmp.PtrValEqual(x.Name, y.Name) {
				fulfills = false
			}
			if !cmp.PtrValEqual(x.TargetPort, y.TargetPort) {
				fulfills = false
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
				fulfills = false
			}
			if x.Targets == nil || y.Targets == nil {
				// At this point one pointer is nil.
				// We consider nil pointer to be equal to a nil slice and an empty slice.
				if cmp.LenSlicePtr(x.Targets) != cmp.LenSlicePtr(y.Targets) {
					fulfills = false
				}
			} else if !cmp.SliceEqualUnordered(*x.Targets, *y.Targets, func(a, b loadbalancer.Target) bool {
				if !cmp.PtrValEqual(a.DisplayName, b.DisplayName) {
					return false
				}
				if !cmp.PtrValEqual(a.Ip, b.Ip) {
					return false
				}
				return true
			}) {
				fulfills = false
			}
		}
	}

	if !cmp.SliceEqual(
		cmp.UnpackPtr(cmp.UnpackPtr(cmp.UnpackPtr(lb.Options).AccessControl).AllowedSourceRanges),
		cmp.UnpackPtr(cmp.UnpackPtr(cmp.UnpackPtr(spec.Options).AccessControl).AllowedSourceRanges),
	) {
		return false, &resultImmutableChanged{field: ".options.accessControl"}
	}

	return fulfills, immutableChanged
}
