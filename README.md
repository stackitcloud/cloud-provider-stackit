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
region: eu01
loadBalancerApi:
  # If not set, defaults to production.
  url: https://load-balancer-dev.api.qa.stackit.cloud
```

- Required: STACKIT authentication for SDK
  - To authenticate against the STACKIT API follow [STACKIT SDK authentication](https://github.com/stackitcloud/stackit-sdk-go#authentication). The cloud controller manager supports all authentication methods that are supported by the SDK.
- Service metrics are available at `https://:10258/metrics`. To allow unauthorized access add `--authorization-always-allow-paths=/metrics`.
- Load Balancer metrics can be sent to a remote write endpoint (e.g. STACKIT observability). To use this feature all the following environment variables need to be set:
  - `STACKIT_REMOTEWRITE_ENDPOINT` the remote write push URL to send the metrics to
  - `STACKIT_REMOTEWRITE_USER` the basic auth username
  - `STACKIT_REMOTEWRITE_PASSWORD` the basic auth password
  - If none of these environment variables are set, this feature is ignored and no Load Balancer metrics are sent.

## Load balancers user documentation

The cloud controller manager provisions STACKIT load balancers for Kubernetes services of type load balancer.

In order to avoid collisions with other load balancer implementations, the following annotation needs to be set on the service.

