package ccm

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
	// servicePlanAnnotation defines the service plan to be used when creating an LB
	servicePlanAnnotation = "lb.stackit.cloud/service-plan-id"
	// ipModeProxyAnnotation defines whether the service status should reflect that the load balancer is of type proxy.
	ipModeProxyAnnotation = "lb.stackit.cloud/ip-mode-proxy"
	// sessionPersistenceWithSourceIP defines whether the load balancer should use the source IP address for load balancing.
	// When set to true, all connections from the same source IP are consistently routed to the same target.
	// This setting changes the load balancing algorithm to Maglev.
	// Note: This only works reliably when externalTrafficPolicy: Local is set on the Service,
	// and each node has exactly one backing pod. Otherwise, session persistence may break.
	sessionPersistenceWithSourceIP = "lb.stackit.cloud/session-persistence-with-source-ip"
	// listenerNetworkAnnotation defines the network in which the load balancer should listen.
	// If not set, the SKE network is used for listening.
	// The value must be a network ID, not a subnet.
	// The annotation can neither be changed nor be added or removed after service creation.
	listenerNetworkAnnotation = "lb.stackit.cloud/listener-network"
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

const eventReasonYawolAnnotationPresent = "YawolAnnotationPresent"

var availablePlanIDs = []string{"p10", "p50", "p250", "p750"}

// the default plan ID when no plan ID annotation is found
var defaultServicePlan = "p10"

var flavorsMap = map[string]string{
	"85f57dd5-712b-489d-a0e3-4898c3962930": "p10",  // t1.2
	"cd49f4fd-1e48-497f-91ad-79894c8b95e4": "p50",  // s1a.4d
	"72f11e14-2825-471d-a237-b1afa775fdad": "p250", // s1a.8d
	"53408825-7086-48c2-9126-cafdeb2d35d6": "p750", // s1a.16d
}

