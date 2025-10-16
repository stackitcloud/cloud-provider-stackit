# cloud-provider-stackit

[![GitHub License](https://img.shields.io/github/license/stackitcloud/stackit-sdk-go)](https://www.apache.org/licenses/LICENSE-2.0)

This repository includes:

- Cloud Controller Manager (CCM)
- STACKIT CSI driver
- Kubernetes Resources for the Manager
- Ginko bootstrapped Test Suite

Does not include:

- readyz and healthz
- Kubernetes Client with self authorization by `inClusterConfig`

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

## User Documentation

- [Load Balancer](https://github.com/stackitcloud/cloud-provider-stackit/docs/load-balancer.md)
- [CSI Driver](https://github.com/stackitcloud/cloud-provider-stackit/docs/csi-driver.md)
