# STACKIT Cloud Provider Deployment Documentation

## Table of Contents

1. [Overview](#overview)
2. [Deployment Components](#deployment-components)
3. [Deployment Configuration](#deployment-configuration)
   - [Cloud Controller Manager Flags](#cloud-controller-manager-flags)
   - [CSI Driver Flags](#csi-driver-flags)
4. [Deployment Steps](#deployment-steps)
5. [Example Deployment](#example-deployment)
6. [Configuration Options](#configuration-options)
   - [Cloud Configuration](#cloud-configuration)
7. [Monitoring and Logging](#monitoring-and-logging)
   - [Metrics](#metrics)
   - [Logs](#logs)

## Overview

The STACKIT Cloud Provider includes both the Cloud Controller Manager (CCM) for managing cloud resources and the CSI driver for persistent storage. This deployment provides a unified solution for cloud integration and storage provisioning.

## Deployment Components

The deployment consists of the following components:

1. **ServiceAccount**: `stackit-cloud-controller-manager` with appropriate RBAC permissions
2. **Deployment**: Runs the cloud provider container with necessary configuration
3. **Service**: Exposes metrics and API endpoints

## Deployment Configuration

The deployment can be customized using the following flags:

### Cloud Controller Manager Flags

- `--cloud-provider=stackit`: Set the cloud provider to STACKIT.
- `--webhook-secure-port=0`: Disable cloud provider webhook.
- `--concurrent-service-syncs=3`: The number of services that are allowed to sync concurrently. Larger number = more responsive service management, but more CPU (and network) load.
- `--controllers=service-lb-controller`: Enable specific controllers.
- `authorization-always-allow-paths`
- `--leader-elect=true`: Enable leader election, see [Kube Controller Manager](https://kubernetes.io/docs/reference/command-line-tools-reference/kube-controller-manager/).
- `--leader-elect-resource-name=stackit-cloud-controller-manager`: Set leader election resource name, see [Kube Controller Manager](https://kubernetes.io/docs/reference/command-line-tools-reference/kube-controller-manager/).

### CSI Driver Flags

- `--endpoint`: CSI endpoint URL
- `--cloud-config`: Path to cloud configuration file
- `--cluster`: Cluster identifier
- `--http-endpoint`: HTTP server endpoint for metrics
- `--provide-controller-service`: Enable controller service (default: true)
- `--provide-node-service`: Enable node service (default: true)

## Deployment Steps

Apply the deployment using kustomize:

```bash
kubectl apply -k deploy/
```

## Example Deployment

Here's an example of a complete deployment configuration:

```YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: stackit-cloud-controller-manager
  namespace: kube-system
spec:
  replicas: 2
  selector:
    matchLabels:
      app: stackit-cloud-controller-manager
  template:
    metadata:
      labels:
        app: stackit-cloud-controller-manager
    spec:
      serviceAccountName: stackit-cloud-controller-manager
      containers:
      - name: stackit-cloud-controller-manager
        image: ghcr.io/stackitcloud/cloud-provider-stackit/cloud-controller-manager:release-v1.33
        args:
        # CCM flags
        - --cloud-provider=stackit
        - --webhook-secure-port=0
        - --concurrent-service-syncs=3
        - --controllers=service-lb-controller
        - --authorization-always-allow-paths=/metrics
        - --leader-elect=true
        - --leader-elect-resource-name=stackit-cloud-controller-manager
        # CSI flags
        - --endpoint=unix:///csi/csi.sock
        - --cloud-config=/etc/config/cloud-config.yaml
        - --cluster=my-cluster-id
        - --provide-controller-service=true
        - --provide-node-service=true
        ports:
        - containerPort: 10258
          hostPort: 10258
          name: https
          protocol: TCP
        - containerPort: 9090
          hostPort: 9090
          name: metrics
          protocol: TCP
        resources:
          limits:
            cpu: "0.5"
            memory: 500Mi
          requests:
            cpu: "0.1"
            memory: 100Mi
```

## Configuration Options

### Cloud Configuration

The cloud configuration file should be mounted at `/etc/config/cloud-config.yaml` and contain the necessary credentials and settings for accessing STACKIT services.

Example cloud configuration:

```yaml
# cloud-config.yaml
projectId: your-project-id
networkId: your-network-id
region: eu01
loadBalancerApi:
  url: https://load-balancer.api.eu01.stackit.cloud
```

## Monitoring and Logging

### Metrics

The cloud provider exposes metrics on port 9090. Configure your monitoring system to scrape these metrics for observability.

Example ServiceMonitor configuration for Prometheus Operator:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: stackit-cloud-controller-manager
  namespace: kube-system
  labels:
    release: prometheus
spec:
  selector:
    matchLabels:
      app: stackit-cloud-controller-manager
  endpoints:
    - port: metrics
      interval: 30s
      path: /metrics
```

### Logs

Cloud provider logs can be found in the Kubernetes controller manager pods. Enable verbose logging by setting the log level to debug.

Example log level configuration:

```yaml
args:
  - --v=4 # Debug log level
```
