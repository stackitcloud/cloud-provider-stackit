# STACKIT CSI Driver User Documentation

## Table of Contents

- [Overview](#overview)
- [Key Features](#key-features)
- [Basic Usage](#basic-usage)
  - [Create a StorageClass](#create-a-storageclass)
  - [Create a PersistentVolumeClaim](#create-a-persistentvolumeclaim)
  - [Use the PVC in a Pod](#use-the-pvc-in-a-pod)
- [Configuration](#configuration)
  - [Topology Support](#topology-support)
  - [Volume Encryption](#volume-encryption)
  - [Volume Snapshots](#volume-snapshots)
  - [Volume Expansion](#volume-expansion)

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

### Supported Filesystems

We currently support the following file systems:

- ext3
- ext4 (default)
- xfs
- [EXPERIMENTAL] btrfs. Use at your own risk!

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
  type: "storage_premium_perf4"
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
parameters:
  type: "snapshot"
```

### Parameters

- `type`: (Optional) Defines the Cinder backend operation to perform. If not specified, it defaults to `"snapshot"`.

  - `type: "snapshot"` (Default)
    This is a fast, point-in-time copy stored on the **same storage backend** as the original volume.

    - **Best for:** Cloning volumes or fast, short-term rollbacks.
    - **Warning:** This is **not** a true backup. Failure of the storage backend will result in the loss of both the volume and its snapshots.

  - `type: "backup"`
    This creates a full, independent copy of the volume's data in a **separate repository**.
    - **Best for:** True disaster recovery and long-term data protection.
    - **Note:** This operation is slower as it copies all data to a different location.

### Volume Expansion

To expand volumes, the StorageClass must allow it:

```YAML
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: expandable-storage
provisioner: block-storage.csi.stackit.cloud
allowVolumeExpansion: true
```

To expand a volume, increase the storage request of the PVC:

```bash
kubectl patch pvc my-pvc -p '{"spec":{"resources":{"requests":{"storage":"20Gi"}}}}'
```

Volumes can be expanded while they are attached to a running Pod (online) or while they are not in use (offline). The driver resizes the volume first and then the filesystem on the node. For an offline volume, the filesystem is resized when a Pod mounts the volume again. Volumes cannot be shrunk.

The driver reports an expansion as successful only after the volume has the requested size. The STACKIT storage backend can reject a resize after it accepted the request, for example when it has not enough capacity. In this case:

- The PVC keeps its old capacity (`status.capacity`).
- The PVC gets a `VolumeResizeFailed` warning event with a message like `volume <id> has size 10 GiB after resize, requested 20 GiB`.
- Kubernetes retries the expansion automatically with an increasing delay. No action is necessary when the backend has capacity again.

Use `kubectl describe pvc my-pvc` to see the events.
