# cloud-provider-stackit

This repository includes:

- Cloud Controller Manager (CCM)
- Kubernetes Resources for the Manager
- Ginko bootstrapped Test Suite
- Prow Job in ske-ci-infra for build

For further information: take the [hcloud-example](https://github.com/hetznercloud/hcloud-cloud-controller-manager/tree/main) as reference.

Does not include:

- readyz and healthz
- Kubernetes Client with self authorization by `inClusterConfig`

TODOs:

- remove all `nolint:golint,all` from Code
- switch to Prow-Cluster `ske-prow-trusted` in the `ske-ci-infra` repository
- `externalTrafficPolicy=local`
- `sessionAffinity`
- Active health checks of nodes

## Operations

- Required: STACKIT-specific settings have to be set using a cloud config via `--cloud-config=cloud-config.yaml`.

```yaml
# cloud-config.yaml
projectId: 
networkId: 
nonStackitClassNames: # If not set, defaults to "updateAndCreate" (see: Non-STACKIT class names).
loadBalancerApi:
  # If not set, defaults to production.
  url: https://load-balancer-dev.api.eu01.stg.stackit.cloud
```

- Required: STACKIT authentication for SDK
    - To authenticate against the STACKIT API follow [STACKIT SDK authentication](https://github.com/stackitcloud/stackit-sdk-go#authentication). The cloud controller manager supports all authentication methods that are supported by the SDK.
- Metrics are available at `https://:10258/metrics`. To allow unauthorized access add `--authorization-always-allow-paths=/metrics`.

## Load balancers user documentation

The cloud controller manager provisions STACKIT load balancers for Kubernetes services of type load balancer.

In order to avoid collisions with other load balancer implementations, the following annotation needs to be set on the service.
```
annotations:
    yawol.stackit.cloud/className: stackit
```
This annotation is immutable. It must not be changed on existing load balancers.  
The controller will always manage all services whose class name annotation is `stackit`.

> :warning: The CCM adds a finalizer to the service regardless of whether it has a matching class name annotation or not.

### Non-STACKIT class names
For load balancers with not `stackit` as class name (identified via the `yawol.stackit.cloud/className` annotation) the controller manages them in different ways.
The controller modes are configured via `nonStackitClassNames` in the cloud-config.yaml:
- `ignore`: Return "implemented elsewhere" for all services whose class name
  annotation is not `stackit`.
- `update`: Update load balancers whose class name annotation is not `stackit`.
  If the load balancer is not found, no error is returned.
- `updateAndCreate` (default): The CCM treats every service the same, i.e.
  ignores the class name annotation.

If no `nonStackitClassNames` mode is set in the config file, the mode will automatically be set to `updateAndCreate`.

### Limitations

- `externalTrafficPolicy=local` is not supported.
- `sessionAffinity` is not supported.
- Health checks are not implemented. If a node becomes unhealthy then it is removed from the targets via the CCM.
- The load balancer service currently adds security rules to each target. 
  In the case of the CCM the targets are the Kubernetes nodes.
  Experiments have shown that SKE will leave the assignment untouched, even during a maintenance.


### Load balancer service enablement

To create load balancers, the STACKIT load balancer service must be enabled. 
The cloud controller manager automatically enables the service when the first load balancer is created.
The cloud controller manager does not disable the services when it no longer needs it, because other load balancers might have been created in the meantime.

### Configuring the load balancer

The cloud controller manager provisions a load balancer based on the specification of the service. 
STACKIT-specific options can be configured via annotations.
Values for boolean annotations are parsed according to [ParseBool](https://pkg.go.dev/strconv#ParseBool).

| Name | Default | Description |
|---|---|---|
| lb.stackit.cloud/internal-lb | "false" | If true, the load balancer is not exposed via a floating IP. |
| lb.stackit.cloud/external-address | *none* | References an OpenStack floating IP that should be used by the load balancer. If set it will be used instead of an ephemeral IP. The IP must be created by the user. When the service is deleted, the floating IP will not be deleted. The IP is ignored if the load balancer internal. If the annotation is set after the creation it must match the ephemeral IP. This will promote the ephemeral IP to a static IP. |
| lb.stackit.cloud/tcp-proxy-protocol | "false" | Enables the TCP proxy protocol for TCP ports. |
| lb.stackit.cloud/tcp-proxy-protocol-ports-filter | *none* | Defines which port use the TCP proxy protocol. Only takes effect if TCP proxy protocol is enabled. If the annotation is not present then all TCP ports use the TCP proxy protocol. Has no effect on UDP ports. |
| lb.stackit.cloud/tcp-idle-timeout | Defines the idle timeout for all TCP ports (including ports with the PROXY protocol). If unset, the default is 60 minutes. |
| lb.stackit.cloud/udp-idle-timeout | Defines the idle timeout for all UDP ports. If unset, the default is 2 minutes. |


#### Supported yawol annotations

To simplify the transition from a yawol load balancer, some yawol annotations are supported on STACKIT load balancers.
Legacy yawol annotations on STACKIT load balancers should be replaced with their STACKIT counterpart.
A load balancer contain both annotations as long as their values are compatible.

| Name | Description |
|---|---|
| yawol.stackit.cloud/internalLB | If true, the load balancer is not exposed via a floating IP. Default is false (i.e. exposed). Deprecated: Use lb.stackit.cloud/internal-lb instead. |
| yawol.stackit.cloud/existingFloatingIP | References an OpenStack floating IP that should be used by the load balancer. If set it will be used instead of an ephemeral IP. The IP must be created by the user. When the service is deleted, the floating IP will not be deleted. The IP is ignored if the load balancer internal. Deprecated: Use lb.stackit.cloud/external-address instead. |
| yawol.stackit.cloud/loadBalancerSourceRanges | Specify the loadBalancerSourceRanges for the LoadBalancer like service.spec.loadBalancerSourceRanges (comma separated list). Deprecated: Use service.spec.loadBalancerSourceRanges instead. |
| yawol.stackit.cloud/tcpProxyProtocol | Enables the TCP proxy protocol. Deprecated: Use lb.stackit.cloud/tcp-proxy-protocol instead. |
| yawol.stackit.cloud/tcpProxyProtocolPortsFilter | Defines which ports should use the TCP proxy protocol. Deprecated: Use lb.stackit.cloud/tcp-proxy-protocol-ports-filter instead. |
| yawol.stackit.cloud/tcpIdleTimeout | Defines the idle timeout for all TCP ports (including ports with the PROXY protocol). Deprecated: Use lb.stackit.cloud/tcp-idle-timeout instead. |
| yawol.stackit.cloud/udpIdleTimeout | Defines the idle timeout for all UDP ports. Deprecated: Use lb.stackit.cloud/udp-idle-timeout instead. |

#### Unsopported yawol annotations

These annotations are no longer supported. 
If any of those annotations are present then the load balancer provision will fail.

| Name | Notes |
|---|---|
| yawol.stackit.cloud/imageId |  |
| yawol.stackit.cloud/flavorId |  |
| yawol.stackit.cloud/defaultNetworkID |  |
| yawol.stackit.cloud/skipCloudControllerDefaultNetworkID |  |
| yawol.stackit.cloud/floatingNetworkID |  |
| yawol.stackit.cloud/availabilityZone |  |
| yawol.stackit.cloud/debug |  |
| yawol.stackit.cloud/debugsshkey |  |
| yawol.stackit.cloud/replicas |  |
| yawol.stackit.cloud/logForward |  |
| yawol.stackit.cloud/logForwardLokiURL |  |
| yawol.stackit.cloud/serverGroupPolicy |  |
| yawol.stackit.cloud/additionalNetworks |  |

#### Node labels

The cloud controller manager supports the well-known label `node.kubernetes.io/exclude-from-external-load-balancers` on nodes to exclude them from receiving traffic from the load balancer.