var appoximateFlavorsMap = map[string]string{
	"2faeefeb-efe7-4f8b-9e52-3246a5d709f0": "p50",  // s1a.2d
	"9b6bfa7b-bb80-4da8-aa10-ddd4cfaaa1a1": "p750", // s1a.32d
	"8936d6a5-30bb-4012-834c-29c599800e53": "p750", // s1a.60d

	"75e8134a-e1de-4052-b3be-75c5157c47c6": "p250", // b1.1
	"1493fabc-3e5c-4992-82fc-d43e2c33902a": "p750", // b1.2
	"f77046c4-6c41-452c-9983-7264151252fa": "p750", // b1.3
	"f778f21f-b0a7-4ae0-88e9-917f01d6fb52": "p750", // b1.4

	"49902b99-b428-4e6a-ad34-d8b9e719390f": "p250", // b1a.1d
	"ce99338f-afc2-4966-89e2-34e494d89e4b": "p750", // b1a.2d
	"2e364c23-ee61-451c-841c-8fa25573ae9d": "p750", // b1a.4d
	"c1e2def6-e182-4bf6-a0f9-9b5b453eb55a": "p750", // b1a.8d
	"fda4f402-6d43-4db5-bcf1-384596f237bb": "p750", // b1a.16d
	"704c07a3-1308-4cdd-b8f3-0892589cb99c": "p750", // b1a.32d
	"696d8b7a-6aaa-456c-853b-11a7ba490b66": "p750", // b1a.60d
	"8b46ca04-e7ec-4f01-a2ed-67e75e3fe04f": "p750", // b1a.120d

	"882a98ba-d47f-4a52-bd85-ccbc2b08f8f8": "p250", // b2i.1d
	"6c1b79d7-b344-407e-808d-476187c7dcd6": "p750", // b2i.2d
	"013451f5-4c26-4464-84e6-cc5f1c8b0f8a": "p750", // b2i.4d
	"2787f539-a8b9-40d3-873d-6db51a2edb41": "p750", // b2i.8d
	"7bd9d46f-7c3b-4089-88e5-fca17581295e": "p750", // b2i.16d
	"562af0ba-2540-4b49-943e-0beb6c9afa04": "p750", // b2i.30d
	"a09e6576-4f74-4a4d-963a-05ec49e27f18": "p750", // b2i.36d

	"7d1572e1-11c9-4872-8ce8-4b953cdf6fb3": "p50",  // c1.1
	"5fe737c2-18d8-43c6-bb11-dc9c97ff9515": "p50",  // c1.2
	"8512c5f9-4426-47f1-a9dc-5c5a5a798b54": "p250", // c1.3
	"ecb39de6-8b6c-431e-8455-9d857639be92": "p750", // c1.4
	"442e31fa-654a-4f76-b7c2-4802592f9cc7": "p750", // c1.5

	"6f65263f-0902-47ca-8761-6e449648c8f0": "p50",  // c1a.1d
	"a9704593-dc26-45b7-8b1c-a37bf42d253e": "p50",  // c1a.2d
	"d04236f9-4740-4058-9695-0a80a9b3a9b0": "p250", // c1a.4d
	"cac16b39-a179-43c5-b5e5-ad22eca1c87c": "p750", // c1a.8d
	"381b5633-b064-41aa-af78-cd1bd318a0e1": "p750", // c1a.16d

	"ef66543f-3225-48b0-ab42-4cfda07668b8": "p50",  // c2i.1
	"0c69d386-ca5e-4720-8812-225bbf4d4879": "p50",  // c2i.2
	"a03cf8cd-f5e4-4897-b639-41b4a1a46dc6": "p250", // c2i.4
	"cee66b47-8465-469d-bb61-7a23073c3488": "p750", // c2i.8
	"5fa8e67c-4259-4353-8e29-dece88c3a394": "p750", // c2i.16

	"64d695be-04b8-4f14-b020-712ef0e30a6b": "p50",  // g1.1
	"3b11b27e-6c73-470d-b595-1d85b95a8cdf": "p250", // g1.2
	"028a4cf9-d9de-4706-a6d2-3ec9a456a736": "p250", // g1.3
	"21ff0965-d385-4e90-9ae4-e1ac8ca8f569": "p750", // g1.4
	"d1f51f86-3fa3-46a1-9e9f-b8b1308f039e": "p750", // g1.5

	"17837ed5-515a-457f-b36b-531fdb861b8a": "p50",  // g1a.1d
	"c6b4adc7-d101-48d4-a2ea-d77cbaa63768": "p250", // g1a.2d
	"c995089f-8d81-4085-be7b-dc2f7ad3f05f": "p250", // g1a.4d
	"cfd6f5f6-b2da-49db-9f1c-4f2ef4c8e831": "p750", // g1a.8d
	"816c3d62-3526-47c6-90b4-7c47318f7526": "p750", // g1a.16d
	"84a73cca-db0b-4d56-837f-5e5422520d51": "p750", // g1a.32d
	"b9f4c5f0-49d7-48a1-ab41-34000f00664b": "p750", // g1a.60d

	"8d811c25-a261-4cbe-aadf-6c2d9667c842": "p50",  // g1r.1d
	"8a7bd5b4-7ac6-414b-ac62-a6c43229038a": "p250", // g1r.2d
	"e3abfbba-b9fe-4973-ac92-2856f489d09a": "p250", // g1r.4d
	"f5bfad0d-22d2-4e47-a807-8413e6d0818f": "p750", // g1r.8d
	"396a0814-b339-4e9f-8d2f-ccef53937541": "p750", // g1r.16d
	"a98b686a-4207-4bc2-902a-6f303da7b043": "p750", // g1r.30d

	"474e2367-9c96-4fc0-ac41-eac7f59a1c7b": "p50",  // g2i.1
	"410cc4c1-0684-47fa-9e72-866f1044a330": "p50",  // g2i.1s
	"b7aa1635-3726-4924-9d73-18b9683fb67a": "p250", // g2i.2
	"79021845-f6de-46f0-be07-17835930d030": "p250", // g2i.2s
	"8d705710-c7a8-4e64-aa96-87add166f42d": "p250", // g2i.4
	"88883131-9afa-43ad-bc4e-77b79494e89f": "p250", // g2i.4s
	"9c0a9d2d-79e9-4b99-a72f-e15c0d987e7d": "p750", // g2i.8
	"f6303887-f07c-4976-9198-0eb0dd964b18": "p750", // g2i.8s
	"b1ca70ea-4da7-441d-8aef-8337c5f81fb2": "p750", // g2i.16
	"901a56b3-4c98-4725-986a-2aa081b9c955": "p750", // g2i.16s

	"04b9be2d-afee-4f32-874b-8356b96ebb0b": "p50",  // m1.1
	"39d1344f-91dc-412a-a3b4-72e61b6c6eaa": "p250", // m1.2
	"baa5374c-cb97-47b9-be8c-e79103b3d097": "p750", // m1.3
	"ccd8f298-d885-4c8b-bf40-414ee8f39967": "p750", // m1.4
	"acd9945a-f94a-4e64-a4a3-81f587b596cf": "p750", // m1.5

	"osAyv1W3z2TU5D6h": "p10", // m1.amphora

	"a24a1a07-4c66-4030-bbfd-68368e4bb8be": "p50",  // m1a.1d
	"d61f0a42-a3e0-4723-b78a-d51d3d719ade": "p250", // m1a.2d
	"27b97158-43d6-44a1-8cf2-d95989f2cc07": "p750", // m1a.4d
	"9d76cf5e-1354-446f-8ca1-0d704bee89ca": "p750", // m1a.8d
	"e6897bc6-6a21-451c-b6cb-d80725781446": "p750", // m1a.16d
	"6fedeb8b-0536-4fbb-80b3-3791d00346b2": "p750", // m1a.32d
	"e772e068-6400-4664-8813-811e835b169e": "p750", // m1a.60d
	"ae6ea4bb-f26c-41c1-a974-ff422601c0b6": "p750", // m1a.120d

	"aa603f7b-4214-486c-81ce-369535cef8ed": "p50",  // m2i.1
	"b4be83e0-475c-4255-a5ef-e6876c413a09": "p250", // m2i.2
	"79b7b9a7-a062-4aab-9861-ba93c1935a68": "p750", // m2i.4
	"309471e2-21e8-4384-8ad9-484bf56372b3": "p750", // m2i.8
	"c4d49baf-fcfa-4ec1-8160-9ec7e57123fc": "p750", // m2i.16

	"d651f154-7fc9-4bf6-a34a-a32e3dd57c5d": "p50",  // s1.2
	"8b2c5d9f-e7da-4cd2-a359-e652817845fe": "p50",  // s1.3
	"709d7f73-41f0-4295-94d5-0f0cf351c93b": "p250", // s1.4
	"178cf1ad-abcb-46c2-a045-73bc935f100e": "p750", // s1.5
	"482434d3-3604-4f72-98d8-2af2c654d4e9": "p750", // s1.6

	"6f04a265-3a9f-4b99-bb66-944ac848af22": "p750", // n1.14d.g1
	"4b4891d6-860b-4c70-b48e-d261e9751b56": "p750", // n1.28d.g2

	"396671f4-6f19-4531-98d0-127272916cee": "p750", // n2.14d.g1
	"0c85bbb2-5c85-465c-abf9-71261b8fbcd4": "p750", // n2.28d.g2
	"a7c5c538-aef1-4f40-8c8d-1366c5b80271": "p750", // n2.56d.g4

	"25129382-dbe8-43eb-b71b-72253dd69452": "p10", // t1.1
	"22b37153-8817-4c85-9805-92426b2f903c": "p10", // t2i.1
}

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

