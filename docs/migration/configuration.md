# Migration Guide: CCM and CSI Configuration to Unified YAML Format

## Introduction

This guide provides step-by-step instructions for migrating from the legacy Cloud Controller Manager (CCM) and Container Storage Interface (CSI) configurations to the new unified YAML-based configuration format.

## Overview

The migration involves:

- Updating CCM and CSI configurations to use the new unified YAML format
- Mapping legacy configuration keys to new schema
- Ensuring compatibility with the latest version of the STACKIT Kubernetes integration

**Note**: While the new format uses a unified structure, CCM and CSI configurations remain in separate files but follow the same YAML schema.

## Removed Configuration Options

The following configuration options have been removed in the new format:

### CCM

- `nonStackitClassNames` - No longer supported

### CSI

- `node-volume-attach-limit` - No longer configurable

## Old Configuration Reference

### CCM Configuration

```yaml
# cloudprovider.yaml
projectId: my-stackit-project-id
networkId: my-stackit-network-id
region: eu01
nonStackitClassNames: my-non-stackit-class
extraLabels:
  key1: value1
  key2: value2
metadata:
  searchOrder: "configDrive,metadataService"
  requestTimeout: "5s"
loadBalancerApi:
  url: https://loadbalancer.example.com
```

### CSI Configuration

```ini
# cloudprovider.conf
[Global]
project-id = "my-stackit-project-id"
iaas-api-url = "https://iaas.example.com"
[Metadata]
search-order = "configDrive,metadataService"
request-timeout = "5s"
[BlockStorage]
node-volume-attach-limit = 20
rescan-on-resize = true
```

## New Configuration Reference

### CCM Configuration

```yaml
# cloudprovider.yaml
global:
  projectId: my-stackit-project-id
  region: eu01
metadata:
  searchOrder: "configDrive,metadataService"
  requestTimeout: "5s"
loadBalancer:
  api:
    url: https://loadbalancer.example.com
  networkId: my-stackit-network-id
  extraLabels:
    key1: value1
    key2: value2
```

### CSI Configuration

```yaml
# cloudprovider.conf
global:
  projectId: my-stackit-project-id
  iaasApi: https://iaas.example.com
metadata:
  searchOrder: "configDrive,metadataService"
  requestTimeout: "5s"
blockStorage:
  rescanOnResize: true
```