```YAML
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

| Name                                                | Default    | Description                                                                                                                                                                                                                                                                                                                                                                                                            |
| --------------------------------------------------- | ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| lb.stackit.cloud/internal-lb                        | "false"    | If true, the load balancer is not exposed via a floating IP.                                                                                                                                                                                                                                                                                                                                                           |
| lb.stackit.cloud/external-address                   | _none_     | References an OpenStack floating IP that should be used by the load balancer. If set it will be used instead of an ephemeral IP. The IP must be created by the user. When the service is deleted, the floating IP will not be deleted. The IP is ignored if the load balancer internal. If the annotation is set after the creation it must match the ephemeral IP. This will promote the ephemeral IP to a static IP. |
| lb.stackit.cloud/tcp-proxy-protocol                 | "false"    | Enables the TCP proxy protocol for TCP ports.                                                                                                                                                                                                                                                                                                                                                                          |
| lb.stackit.cloud/tcp-proxy-protocol-ports-filter    | _none_     | Defines which port use the TCP proxy protocol. Only takes effect if TCP proxy protocol is enabled. If the annotation is not present then all TCP ports use the TCP proxy protocol. Has no effect on UDP ports.                                                                                                                                                                                                         |
| lb.stackit.cloud/tcp-idle-timeout                   | 60 minutes | Defines the idle timeout for all TCP ports (including ports with the PROXY protocol).                                                                                                                                                                                                                                                                                                                                  |
| lb.stackit.cloud/udp-idle-timeout                   | 2 minutes  | Defines the idle timeout for all UDP ports.                                                                                                                                                                                                                                                                                                                                                                            |
| lb.stackit.cloud/service-plan-id                    | p10        | Defines the [plan ID](https://docs.api.eu01.stackit.cloud/documentation/load-balancer/version/v1#tag/Load-Balancer/operation/APIService_CreateLoadBalancer) when creating a load balancer. Allowed values are: p10, p50, p250 and p750                                                                                                                                                                                 |
| lb.stackit.cloud/ip-mode-proxy                      | false      | If true, the load balancer will be reported to Kubernetes as a proxy (in the service status). This causes connections to the load balancer IP that come from within the cluster to be routed to through the load balancer, rather than directly to the kube-proxy. Requires Kubernetes v1.30. The annotation has no effect on earlier version. Recommended in combination with the TCP proxy protocol.                 |
| lb.stackit.cloud/session-persistence-with-source-ip | false      | When set to true, all connections from the same source IP are consistently routed to the same target. This setting changes the load balancing algorithm to Maglev. Note, this only works reliably when externalTrafficPolicy: Local is set on the Service, and each node has exactly one backing pod. Otherwise, session persistence may break.                                                                        |
| lb.stackit.cloud/listener-network                   | _none_     | When set, defines the network in which the load balancer should listen. If not set, the SKE network is used for listening. The value must be a network ID, not a subnet. The annotation can neither be changed nor be added or removed after service creation.                                                                                                                                                         |

#### Supported yawol annotations

To simplify the transition from a yawol load balancer, some yawol annotations are supported on STACKIT load balancers.
Legacy yawol annotations on STACKIT load balancers should be replaced with their STACKIT counterpart.
A load balancer contain both annotations as long as their values are compatible.

| Name                                            | Description                                                                                                                                                                                                                                                                                                                                        |
| ----------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| yawol.stackit.cloud/internalLB                  | If true, the load balancer is not exposed via a floating IP. Default is false (i.e. exposed). Deprecated: Use lb.stackit.cloud/internal-lb instead.                                                                                                                                                                                                |
| yawol.stackit.cloud/existingFloatingIP          | References an OpenStack floating IP that should be used by the load balancer. If set it will be used instead of an ephemeral IP. The IP must be created by the user. When the service is deleted, the floating IP will not be deleted. The IP is ignored if the load balancer internal. Deprecated: Use lb.stackit.cloud/external-address instead. |
| yawol.stackit.cloud/loadBalancerSourceRanges    | Specify the loadBalancerSourceRanges for the LoadBalancer like service.spec.loadBalancerSourceRanges (comma separated list). Deprecated: Use service.spec.loadBalancerSourceRanges instead.                                                                                                                                                        |
| yawol.stackit.cloud/tcpProxyProtocol            | Enables the TCP proxy protocol. Deprecated: Use lb.stackit.cloud/tcp-proxy-protocol instead.                                                                                                                                                                                                                                                       |
| yawol.stackit.cloud/tcpProxyProtocolPortsFilter | Defines which ports should use the TCP proxy protocol. Deprecated: Use lb.stackit.cloud/tcp-proxy-protocol-ports-filter instead.                                                                                                                                                                                                                   |
| yawol.stackit.cloud/tcpIdleTimeout              | Defines the idle timeout for all TCP ports (including ports with the PROXY protocol). Deprecated: Use lb.stackit.cloud/tcp-idle-timeout instead.                                                                                                                                                                                                   |
| yawol.stackit.cloud/udpIdleTimeout              | Defines the idle timeout for all UDP ports. Deprecated: Use lb.stackit.cloud/udp-idle-timeout instead.                                                                                                                                                                                                                                             |
| yawol.stackit.cloud/flavorId                    | Defines the flavor used for the load balancer machines. Because STACKIT load balancers don't explicitly support flavors, the selected flavor will be mapped to a service plan that has a similar performance. Check bellow for the mappings Deprecated: Use lb.stackit.cloud/service-plan-id instead.                                              |

#### Unsopported yawol annotations

These annotations are no longer supported.
They are ignored for provisioning but an event is logged on the Kubernetes service.

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

#### Node labels

The cloud controller manager supports the well-known label `node.kubernetes.io/exclude-from-external-load-balancers` on nodes to exclude them from receiving traffic from the load balancer.

#### Mapping flavor IDs to service plans

| Flavor name | Service plan ID | Flavor ID                            |
| ----------- | --------------- | ------------------------------------ |
| s1a.2d      | p50             | 2faeefeb-efe7-4f8b-9e52-3246a5d709f0 |
| s1a.4d      | p50             | cd49f4fd-1e48-497f-91ad-79894c8b95e4 |
| s1a.8d      | p250            | 72f11e14-2825-471d-a237-b1afa775fdad |
| s1a.16d     | p750            | 53408825-7086-48c2-9126-cafdeb2d35d6 |
| s1a.32d     | p750            | 9b6bfa7b-bb80-4da8-aa10-ddd4cfaaa1a1 |
| s1a.60d     | p750            | 8936d6a5-30bb-4012-834c-29c599800e53 |
| b1.1        | p250            | 75e8134a-e1de-4052-b3be-75c5157c47c6 |
| b1.2        | p750            | 1493fabc-3e5c-4992-82fc-d43e2c33902a |
| b1.3        | p750            | f77046c4-6c41-452c-9983-7264151252fa |
| b1.4        | p750            | f778f21f-b0a7-4ae0-88e9-917f01d6fb52 |
| b1a.1d      | p250            | 49902b99-b428-4e6a-ad34-d8b9e719390f |
| b1a.2d      | p750            | ce99338f-afc2-4966-89e2-34e494d89e4b |
| b1a.4d      | p750            | 2e364c23-ee61-451c-841c-8fa25573ae9d |
| b1a.8d      | p750            | c1e2def6-e182-4bf6-a0f9-9b5b453eb55a |
| b1a.16d     | p750            | fda4f402-6d43-4db5-bcf1-384596f237bb |
| b1a.32d     | p750            | 704c07a3-1308-4cdd-b8f3-0892589cb99c |
| b1a.60d     | p750            | 696d8b7a-6aaa-456c-853b-11a7ba490b66 |
| b1a.120d    | p750            | 8b46ca04-e7ec-4f01-a2ed-67e75e3fe04f |
| b2i.1d      | p250            | 882a98ba-d47f-4a52-bd85-ccbc2b08f8f8 |
| b2i.2d      | p750            | 6c1b79d7-b344-407e-808d-476187c7dcd6 |
| b2i.4d      | p750            | 013451f5-4c26-4464-84e6-cc5f1c8b0f8a |
| b2i.8d      | p750            | 2787f539-a8b9-40d3-873d-6db51a2edb41 |
| b2i.16d     | p750            | 7bd9d46f-7c3b-4089-88e5-fca17581295e |
| b2i.30d     | p750            | 562af0ba-2540-4b49-943e-0beb6c9afa04 |
| b2i.36d     | p750            | a09e6576-4f74-4a4d-963a-05ec49e27f18 |
| c1.1        | p50             | 7d1572e1-11c9-4872-8ce8-4b953cdf6fb3 |
| c1.2        | p50             | 5fe737c2-18d8-43c6-bb11-dc9c97ff9515 |
| c1.3        | p250            | 8512c5f9-4426-47f1-a9dc-5c5a5a798b54 |
| c1.4        | p750            | ecb39de6-8b6c-431e-8455-9d857639be92 |
| c1.5        | p750            | 442e31fa-654a-4f76-b7c2-4802592f9cc7 |
| c1a.1d      | p50             | 6f65263f-0902-47ca-8761-6e449648c8f0 |
| c1a.2d      | p50             | a9704593-dc26-45b7-8b1c-a37bf42d253e |
| c1a.4d      | p250            | d04236f9-4740-4058-9695-0a80a9b3a9b0 |
| c1a.8d      | p750            | cac16b39-a179-43c5-b5e5-ad22eca1c87c |
| c1a.16d     | p750            | 381b5633-b064-41aa-af78-cd1bd318a0e1 |
| c2i.1       | p50             | ef66543f-3225-48b0-ab42-4cfda07668b8 |
| c2i.2       | p50             | 0c69d386-ca5e-4720-8812-225bbf4d4879 |
| c2i.4       | p250            | a03cf8cd-f5e4-4897-b639-41b4a1a46dc6 |
| c2i.8       | p750            | cee66b47-8465-469d-bb61-7a23073c3488 |
| c2i.16      | p750            | 5fa8e67c-4259-4353-8e29-dece88c3a394 |
| g1.1        | p50             | 64d695be-04b8-4f14-b020-712ef0e30a6b |
| g1.2        | p250            | 3b11b27e-6c73-470d-b595-1d85b95a8cdf |
| g1.3        | p250            | 028a4cf9-d9de-4706-a6d2-3ec9a456a736 |
| g1.4        | p750            | 21ff0965-d385-4e90-9ae4-e1ac8ca8f569 |
| g1.5        | p750            | d1f51f86-3fa3-46a1-9e9f-b8b1308f039e |
| g1a.1d      | p50             | 17837ed5-515a-457f-b36b-531fdb861b8a |
| g1a.2d      | p250            | c6b4adc7-d101-48d4-a2ea-d77cbaa63768 |
| g1a.4d      | p250            | c995089f-8d81-4085-be7b-dc2f7ad3f05f |
| g1a.8d      | p750            | cfd6f5f6-b2da-49db-9f1c-4f2ef4c8e831 |
| g1a.16d     | p750            | 816c3d62-3526-47c6-90b4-7c47318f7526 |
| g1a.32d     | p750            | 84a73cca-db0b-4d56-837f-5e5422520d51 |
| g1a.60d     | p750            | b9f4c5f0-49d7-48a1-ab41-34000f00664b |
| g1r.1d      | p50             | 8d811c25-a261-4cbe-aadf-6c2d9667c842 |
| g1r.2d      | p250            | 8a7bd5b4-7ac6-414b-ac62-a6c43229038a |
| g1r.4d      | p250            | e3abfbba-b9fe-4973-ac92-2856f489d09a |
| g1r.8d      | p750            | f5bfad0d-22d2-4e47-a807-8413e6d0818f |
| g1r.16d     | p750            | 396a0814-b339-4e9f-8d2f-ccef53937541 |
| g1r.30d     | p750            | a98b686a-4207-4bc2-902a-6f303da7b043 |
| g2i.1       | p50             | 474e2367-9c96-4fc0-ac41-eac7f59a1c7b |
| g2i.1s      | p50             | 410cc4c1-0684-47fa-9e72-866f1044a330 |
| g2i.2       | p250            | b7aa1635-3726-4924-9d73-18b9683fb67a |
| g2i.2s      | p250            | 79021845-f6de-46f0-be07-17835930d030 |
| g2i.4       | p250            | 8d705710-c7a8-4e64-aa96-87add166f42d |
| g2i.4s      | p250            | 88883131-9afa-43ad-bc4e-77b79494e89f |
| g2i.8       | p750            | 9c0a9d2d-79e9-4b99-a72f-e15c0d987e7d |
| g2i.8s      | p750            | f6303887-f07c-4976-9198-0eb0dd964b18 |
| g2i.16      | p750            | b1ca70ea-4da7-441d-8aef-8337c5f81fb2 |
| g2i.16s     | p750            | 901a56b3-4c98-4725-986a-2aa081b9c955 |
| m1.1        | p50             | 04b9be2d-afee-4f32-874b-8356b96ebb0b |
| m1.2        | p250            | 39d1344f-91dc-412a-a3b4-72e61b6c6eaa |
| m1.3        | p750            | baa5374c-cb97-47b9-be8c-e79103b3d097 |
| m1.4        | p750            | ccd8f298-d885-4c8b-bf40-414ee8f39967 |
| m1.5        | p750            | acd9945a-f94a-4e64-a4a3-81f587b596cf |
| m1.amphora  | p10             | osAyv1W3z2TU5D6h                     |
| m1a.1d      | p50             | a24a1a07-4c66-4030-bbfd-68368e4bb8be |
| m1a.2d      | p250            | d61f0a42-a3e0-4723-b78a-d51d3d719ade |
| m1a.4d      | p750            | 27b97158-43d6-44a1-8cf2-d95989f2cc07 |
| m1a.8d      | p750            | 9d76cf5e-1354-446f-8ca1-0d704bee89ca |
| m1a.16d     | p750            | e6897bc6-6a21-451c-b6cb-d80725781446 |
| m1a.32d     | p750            | 6fedeb8b-0536-4fbb-80b3-3791d00346b2 |
| m1a.60d     | p750            | e772e068-6400-4664-8813-811e835b169e |
| m1a.120d    | p750            | ae6ea4bb-f26c-41c1-a974-ff422601c0b6 |
| m2i.1       | p50             | aa603f7b-4214-486c-81ce-369535cef8ed |
| m2i.2       | p250            | b4be83e0-475c-4255-a5ef-e6876c413a09 |
| m2i.4       | p750            | 79b7b9a7-a062-4aab-9861-ba93c1935a68 |
| m2i.8       | p750            | 309471e2-21e8-4384-8ad9-484bf56372b3 |
| m2i.16      | p750            | c4d49baf-fcfa-4ec1-8160-9ec7e57123fc |
| s1.2        | p50             | d651f154-7fc9-4bf6-a34a-a32e3dd57c5d |
| s1.3        | p50             | 8b2c5d9f-e7da-4cd2-a359-e652817845fe |
| s1.4        | p250            | 709d7f73-41f0-4295-94d5-0f0cf351c93b |
| s1.5        | p750            | 178cf1ad-abcb-46c2-a045-73bc935f100e |
| s1.6        | p750            | 482434d3-3604-4f72-98d8-2af2c654d4e9 |
| n1.14d.g1   | p750            | 6f04a265-3a9f-4b99-bb66-944ac848af22 |
| n1.28d.g2   | p750            | 4b4891d6-860b-4c70-b48e-d261e9751b56 |
| n2.14d.g1   | p750            | 396671f4-6f19-4531-98d0-127272916cee |
| n2.28d.g2   | p750            | 0c85bbb2-5c85-465c-abf9-71261b8fbcd4 |
| n2.56d.g4   | p750            | a7c5c538-aef1-4f40-8c8d-1366c5b80271 |
| t1.1        | p10             | 25129382-dbe8-43eb-b71b-72253dd69452 |
| t1.2        | p10             | 85f57dd5-712b-489d-a0e3-4898c3962930 |
| t2i.1       | p10             | 22b37153-8817-4c85-9805-92426b2f903c |