// getPlanId returns the plan ID from the service annotations
// if no plan id or flavor ID annotations are found then default p10 plan is used
func getPlanID(service *corev1.Service) (planID *string, msgs []string, err error) {
	msgs = make([]string, 0)
	if planID, found := service.Annotations[servicePlanAnnotation]; found {
		for _, availablePlan := range availablePlanIDs {
			if planID == availablePlan {
				return &planID, nil, nil
			}
		}
		return nil, nil, fmt.Errorf("unsupported plan ID value %q, supported values are %v", planID, availablePlanIDs)
	}
	if flavorID, found := service.Annotations[yawolFlavorIDAnnotation]; found {
		planID, ok := flavorsMap[flavorID]
		if !ok {
			planID, ok = appoximateFlavorsMap[flavorID]
			if !ok {
				return nil, nil, fmt.Errorf("unsupported flavor ID value %q", flavorID)
			}
		}
		//nolint: lll // We cannot shortten this line
		msgs = append(msgs, fmt.Sprintf(`Flavors are deprecated in favor of service plans. Picking load balancer service plan %s for flavor %s. Use the annotation lb.stackit.cloud/service-plan-id to explicitly choose a service plan.`, planID, flavorID))
		return &planID, msgs, nil
	}
	return &defaultServicePlan, nil, nil
}

