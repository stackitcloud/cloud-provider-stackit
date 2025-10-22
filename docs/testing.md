# Testing

## 🧪 Running End-to-End (E2E) Tests for the CSI Driver
The CSI E2E test suite validates the full functionality of the CSI driver. Tests are divided into parallel and sequential execution sets to accommodate different operational requirements, particularly for stateful operations like snapshots.

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

You can apply this manifest using kubectl apply -f <filename.yaml> or ensuring it is present in the cluster.

### Running the Sequential Tests
- **Command:** `make verify-e2e-csi-sequential`
- **Scope:** Specifically targets CSI operations related to volume state, including Snapshots and Restores (Backups).
- **Requirement:** The VolumeSnapshotClass (as defined above) must be present in the cluster.

### Customizing Test Execution
The test configuration file allows for granular control over which specific tests are executed. This is useful for debugging or targeting a subset of features.

- **Configuration File:** `test/e2e/csi/block-storage.yaml`
- **Action:** Modify this YAML file to include or exclude specific test cases, adjust parameters, or change timeout settings for the E2E runs. This allows for focused testing without running the entire suite.