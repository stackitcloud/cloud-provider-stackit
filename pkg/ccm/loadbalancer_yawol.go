package ccm

// This file contains annotations defined by yawol.
// Some of them are supported by the cloud controller manager to simplify the transition.

const (
	// yawolClassNameAnnotation defines the load balancer class for the service, and therefore which controller provisions the load balancer.
	// It must be set to "stackit" for the cloud controller manager to handle this load balancer.
	// To avoid collisions while being backwards-compatible, yawol handles the service if this annotation is not set.
	// service.spec.loadBalancerClass is not supported because yawol as well as the cloud controller manager handle the empty class name.
	// There is no successor for this annotation because both controllers need to understand it.
	yawolClassNameAnnotation = "yawol.stackit.cloud/className"
	// yawolInternalLBAnnotation defines whether the load balancer should be exposed via a public IP.
	// Deprecated: use lb.stackit.cloud/internal-lb instead.
	yawolInternalLBAnnotation = "yawol.stackit.cloud/internalLB"
	// yawolExistingFloatingIPAnnotation references an existing floating IP for the load balancer to use.
	// Deprecated: Use lb.stackit.cloud/external-address instead.
	yawolExistingFloatingIPAnnotation = "yawol.stackit.cloud/existingFloatingIP"
	// Specify the loadBalancerSourceRanges for the LoadBalancer like service.spec.loadBalancerSourceRanges (comma separated list).
	// Deprecated: Use service.spec.loadBalancerSourceRanges instead.
	yawolLoadBalancerSourceRangesAnnotation = "yawol.stackit.cloud/loadBalancerSourceRanges"
	// yawolTCPProxyProtocolEnabledAnnotation enables the TCP proxy protocol.
	// Deprecated: Use lb.stackit.cloud/tcp-proxy-protocol instead.
	yawolTCPProxyProtocolEnabledAnnotation = "yawol.stackit.cloud/tcpProxyProtocol"
	// yawolTCPProxyProtocolPortFilterAnnotation defines which ports should use the TCP proxy protocol.
	// Deprecated: Use lb.stackit.cloud/tcp-proxy-protocol-ports-filter instead.
	yawolTCPProxyProtocolPortFilterAnnotation = "yawol.stackit.cloud/tcpProxyProtocolPortsFilter"
	// yawolTCPIdleTimeoutAnnotation defines the idle timeout for all TCP ports.
	// Deprecated: Use lb.stackit.cloud/tcp-idle-timeout instead.
	yawolTCPIdleTimeoutAnnotation = "yawol.stackit.cloud/tcpIdleTimeout"
	// yawolUDPIdleTimeoutAnnotation defines the idle timeout for all UDP ports.
	// Deprecated: Use lb.stackit.cloud/udp-idle-timeout instead.
	yawolUDPIdleTimeoutAnnotation = "yawol.stackit.cloud/udpIdleTimeout"
	// yawolFlavorID is used to select a plan ID that matches the selected flavor.
	// Deprecated: Use lb.stackit.cloud/service-plan-id instead.
	yawolFlavorIDAnnotation = "yawol.stackit.cloud/flavorId"
)

// yawol annotations, that are ignored by the CCM.
const (
	// yawolImageIDAnnotation is not supported.
	yawolImageIDAnnotation = "yawol.stackit.cloud/imageId"
	// yawolDefaultNetworkIDAnnotation is not supported.
	// The load balancer is always only connected to the SKE network.
	yawolDefaultNetworkIDAnnotation = "yawol.stackit.cloud/defaultNetworkID"
	// yawolSkipDefaultNetworkIDAnnotation is not supported.
	yawolSkipDefaultNetworkIDAnnotation = "yawol.stackit.cloud/skipCloudControllerDefaultNetworkID"
	// yawolFloatingNetworkIDAnnotation is not supported.
	// The floating network is selected by the load balancer API.
	yawolFloatingNetworkIDAnnotation = "yawol.stackit.cloud/floatingNetworkID"
	// yawolAvailabilityZoneAnnotation is not supported.
	// Load balancers no longer use metro VMs by default, but each machine can be in any AZ.
	yawolAvailabilityZoneAnnotation = "yawol.stackit.cloud/availabilityZone"
	// yawolDebugAnnotation is not supported.
	yawolDebugAnnotation = "yawol.stackit.cloud/debug"
	// yawolDebugSSHKeyAnnotation is not supported.
	yawolDebugSSHKeyAnnotation = "yawol.stackit.cloud/debugsshkey"
	// yawolReplicasAnnotation is not supported.
	// The load balancer service is always highly available.
	yawolReplicasAnnotation = "yawol.stackit.cloud/replicas"
	// yawolLogForwardAnnotation is not supported.
	// Load balancer service doesn't log shipping.
	yawolLogForwardAnnotation = "yawol.stackit.cloud/logForward"
	// yawolLogForwardURLAnnotation is not supported.
	// Load balancer service doesn't do log shipping.
	yawolLogForwardURLAnnotation = "yawol.stackit.cloud/logForwardLokiURL"
	// yawolServerGroupPolicyAnnotation is not supported.
	// It would only apply when LBMs are in the same AZ.
	yawolServerGroupPolicyAnnotation = "yawol.stackit.cloud/serverGroupPolicy"
	// yawolAdditionalNetworksAnnotation is not supported.
	yawolAdditionalNetworksAnnotation = "yawol.stackit.cloud/additionalNetworks"
)

var yawolUnsupportedAnnotations = []string{
	yawolImageIDAnnotation,
	yawolDefaultNetworkIDAnnotation,
	yawolFloatingNetworkIDAnnotation,
	yawolSkipDefaultNetworkIDAnnotation,
	yawolAvailabilityZoneAnnotation,
	yawolDebugAnnotation,
	yawolDebugSSHKeyAnnotation,
	yawolReplicasAnnotation,
	yawolLogForwardAnnotation,
	yawolLogForwardURLAnnotation,
	yawolServerGroupPolicyAnnotation,
	yawolAdditionalNetworksAnnotation,
}