// lbSpecFromService returns a load balancer specification in the form of a create payload matching the specification of the service, nodes and network.
// The property name will be empty and must be set by the caller to produce a valid payload for the API.
// An error is returned if the service has invalid options.
func lbSpecFromService( //nolint:funlen,gocyclo // It is long but not complex.
	service *corev1.Service,
	nodes []*corev1.Node,
	networkID string,
	observability *loadbalancer.LoadbalancerOptionObservability,
) (*loadbalancer.CreateLoadBalancerPayload, []Event, error) {
	lb := &loadbalancer.CreateLoadBalancerPayload{
		Options: &loadbalancer.LoadBalancerOptions{},
		Networks: &[]loadbalancer.Network{
			{
				Role:      utils.Ptr(loadbalancer.NETWORKROLE_LISTENERS_AND_TARGETS),
				NetworkId: &networkID,
			},
		},
	}

	if listenerNetwork := service.Annotations[listenerNetworkAnnotation]; listenerNetwork != "" {
		lb.Networks = &[]loadbalancer.Network{
			{
				Role:      utils.Ptr(loadbalancer.NETWORKROLE_TARGETS),
				NetworkId: &networkID,
			}, {
				Role:      utils.Ptr(loadbalancer.NETWORKROLE_LISTENERS),
				NetworkId: &listenerNetwork,
			},
		}
	} else {
		lb.Networks = &[]loadbalancer.Network{
			{
				Role:      utils.Ptr(loadbalancer.NETWORKROLE_LISTENERS_AND_TARGETS),
				NetworkId: &networkID,
			},
		}
	}

	events := make([]Event, 0)

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
			return nil, nil, fmt.Errorf("invalid bool value %q for annotation %q: %w", internalStr, internalLBAnnotation, err)
		}
		lb.Options.PrivateNetworkOnly = internal
	}
	if internalStr, found := service.Annotations[yawolInternalLBAnnotation]; found {
		i, _ := strconv.ParseBool(internalStr)
		yawolInternal = &i
		lb.Options.PrivateNetworkOnly = yawolInternal
	}
	if yawolInternal != nil && internal != nil && *yawolInternal != *internal {
		return nil, nil, fmt.Errorf("incompatible values for annotations %s and %s", yawolInternalLBAnnotation, internalLBAnnotation)
	}

	// process service-plan-id annotation
	planID, msgs, err := getPlanID(service)
	if err != nil {
		return nil, nil, fmt.Errorf("getPlanId: %w", err)
	}
	lb.PlanId = planID

	for _, msg := range msgs {
		events = append(events, Event{
			Type:    corev1.EventTypeWarning,
			Message: msg,
			Reason:  EventReasonSelectedPlanID,
		})
	}

	// Parse external IP from annotations.
	// TODO: Split into separate function.
	externalIP, found := service.Annotations[externalIPAnnotation]
	yawolExternalIP, yawolFound := service.Annotations[yawolExistingFloatingIPAnnotation]
	if found && yawolFound && externalIP != yawolExternalIP {
		return nil, nil, fmt.Errorf(
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
			return nil, nil, fmt.Errorf("invalid format for external IP: %w", err)
		}
		if ip.Is6() {
			return nil, nil, fmt.Errorf("external IP must be an IPv4 address")
		}
		lb.ExternalAddress = &externalIP
	}

	// Add metric metricsRemoteWrite settings
	lb.Options.Observability = observability

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
			return nil, nil, fmt.Errorf("invalid format for annotation %s: %w", tcpIdleTimeoutAnnotation, err)
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
		return nil, nil, fmt.Errorf("incompatible values for annotations %s and %s", tcpIdleTimeoutAnnotation, yawolTCPIdleTimeoutAnnotation)
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
			return nil, nil, fmt.Errorf("invalid format for annotation %s: %w", udpIdleTimeoutAnnotation, err)
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
		return nil, nil, fmt.Errorf("incompatible values for annotations %s and %s", udpIdleTimeoutAnnotation, yawolUDPIdleTimeoutAnnotation)
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
			return nil, nil, fmt.Errorf("invalid bool value for annotation %s: %w", tcpProxyProtocolEnabledAnnotation, err)
		}
	}
	if yawolFound {
		// For backwards-compatibility, we don't error on non-boolean values.
		e, _ := strconv.ParseBool(service.Annotations[yawolTCPProxyProtocolEnabledAnnotation])
		yawolTCPProxyProtocolEnabled = e
	}
	if found && yawolFound && yawolTCPProxyProtocolEnabled != tcpProxyProtocolEnabled {
		return nil, nil, fmt.Errorf(
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
			return nil, nil, fmt.Errorf(
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
						return nil, nil, fmt.Errorf(
							"invalid port %q at position %d in annotation %q: %w", portStr, i, tcpProxyProtocolPortFilterAnnotation, err,
						)
					}
					tcpProxyProtocolPortFilter = append(tcpProxyProtocolPortFilter, uint16(port))
				}
			}
		}
	}

	// Parse session persistence with source ip addresss from annotation.
	useSourceIP := false
	if val, found := service.Annotations[sessionPersistenceWithSourceIP]; found {
		parsed, err := strconv.ParseBool(val)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid bool value for annotation %s: %w", sessionPersistenceWithSourceIP, err)
		}
		useSourceIP = parsed
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
			name = fmt.Sprintf("port-%s-%d", strings.ToLower(string(port.Protocol)), port.Port)
		}

		var protocol loadbalancer.ListenerProtocol
		var tcpOptions *loadbalancer.OptionsTCP
		var udpOptions *loadbalancer.OptionsUDP

		switch port.Protocol {
		case corev1.ProtocolTCP:
			if proxyProtocolEnableForPort(tcpProxyProtocolEnabled, tcpProxyProtocolPortFilter, port.Port) {
				protocol = loadbalancer.LISTENERPROTOCOL_TCP_PROXY
			} else {
				protocol = loadbalancer.LISTENERPROTOCOL_TCP
			}
			tcpOptions = &loadbalancer.OptionsTCP{
				IdleTimeout: utils.Ptr(fmt.Sprintf("%.0fs", tcpIdleTimeout.Seconds())),
			}
		case corev1.ProtocolUDP:
			protocol = loadbalancer.LISTENERPROTOCOL_UDP
			udpOptions = &loadbalancer.OptionsUDP{
				IdleTimeout: utils.Ptr(fmt.Sprintf("%.0fs", udpIdleTimeout.Seconds())),
			}
		default:
			return nil, nil, fmt.Errorf("unsupported protocol %q for port %q", port.Protocol, port.Name)
		}

		listeners = append(listeners, loadbalancer.Listener{
			DisplayName: &name,
			Port:        utils.Ptr(int64(port.Port)),
			TargetPool:  &name,
			Protocol:    utils.Ptr(protocol),
			Tcp:         tcpOptions,
			Udp:         udpOptions,
		})

		targetPools = append(targetPools, loadbalancer.TargetPool{
			Name:       &name,
			TargetPort: utils.Ptr(int64(port.NodePort)),
			Targets:    &targets,
			SessionPersistence: &loadbalancer.SessionPersistence{
				UseSourceIpAddress: utils.Ptr(useSourceIP),
			},
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

	if event := checkUnsupportedAnnotations(service); event != nil {
		events = append(events, *event)
	}

	if events != nil {
		return lb, events, nil
	}
	return lb, nil, nil
}

func checkUnsupportedAnnotations(service *corev1.Service) *Event {
	usedAnnotations := []string{}
	for _, a := range yawolUnsupportedAnnotations {
		if _, found := service.Annotations[a]; found {
			usedAnnotations = append(usedAnnotations, a)
		}
	}
	if len(usedAnnotations) > 0 {
		// The maximum event size is 1024 characters. Even with all ignored we reach less than 600 characters.
		message := "The following annotations are only valid for yawol load balancers and will be ignored for STACKIT load balancers: " +
			strings.Join(usedAnnotations, ", ")
		return &Event{
			Type:    corev1.EventTypeWarning,
			Reason:  eventReasonYawolAnnotationPresent,
			Message: message,
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
// Otherwise, fulfills will indicate whether an update is necessary.
func compareLBwithSpec(lb *loadbalancer.LoadBalancer, spec *loadbalancer.CreateLoadBalancerPayload) (fulfills bool, immutableChanged *resultImmutableChanged) { //nolint:gocyclo,funlen,lll // It is long but not complex.
	// If a mutable property has changed we must still check the rest of the object because if there is an immutable change it must always be returned.
	fulfills = true

	if cmp.UnpackPtr(cmp.UnpackPtr(lb.Options).PrivateNetworkOnly) != cmp.UnpackPtr(cmp.UnpackPtr(spec.Options).PrivateNetworkOnly) {
		return false, &resultImmutableChanged{field: ".options.privateNetworkOnly"}
	}

	if !cmp.PtrValEqualFn(
		cmp.UnpackPtr(lb.Options).Observability,
		cmp.UnpackPtr(spec.Options).Observability,
		func(a, b loadbalancer.LoadbalancerOptionObservability) bool {
			sameMetrics := cmp.PtrValEqualFn(
				a.Metrics,
				b.Metrics,
				func(c, d loadbalancer.LoadbalancerOptionMetrics) bool {
					return cmp.UnpackPtr(c.PushUrl) == cmp.UnpackPtr(d.PushUrl) &&
						cmp.UnpackPtr(c.CredentialsRef) == cmp.UnpackPtr(d.CredentialsRef)
				},
			)
			sameLogs := cmp.PtrValEqualFn(
				a.Logs,
				b.Logs,
				func(c, d loadbalancer.LoadbalancerOptionLogs) bool {
					return cmp.UnpackPtr(c.PushUrl) == cmp.UnpackPtr(d.PushUrl) &&
						cmp.UnpackPtr(c.CredentialsRef) == cmp.UnpackPtr(d.CredentialsRef)
				},
			)
			return sameMetrics && sameLogs
		},
	) {
		fulfills = false
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
			if (cmp.UnpackPtr(x.Protocol) == loadbalancer.LISTENERPROTOCOL_TCP || cmp.UnpackPtr(x.Protocol) == loadbalancer.LISTENERPROTOCOL_TCP_PROXY) &&
				!cmp.PtrValEqualFn(x.Tcp, y.Tcp, func(a, b loadbalancer.OptionsTCP) bool {
					return cmp.PtrValEqual(a.IdleTimeout, b.IdleTimeout)
				}) {
				fulfills = false
			}
			if cmp.UnpackPtr(x.Protocol) == loadbalancer.LISTENERPROTOCOL_UDP && !cmp.PtrValEqualFn(x.Udp, y.Udp, func(a, b loadbalancer.OptionsUDP) bool {
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
			if cmp.UnpackPtr(cmp.UnpackPtr(x.SessionPersistence).UseSourceIpAddress) != cmp.UnpackPtr(cmp.UnpackPtr(y.SessionPersistence).UseSourceIpAddress) {
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

	if !cmp.PtrValEqual(lb.PlanId, spec.PlanId) {
		// In this comparison, an empty service plan is not equal to a default service plan.
		// The API might return a default value if no value is specified.
		// To avoid problems in the change detection, the CCM should also explicitly set a value.
		fulfills = false
	}

	if !cmp.SliceEqual(
		cmp.UnpackPtr(cmp.UnpackPtr(cmp.UnpackPtr(lb.Options).AccessControl).AllowedSourceRanges),
		cmp.UnpackPtr(cmp.UnpackPtr(cmp.UnpackPtr(spec.Options).AccessControl).AllowedSourceRanges),
	) {
		fulfills = false
	}

	return fulfills, immutableChanged
}
