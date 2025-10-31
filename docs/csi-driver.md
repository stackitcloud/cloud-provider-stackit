# STACKIT CSI Driver User Documentation

## Table of Contents

1. [Overview](#overview)
2. [Key Features](#key-features)
3. [Basic Usage](#basic-usage)
   - [Create a StorageClass](#create-a-storageclass)
   - [Create a PersistentVolumeClaim](#create-a-persistentvolumeclaim)
   - [Use the PVC in a Pod](#use-the-pvc-in-a-pod)
4. [Configuration](#configuration)
   - [Topology Support](#topology-support)
   - [Volume Encryption](#volume-encryption)

## Overview

The CSI driver enables dynamic provisioning and management of persistent volumes in Kubernetes using STACKIT's block storage services. It follows the CSI specification to ensure compatibility with Kubernetes and other container orchestration systems.

## Key Features

- Dynamic provisioning of persistent volumes
- Volume snapshotting and restoration
- Topology-aware volume placement
- Integration with Kubernetes CSI sidecars
- Volume encryption support
- Volume expansion capabilities

## Basic Usage

### Create a StorageClass

```YAML
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: premium-perf4-stackit
provisioner: block-storage.csi.stackit.cloud
parameters:
  type: "storage_premium_perf4"
```

### Create a PersistentVolumeClaim

```YAML
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: stackit-block-storage
```

### Use the PVC in a Pod

```YAML
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
spec:
  containers:
    - name: my-container
      image: nginx
      volumeMounts:
        - mountPath: "/data"
          name: my-volume
  volumes:
    - name: my-volume
      persistentVolumeClaim:
        claimName: my-pvc
```

## Configuration

### Topology Support

The driver supports topology-aware volume placement. The `GetAZFromTopology` function extracts the availability zone from topology requirements passed by Kubernetes.

Example topology requirement:

```YAML
storageClass:
  volumeBindingMode: WaitForFirstConsumer
  allowedTopologies:
    - matchLabelExpressions:
        - key: topology.block-storage.csi.stackit.cloud/zone
          values:
            - zone1
            - zone2
```

### Volume Encryption

The driver supports volume encryption with the following parameters:

- `encrypted`: Boolean to enable encryption
- `kmsKeyID`: KMS key ID for encryption
- `kmsKeyringID`: KMS keyring ID
- `kmsKeyVersion`: KMS key version
- `kmsServiceAccount`: KMS service account

Example StorageClass with encryption:

```YAML
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: encrypted-storage
provisioner: block-storage.csi.stackit.cloud
parameters:
  type: "storage_premium_perf4_encrypted"
  encrypted: "true"
  kmsKeyID: "your-kms-key-id"
  kmsKeyringID: "your-keyring-id"
  kmsKeyVersion: "1"
  kmsServiceAccount: "your-service-account"
```

### Volume Snapshots

This feature enables creating volume snapshots and restoring volumes from snapshots. The corresponding CSI feature (VolumeSnapshotDataSource) has been generally available since Kubernetes v1.20.

To use this feature, deploy the snapshot-controller and CRDs as part of your Kubernetes cluster management process (independent of any CSI Driver). For more information, refer to the [Snapshot Controller](https://kubernetes-csi.github.io/docs/snapshot-controller.html) documentation.

It is also required to create a `SnapshotClass` for example:

```Yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshotClass
metadata:
  name: stackit
driver: block-storage.csi.stackit.cloud
deletionPolicy: Delete
```
