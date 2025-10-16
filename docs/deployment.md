# STACKIT Cloud Provider Deployment Documentation

## Table of Contents

1. [Overview](#overview)
2. [Deployment Components](#deployment-components)
3. [Deployment Configuration](#deployment-configuration)
   - [Cloud Controller Manager Flags](#cloud-controller-manager-flags)
   - [CSI Driver Flags](#csi-driver-flags)
4. [Deployment Steps](#deployment-steps)
   - [1. Create RBAC Resources](#1-create-rbac-resources)
   - [2. Deploy the Cloud Provider](#2-deploy-the-cloud-provider)
   - [3. Create the Service](#3-create-the-service)
5. [Example Deployment](#example-deployment)
6. [Configuration Options](#configuration-options)
   - [Cloud Configuration](#cloud-configuration)
   - [Topology Configuration](#topology-configuration)
7. [Monitoring and Logging](#monitoring-and-logging)
   - [Metrics](#metrics)
   - [Logs](#logs)

## Overview

The STACKIT Cloud Provider includes both the Cloud Controller Manager (CCM) for managing cloud resources and the CSI driver for persistent storage. This deployment provides a unified solution for cloud integration and storage provisioning.

## Deployment Components

The deployment consists of the following components:

1. **ServiceAccount**: `cloud-provider-stackit` with appropriate RBAC permissions
2. **Deployment**: Runs the cloud provider container with necessary configuration
3. **Service**: Exposes metrics and API endpoints

## Deployment Configuration

The deployment can be customized using the following flags:

### Cloud Controller Manager Flags

- `--allow-untagged-cloud`: Allow untagged cloud resources
- `--cloud-provider=stackit`: Set the cloud provider to STACKIT
- `--route-reconciliation-period=30s`: Set route reconciliation period
- `--webhook-secure-port=0`: Disable webhook secure port
- `--leader-elect=true`: Enable leader election
- `--leader-elect-resource-name=stackit-cloud-controller-manager`: Set leader election resource name
- `--concurrent-service-syncs=3`: Set number of concurrent service syncs
- `--controllers=service-lb-controller`: Enable specific controllers

### CSI Driver Flags

- `--endpoint`: CSI endpoint URL
- `--cloud-config`: Path to cloud configuration file
- `--with-topology`: Enable topology awareness (default: true)
- `--additional-topology`: Additional topology keys for volume placement
- `--cluster`: Cluster identifier
- `--http-endpoint`: HTTP server endpoint for metrics
- `--provide-controller-service`: Enable controller service (default: true)
- `--provide-node-service`: Enable node service (default: true)

## Deployment Steps

### 1. Create RBAC Resources

Apply the RBAC configuration to create the necessary ServiceAccount and ClusterRoleBinding:

```bash
kubectl apply -f deploy/cloud-provider-stackit/rbac.yaml
```

### 2. Deploy the Cloud Provider

Apply the deployment configuration:

```bash
kubectl apply -f deploy/cloud-provider-stackit/deployment.yaml
```

### 3. Create the Service

Apply the service configuration:

```bash
kubectl apply -f deploy/cloud-provider-stackit/service.yaml
```

## Example Deployment

Here's an example of a complete deployment configuration:

```YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cloud-provider-stackit
  namespace: kube-system
spec:
  replicas: 2
  selector:
    matchLabels:
      app: cloud-provider-stackit
  template:
    metadata:
      labels:
        app: cloud-provider-stackit
    spec:
      serviceAccountName: cloud-provider-stackit
      containers:
      - name: cloud-provider-stackit
        image: registry.ske.stackit.cloud/stackitcloud/cloud-provider-stackit/cloud-provider-stackit:latest
        args:
        # CCM flags
        - --allow-untagged-cloud
        - --cloud-provider=stackit
        - --route-reconciliation-period=30s
        - --webhook-secure-port=0
        - --leader-elect=true
        - --leader-elect-resource-name=stackit-cloud-controller-manager
        - --concurrent-service-syncs=3
        - --controllers=service-lb-controller
        # CSI flags
        - --endpoint=unix:///csi/csi.sock
        - --cloud-config=/etc/stackit/cloud-config.yaml
        - --with-topology=true
        - --additional-topology=topology.kubernetes.io/region=REGION1
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

The cloud configuration file should be mounted at `/etc/stackit/cloud-config.yaml` and contain the necessary credentials and settings for accessing STACKIT services.

Example cloud configuration:

```yaml
# cloud-config.yaml
projectId: your-project-id
networkId: your-network-id
region: eu01
loadBalancerApi:
  url: https://load-balancer.api.eu01.stackit.cloud
```

### Topology Configuration

The driver supports topology-aware volume placement. Configure topology using the `--with-topology` and `--additional-topology` flags.

Example with multiple topology keys:

```bash
--with-topology=true
--additional-topology=topology.kubernetes.io/region=REGION1,topology.kubernetes.io/zone=ZONE1
```

## Monitoring and Logging

### Metrics

The cloud provider exposes metrics on port 9090. Configure your monitoring system to scrape these metrics for observability.

Example ServiceMonitor configuration for Prometheus Operator:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: cloud-provider-stackit
  namespace: kube-system
  labels:
    release: prometheus
spec:
  selector:
    matchLabels:
      app: cloud-provider-stackit
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
