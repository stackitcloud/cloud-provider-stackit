# Testing

## Table of Contents

- [Bootstrapping a Kubeadm Test Environment](#bootstrapping-a-kubeadm-test-environment)
  - [Prerequisites](#prerequisites)
  - [Script Reference](#script-reference)
  - [Creating a Cluster](#creating-a-cluster)
  - [Accessing the Cluster](#accessing-the-cluster)
  - [Destroying the Cluster](#destroying-the-cluster)
  - [Testing Custom Branches or Images](#testing-custom-branches-or-images)
- [Running End-to-End (E2E) Tests for the CSI Driver](#running-end-to-end-e2e-tests-for-the-csi-driver)
  - [Parallel E2E Test Suite](#parallel-e2e-test-suite)
  - [Sequential E2E Test Suite (Snapshots & Backups)](#sequential-e2e-test-suite-snapshots--backups)
  - [Customizing Test Execution](#customizing-test-execution)
  - [Full Example](#full-example)

## Bootstrapping a Kubeadm Test Environment

To run the E2E tests, you first need a Kubernetes cluster. The script located at `test/e2e/e2e-test-script.sh` (this path is assumed, please adjust if it's located elsewhere) automates the creation and destruction of a single-node Kubeadm cluster on a STACKIT VM.

This script handles provisioning all necessary STACKIT resources, including:

- VM (Server)
- Network
- Security Group (with rules for SSH and K8s API)
- Public IP
- Service Account and Key
- SSH Key (in STACKIT)

It then configures the VM with `kubeadm`, installs Calico CNI, and deploys the STACKIT CCM and CSI driver.

### Prerequisites

Before running the script, you must have the following installed and configured on your local machine:

1. **STACKIT CLI:** The `stackit` command-line tool.
   - You must be authenticated. Run `stackit auth login` if you haven't already.
2. **jq:** The `jq` command-line JSON processor.
3. **SSH Key Pair:** The script needs an SSH key pair to access the VM.

   - By default, it looks for `$HOME/.ssh/stackit-ccm-test.pub` and `$HOME/.ssh/stackit-ccm-test`.
   - You can generate a new key pair with:

     ```bash
     ssh-keygen -f $HOME/.ssh/stackit-ccm-test -t rsa -b 4096
     ```

   - If you want to use a different key, you can set the `E2E_SSH_KEY_NAME` environment variable (e.g., `E2E_SSH_KEY_NAME="my-other-key"`).

### Script Reference

You can control other parameters, such as the VM name or machine type, via environment variables. For a full list of commands and environment variables, run the script with the `--help` flag.

```bash
$ ./test/e2e/e2e.sh --help
Usage: ./test/e2e/e2e.sh <create|destroy> [options]

Actions:
  create    Create a new Kubernetes test environment.
  destroy   Destroy an existing Kubernetes test environment.

Options:
  --project-id <ID>              STACKIT Project ID. (Required for create & destroy)
  --kubernetes-version <VERSION> Kubernetes version (e.g., 1.34.1). (Required for create)
  --help                         Show this help message.

Environment Variables (Optional Overrides):
  E2E_MACHINE_NAME:     Name for the VM, network, SA, and security group.
                        (Default: "stackit-ccm-test")
  E2E_MACHINE_TYPE:     STACKIT machine type for the VM.
                        (Default: "c2i.4")
  E2E_SSH_KEY_NAME:     Name of the SSH key pair to use (must exist at $HOME/.ssh/<name>).
                        (Default: value of E2E_MACHINE_NAME)
  E2E_NETWORK_NAME:     Name of the STACKIT network to create or use.
                        (Default: value of E2E_MACHINE_NAME)
  E2E_DEPLOY_BRANCH:    Specify a git branch for the CCM/CSI manifests.
                        (Default: auto-detects 'release-vX.Y' or 'main')
  E2E_DEPLOY_CCM_IMAGE: Specify a full container image ref to override the CCM deployment.
                        (Default: uses the image from the kustomize base)
  E2E_DEPLOY_CSI_IMAGE: Specify a full container image ref to override the CSI plugin.
                        (Default: uses the image from the kustomize base)
```

### Creating a Cluster

To create and provision the entire test environment, run the `create` command:

```bash

# Assumes the script is at test/e2e/e2e-test-script.sh

./test/e2e/e2e-test-script.sh create \
 --project-id <STACKIT_PROJECT_ID> \
 --kubernetes-version <K8S_VERSION>
```

- **`<STACKIT_PROJECT_ID>`:** Your STACKIT Project UUID.
- **`<K8S_VERSION>`:** The specific Kubernetes version to install (e.g., `1.29.2`).

This process will take several minutes. The script will create all resources, wait for the VM to be ready, install Kubernetes, and deploy the STACKIT components.

### Accessing the Cluster

Upon successful creation, the script generates a `kubeconfig` file locally.

- **Kubeconfig Path:** `test/e2e/kubeconfig-<PROJECT_ID>-<MACHINE_NAME>.yaml`
- **SA Key Path:** `test/e2e/sa-key-<PROJECT_ID>-<MACHINE_NAME>.json`
- **Inventory Path:** `test/e2e/inventory-<PROJECT_ID>-<MACHINE_NAME>.json` (Used for deletion)

You can export the `KUBECONFIG` variable to interact with your new cluster:

```bash
export KUBECONFIG=$PWD/test/e2e/kubeconfig-<PROJECT_ID>-<MACHINE_NAME>.yaml
kubectl get nodes
```

### Destroying the Cluster

To tear down all resources created by the script (including the VM, network, SA, etc.), use the `destroy` command.

```bash
./test/e2e/e2e-test-script.sh destroy --project-id <STACKIT_PROJECT_ID>
```

The script uses the `inventory-....json` file to identify which resources to delete.

### Testing Custom Branches or Images

The bootstrap script is ideal for testing changes in a feature branch or custom-built images. You can use environment variables to override the deployment defaults.

#### Testing a Specific Git Branch

Use **`E2E_DEPLOY_BRANCH`** to instruct the provisioner to pull the CCM/CSI manifests from a specific git branch (e.g., a feature branch or a release branch) instead of the default.

```bash

# This will create a cluster using manifests from the 'release-v1.31' branch

E2E_DEPLOY_BRANCH="release-v1.31" \
./test/e2e/e2e-test-script.sh create \
 --project-id <STACKIT_PROJECT_ID> \
 --kubernetes-version <K8S_VERSION>
```

#### Testing a Custom CSI Driver Image

Use **`E2E_DEPLOY_CSI_IMAGE`** to override the CSI driver image.

```bash

# This will create a cluster and deploy your custom-built CSI image

E2E_DEPLOY_CSI_IMAGE="ghcr.io/stackitcloud/cloud-provider-stackit/stackit-csi-plugin:v1.31.6-4-gf2d85f1" \
./test/e2e/e2e-test-script.sh create \
 --project-id <STACKIT_PROJECT_ID> \
 --kubernetes-version <K8S_VERSION>
```

#### Testing a Custom CCM Image

Use **`E2E_DEPLOY_CCM_IMAGE`** to override the Cloud Controller Manager image.

```bash

# This will create a cluster and deploy your custom-built CCM image

E2E_DEPLOY_CCM_IMAGE="ghcr.io/stackitcloud/cloud-provider-stackit/cloud-controller-manager:v1.31.6-4-gf2d85f1" \
./test/e2e/e2e-test-script.sh create \
 --project-id <STACKIT_PROJECT_ID> \
 --kubernetes-version <K8S_VERSION>
```

## Running End-to-End (E2E) Tests for the CSI Driver

The CSI E2E test suite validates the full functionality of the CSI driver. Tests are divided into parallel and sequential execution sets to accommodate different operational requirements, particularly for stateful operations like snapshots.

> :warning: Make sure that the kubernetes version of the e2e test (hack/tools.mk@KUBERNETES_TEST_VERSION) matches your kubernetes version!

### Parallel E2E Test Suite

To execute the main CSI E2E test suite, which covers most driver features, use the following command. This suite runs tests in parallel to maximize efficiency.

- **Command:** `make verify-e2e-csi-parallel`
- **Scope:** Tests a broad spectrum of the CSI driver's core features (e.g., volume provisioning, mounting, unmounting, deletion), except `VolumeSnapshot`.

### Sequential E2E Test Suite (Snapshots & Backups)

Specific tests related to `VolumeSnapshot` must be run sequentially to ensure proper state management and ordering of operations.

### Prerequisite: VolumeSnapshotClass

Before running the sequential tests, the necessary `VolumeSnapshotClass` resource must be applied to the Kubernetes cluster. This configuration ensures the CSI driver is correctly configured to handle snapshot operations.

Example:

```yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshotClass
metadata:
  name: stackit
driver: block-storage.csi.stackit.cloud
deletionPolicy: Delete
parameters:
  type: "snapshot"
```

> You can also run the same test by using the `type: "backup"` parameter instead. This will run the same test against STACKIT Backups instead of Snapshots.

You can apply this manifest using `kubectl apply -f <filename.yaml>` or ensuring it is present in the cluster.

### Running the Sequential Tests

- **Command:** `make verify-e2e-csi-sequential`
- **Scope:** Specifically targets CSI operations related to volume state, including Snapshots and Restores (Backups).
- **Requirement:** The `VolumeSnapshotClass` (as defined above) must be present in the cluster.

### Customizing Test Execution

The test configuration file allows for granular control over which specific tests are executed. This is useful for debugging or targeting a subset of features.

- **Configuration File:** `test/e2e/csi/block-storage.yaml`
- **Action:** Modify this YAML file to include or exclude specific test cases, adjust parameters, or change timeout settings for the E2E runs. This allows for focused testing without running the entire suite.

### Full Example

The full example is a walk-trough to run the E2E CSI Driver tests for a specific Kubernetes Version v1.32.9.

```bash
# Create a single-node test cluster for Kubernetes v1.32.9
#
# E2E_DEPLOY_BRANCH will auto-detect the Kubernetes version from --kubernetes-version flag
# and use the corresponding release-vX.Y branch. This can be customized by using E2E_DEPLOY_BRANCH.
$ E2E_MACHINE_NAME=stackit-ccm-test-1-32 ./test/e2e/e2e.sh create \
  --project-id <ID> \
  --kubernetes-version 1.32.9

# Check node and pod status
$ kubectl get nodes
NAME                    STATUS   ROLES           AGE    VERSION
stackit-ccm-test-1-32   Ready    control-plane   4d1h   v1.32.9

# Ensure csi-stackit-* and stackit-cloud-controller-manager-* pods are running and healthy
$ kubectl get pods -n kube-system
NAME                                              READY   STATUS    RESTARTS       AGE
coredns-66bc5c9577-dxvk4                          1/1     Running   0              4d1h
coredns-66bc5c9577-gzkt5                          1/1     Running   0              4d1h
csi-stackit-controllerplugin-5844c9df74-9s4x6     6/6     Running   0              21h
csi-stackit-nodeplugin-rrffk                      3/3     Running   0              21h
etcd-stackit-ccm-test-1-32                        1/1     Running   0              4d1h
kube-apiserver-stackit-ccm-test-1-32              1/1     Running   0              4d1h
kube-controller-manager-stackit-ccm-test-1-32     1/1     Running   0              4d1h
kube-proxy-4pg89                                  1/1     Running   0              4d1h
kube-scheduler-stackit-ccm-test-1-32              1/1     Running   0              4d1h
snapshot-controller-5b7776766f-cjjgv              1/1     Running   0              4d1h
snapshot-controller-5b7776766f-mp8dp              1/1     Running   0              4d1h
stackit-cloud-controller-manager-9cbc5fb6-cvj5f   1/1     Running   0              21h

# Run parallel test suite, ensure KUBERNETES_TEST_VERSION matches the cluster Kubernetes version
$ KUBERNETES_TEST_VERSION=1.32.9 make verify-e2e-csi-parallel

# The test result should look like this
[...]
Ran 61 of 7450 Specs in 426.519 seconds
SUCCESS! -- 61 Passed | 0 Failed | 0 Pending | 7389 Skipped
```
