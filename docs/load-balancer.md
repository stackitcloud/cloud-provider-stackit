# Load balancers user documentation

## Table of Contents

- [Overview](#overview)
- [Limitations](#limitations)
- [Service Enablement](#service-enablement)
- [Configuration](#configuration)
  - [STACKIT Annotations](#stackit-annotations)
  - [Supported yawol Annotations](#supported-yawol-annotations)
  - [Unsupported yawol Annotations](#unsupported-yawol-annotations)
- [Node Labels](#node-labels)

## Overview

The cloud controller manager provisions STACKIT load balancers for Kubernetes services of type load balancer.

## Limitations

- `externalTrafficPolicy=local` is not supported.
- `sessionAffinity` is not supported.
- Health checks are not implemented. If a node becomes unhealthy, then it is removed from the targets via the CCM.
- The load balancer service currently adds security rules to each target.
  In the case of the CCM, the targets are the Kubernetes nodes.
  Experiments have shown that SKE will leave the assignment untouched, even during a maintenance.

## Service Enablement

To create load balancers, the STACKIT load balancer service must be enabled.
The cloud controller manager automatically enables the service when the first load balancer is created.
The cloud controller manager does not disable the services when it no longer needs it, because other load balancers might have been created in the meantime.

## Configuration

The cloud controller manager provisions a load balancer based on the specification of the service.
STACKIT-specific options can be configured via annotations.
Values for boolean annotations are parsed according to [ParseBool](https://pkg.go.dev/strconv#ParseBool).

### STACKIT Annotations

| Name                                                | Default    | Description                                                                                                                                                                                                                                                                                                                                                                                                              |
| --------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| lb.stackit.cloud/internal-lb                        | "false"    | If true, the load balancer is not exposed via a floating IP.                                                                                                                                                                                                                                                                                                                                                             |
| lb.stackit.cloud/external-address                   | _none_     | References an OpenStack floating IP that should be used by the load balancer. If set, it will be used instead of an ephemeral IP. The IP must be created by the user. When the service is deleted, the floating IP will not be deleted. The IP is ignored if the load balancer internal. If the annotation is set after the creation, it must match the ephemeral IP. This will promote the ephemeral IP to a static IP. |
| lb.stackit.cloud/tcp-proxy-protocol                 | "false"    | Enables the TCP proxy protocol for TCP ports.                                                                                                                                                                                                                                                                                                                                                                            |
| lb.stackit.cloud/tcp-proxy-protocol-ports-filter    | _none_     | Defines which port use the TCP proxy protocol. Only takes effect if TCP proxy protocol is enabled. If the annotation is not present, then all TCP ports use the TCP proxy protocol. Has no effect on UDP ports.                                                                                                                                                                                                          |
| lb.stackit.cloud/tcp-idle-timeout                   | 60 minutes | Defines the idle timeout for all TCP ports (including ports with the PROXY protocol).                                                                                                                                                                                                                                                                                                                                    |
| lb.stackit.cloud/udp-idle-timeout                   | 2 minutes  | Defines the idle timeout for all UDP ports.                                                                                                                                                                                                                                                                                                                                                                              |
| lb.stackit.cloud/service-plan-id                    | p10        | Defines the [plan ID](https://docs.api.eu01.stackit.cloud/documentation/load-balancer/version/v1#tag/Load-Balancer/operation/APIService_CreateLoadBalancer) when creating a load balancer. Allowed values are: p10, p50, p250 and p750                                                                                                                                                                                   |
| lb.stackit.cloud/ip-mode-proxy                      | false      | If true, the load balancer will be reported to Kubernetes as a proxy (in the service status). This causes connections to the load balancer IP that come from within the cluster to be routed to through the load balancer, rather than directly to the `kube-proxy`. Requires Kubernetes v1.30. The annotation has no effect on earlier versions. Recommended in combination with the TCP proxy protocol.                |
| lb.stackit.cloud/session-persistence-with-source-ip | false      | When set to true, all connections from the same source IP are consistently routed to the same target. This setting changes the load balancing algorithm to Maglev. Note, this only works reliably when `externalTrafficPolicy: Local` is set on the Service, and each node has exactly one backing pod. Otherwise, session persistence may break.                                                                        |
| lb.stackit.cloud/listener-network                   | _none_     | When set, defines the network in which the load balancer should listen. If not set, the SKE network is used for listening. The value must be a network ID, not a subnet. The annotation can neither be changed nor be added or removed after service creation.                                                                                                                                                           |

### Supported yawol Annotations

To simplify the transition from a yawol load balancer, some yawol annotations are supported on STACKIT load balancers.
Legacy yawol annotations on STACKIT load balancers should be replaced with their STACKIT counterpart.
A load balancer contain both annotations as long as their values are compatible.

| Name                                            | Description                                                                                                                                                                                                                                                                                                                                         |
| ----------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| yawol.stackit.cloud/internalLB                  | If true, the load balancer is not exposed via a floating IP. Default is false (i.e. exposed). Deprecated: Use lb.stackit.cloud/internal-lb instead.                                                                                                                                                                                                 |
| yawol.stackit.cloud/existingFloatingIP          | References an OpenStack floating IP that should be used by the load balancer. If set, it will be used instead of an ephemeral IP. The IP must be created by the user. When the service is deleted, the floating IP will not be deleted. The IP is ignored if the load balancer internal. Deprecated: Use lb.stackit.cloud/external-address instead. |
| yawol.stackit.cloud/loadBalancerSourceRanges    | Specify the `loadBalancerSourceRanges` for the load balancer like `service.spec.loadBalancerSourceRanges` (comma separated list). Deprecated: Use `service.spec.loadBalancerSourceRanges` instead.                                                                                                                                                  |
| yawol.stackit.cloud/tcpProxyProtocol            | Enables the TCP proxy protocol. Deprecated: Use lb.stackit.cloud/tcp-proxy-protocol instead.                                                                                                                                                                                                                                                        |
| yawol.stackit.cloud/tcpProxyProtocolPortsFilter | Defines which ports should use the TCP proxy protocol. Deprecated: Use lb.stackit.cloud/tcp-proxy-protocol-ports-filter instead.                                                                                                                                                                                                                    |
| yawol.stackit.cloud/tcpIdleTimeout              | Defines the idle timeout for all TCP ports (including ports with the PROXY protocol). Deprecated: Use lb.stackit.cloud/tcp-idle-timeout instead.                                                                                                                                                                                                    |
| yawol.stackit.cloud/udpIdleTimeout              | Defines the idle timeout for all UDP ports. Deprecated: Use lb.stackit.cloud/udp-idle-timeout instead.                                                                                                                                                                                                                                              |
| yawol.stackit.cloud/flavorId                    | Defines the flavor used for the load balancer machines. Because STACKIT load balancers don't explicitly support flavors, the selected flavor will be mapped to a service plan that has a similar performance. Deprecated: Use lb.stackit.cloud/service-plan-id instead.                                                                             |

### Unsupported yawol Annotations

These annotations are no longer supported.
They are ignored for provisioning, but an event is logged on the Kubernetes service.

| Name                                                    | Notes |
| ------------------------------------------------------- | ----- |
| yawol.stackit.cloud/imageId                             |       |
| yawol.stackit.cloud/defaultNetworkID                    |       |
| yawol.stackit.cloud/skipCloudControllerDefaultNetworkID |       |
| yawol.stackit.cloud/floatingNetworkID                   |       |
| yawol.stackit.cloud/availabilityZone                    |       |
| yawol.stackit.cloud/debug                               |       |
| yawol.stackit.cloud/debugsshkey                         |       |
| yawol.stackit.cloud/replicas                            |       |
| yawol.stackit.cloud/logForward                          |       |
| yawol.stackit.cloud/logForwardLokiURL                   |       |
| yawol.stackit.cloud/serverGroupPolicy                   |       |
| yawol.stackit.cloud/additionalNetworks                  |       |

## Node Labels

The cloud controller manager supports the well-known label `node.kubernetes.io/exclude-from-external-load-balancers` on nodes to exclude them from receiving traffic from the load balancer.
