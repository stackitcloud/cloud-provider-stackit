package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/stackiterrors"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	"github.com/stackitcloud/stackit-sdk-go/core/runtime"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	sdkWait "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api/wait"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

type IaaSClient interface {
	GetServer(ctx context.Context, serverID string) (*iaas.Server, error)
	DeleteServer(ctx context.Context, serverID string) error
	CreateServer(ctx context.Context, create *iaas.CreateServerPayload) (*iaas.Server, error)
	UpdateServer(ctx context.Context, serverID string, update iaas.UpdateServerPayload) (*iaas.Server, error)
	ListServers(ctx context.Context) (*[]iaas.Server, error)

	CreateSnapshot(ctx context.Context, payload *iaas.CreateSnapshotPayload) (*iaas.Snapshot, error)
	ListSnapshots(ctx context.Context, filters map[string]string) ([]iaas.Snapshot, string, error)
	DeleteSnapshot(ctx context.Context, snapshotID string) error
	GetSnapshot(ctx context.Context, snapshotID string) (*iaas.Snapshot, error)
	WaitSnapshotReady(ctx context.Context, snapshotID string) (*string, error)

	CreateBackup(ctx context.Context, name, volID, snapshotID string, tags map[string]string) (*iaas.Backup, error)
	ListBackups(ctx context.Context, filters map[string]string) ([]iaas.Backup, error)
	DeleteBackup(ctx context.Context, backupID string) error
	GetBackup(ctx context.Context, backupID string) (*iaas.Backup, error)
	WaitBackupReady(ctx context.Context, backupID string, snapshotSize int64, backupMaxDurationSecondsPerGB int) (*string, error)

	CreateVolume(ctx context.Context, payload *iaas.CreateVolumePayload) (*iaas.Volume, error)
	DeleteVolume(ctx context.Context, volumeID string) error
	AttachVolume(ctx context.Context, serverID, volumeID string, payload iaas.AddVolumeToServerPayload) (string, error)
	DetachVolume(ctx context.Context, serverID, volumeID string) error
	GetVolume(ctx context.Context, volumeID string) (*iaas.Volume, error)
	GetVolumesByName(ctx context.Context, volName string) ([]iaas.Volume, error)
	GetVolumeByName(ctx context.Context, name string) (*iaas.Volume, error)
	ListVolumes(ctx context.Context, _ int, _ string) ([]iaas.Volume, string, error)
	ExpandVolume(ctx context.Context, volumeID, volumeStatus string, payload iaas.ResizeVolumePayload) error
	WaitVolumeTargetStatus(ctx context.Context, volumeID string, tStatus []string) error
	WaitDiskAttached(ctx context.Context, instanceID, volumeID string) error
	WaitDiskDetached(ctx context.Context, instanceID, volumeID string) error
	WaitVolumeTargetStatusWithCustomBackoff(ctx context.Context, volumeID string, tStatus []string, backoff *wait.Backoff) error
}

const (
	VolumeAvailableStatus    = "AVAILABLE"
	VolumeAttachedStatus     = "ATTACHED"
	operationFinishInitDelay = 1 * time.Second
	operationFinishFactor    = 1.1
	operationFinishSteps     = 10
	diskAttachInitDelay      = 1 * time.Second
	diskAttachFactor         = 1.2
	diskAttachSteps          = 15
	diskDetachInitDelay      = 1 * time.Second
	diskDetachFactor         = 1.2
	diskDetachSteps          = 13
	VolumeDescription        = "Created by STACKIT CSI driver"
)

const (
	BackupDescription                    = "Created by STACKIT CSI driver"
	backupReadyStatus                    = "AVAILABLE"
	backupErrorStatus                    = "error"
	BackupMaxDurationSecondsPerGBDefault = 20
	BackupMaxDurationPerGB               = "backup-max-duration-seconds-per-gb"
	backupBaseDurationSeconds            = 30
	backupReadyCheckIntervalSeconds      = 7
)

const (
	SnapshotReadyStatus = "AVAILABLE"
	snapReadyDuration   = 1 * time.Second
	snapReadyFactor     = 1.2
	snapReadySteps      = 10

	SnapshotType = "type"
)

type VolumeSourceTypes string

const (
	VolumeSource   VolumeSourceTypes = "volume"
	SnapshotSource VolumeSourceTypes = "snapshot"
	BackupSource   VolumeSourceTypes = "backup"
)

var volumeErrorStates = [...]string{"ERROR", "ERROR_RESIZING", "ERROR_DELETING"}

type iaasClient struct {
	Client    iaas.DefaultAPI
	projectID string
	region    string
}

func NewIaaSClient(region, projectID string, options []sdkconfig.ConfigurationOption) (IaaSClient, error) {
	apiClient, err := iaas.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}
	return &iaasClient{
		Client:    apiClient.DefaultAPI,
		projectID: projectID,
		region:    region,
	}, nil
}

func (i iaasClient) GetServer(ctx context.Context, serverID string) (*iaas.Server, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	server, err := i.Client.GetServer(ctx, i.projectID, i.region, serverID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return server, nil
}

func (i iaasClient) DeleteServer(ctx context.Context, serverID string) error {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	err := i.Client.DeleteServer(ctx, i.projectID, i.region, serverID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return err
	}

	return nil
}

//nolint:dupl // SDK request execution and response-ID wrapping pattern intentionally repeated for typed API methods.
func (i iaasClient) CreateServer(ctx context.Context, create *iaas.CreateServerPayload) (*iaas.Server, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	server, err := i.Client.
		CreateServer(ctx, i.projectID, i.region).
		CreateServerPayload(*create).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return server, nil
}

func (i iaasClient) UpdateServer(ctx context.Context, serverID string, update iaas.UpdateServerPayload) (*iaas.Server, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	server, err := i.Client.
		UpdateServer(ctx, i.projectID, i.region, serverID).
		UpdateServerPayload(update).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return server, nil
}

func (i iaasClient) ListServers(ctx context.Context) (*[]iaas.Server, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	resp, err := i.Client.ListServers(ctx, i.projectID, i.region).Details(true).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return &resp.Items, nil
}

//nolint:dupl // SDK request execution and response-ID wrapping pattern intentionally repeated for typed API methods.
func (i iaasClient) CreateSnapshot(ctx context.Context, payload *iaas.CreateSnapshotPayload) (*iaas.Snapshot, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	snapshot, err := i.Client.
		CreateSnapshot(ctx, i.projectID, i.region).
		CreateSnapshotPayload(*payload).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return snapshot, nil
}

func (i iaasClient) ListSnapshots(ctx context.Context, filters map[string]string) ([]iaas.Snapshot, string, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	snaps, err := i.Client.ListSnapshotsInProject(ctx, i.projectID, i.region).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, "", stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, "", err
	}

	filteredSnapshots := FilterSnapshots(snaps.Items, filters)

	return filteredSnapshots, "", nil
}

func (i iaasClient) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	err := i.Client.DeleteSnapshot(ctx, i.projectID, i.region, snapshotID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return err
	}

	return nil
}

func (i iaasClient) GetSnapshot(ctx context.Context, snapshotID string) (*iaas.Snapshot, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	snapshot, err := i.Client.GetSnapshot(ctx, i.projectID, i.region, snapshotID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return snapshot, nil
}

func (i iaasClient) WaitSnapshotReady(ctx context.Context, snapshotID string) (*string, error) {
	backoff := wait.Backoff{
		Duration: snapReadyDuration,
		Factor:   snapReadyFactor,
		Steps:    snapReadySteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		ready, err := i.snapshotIsReady(ctx, snapshotID)
		if err != nil {
			return false, err
		}

		return ready, nil
	})

	if wait.Interrupted(err) {
		err = fmt.Errorf("timeout, Snapshot %s is still not Ready %v", snapshotID, err)
	}

	snap, _ := i.GetSnapshot(ctx, snapshotID)

	if snap != nil {
		return snap.Status, err
	}

	return new("Failed to get Snapshot status"), err
}

func (i iaasClient) snapshotIsReady(ctx context.Context, snapshotID string) (bool, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	snapshot, err := i.Client.GetSnapshot(ctx, i.projectID, i.region, snapshotID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return false, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return false, err
	}

	return *snapshot.Status == SnapshotReadyStatus, nil
}

func (i iaasClient) CreateBackup(ctx context.Context, name, volID, snapshotID string, tags map[string]string) (*iaas.Backup, error) {
	payload, err := BuildCreateBackupPayload(name, volID, snapshotID, tags)
	if err != nil {
		return nil, err
	}

	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	backup, err := i.Client.
		CreateBackup(ctx, i.projectID, i.region).
		CreateBackupPayload(payload).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return backup, nil
}

func BuildCreateBackupPayload(name, volID, snapshotID string, tags map[string]string) (iaas.CreateBackupPayload, error) {
	if name == "" {
		return iaas.CreateBackupPayload{}, errors.New("backup name cannot be empty")
	}

	if volID == "" && snapshotID == "" {
		return iaas.CreateBackupPayload{}, errors.New("either volID or snapshotID must be provided")
	}

	var backupSource VolumeSourceTypes
	var backupSourceID string
	if volID != "" {
		backupSource = VolumeSource
		backupSourceID = volID
	}
	if snapshotID != "" {
		backupSource = SnapshotSource
		backupSourceID = snapshotID
	}

	opts := iaas.CreateBackupPayload{
		Name:        new(name),
		Description: new(BackupDescription),
		Source: iaas.BackupSource{
			Type: string(backupSource),
			Id:   backupSourceID,
		},
	}
	if tags != nil {
		opts.Labels = LabelsFromTags(tags)
	}

	return opts, nil
}

func (i iaasClient) ListBackups(ctx context.Context, filters map[string]string) ([]iaas.Backup, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	backups, err := i.Client.ListBackups(ctx, i.projectID, i.region).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	filteredBackups := FilterBackups(backups.Items, filters)

	return filteredBackups, nil
}

func (i iaasClient) DeleteBackup(ctx context.Context, backupID string) error {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	err := i.Client.DeleteBackup(ctx, i.projectID, i.region, backupID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return err
	}

	return nil
}

func (i iaasClient) GetBackup(ctx context.Context, backupID string) (*iaas.Backup, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	backup, err := i.Client.GetBackup(ctx, i.projectID, i.region, backupID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return backup, nil
}

func (i iaasClient) WaitBackupReady(ctx context.Context, backupID string, snapshotSize int64, backupMaxDurationSecondsPerGB int) (*string, error) {
	duration := time.Duration(int64(backupMaxDurationSecondsPerGB)*snapshotSize + backupBaseDurationSeconds)
	err := i.waitBackupReadyWithContext(backupID, duration)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("timeout, Backup %s is still not Ready: %w", backupID, err)
		}
		return nil, err
	}

	backup, err := i.GetBackup(ctx, backupID)
	if err != nil {
		return nil, err
	}

	if backup != nil {
		return backup.Status, nil
	}

	return new("Failed to get backup status"), nil
}

func (i iaasClient) waitBackupReadyWithContext(backupID string, duration time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), duration*time.Second)
	defer cancel()
	var done bool
	var err error
	ticker := time.NewTicker(backupReadyCheckIntervalSeconds * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			done, err = i.backupIsReady(ctx, backupID)
			if err != nil {
				return err
			}

			if done {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Supporting function for waitBackupReadyWithContext().
// Returns true when the backup is ready.
func (i iaasClient) backupIsReady(ctx context.Context, backupID string) (bool, error) {
	backup, err := i.GetBackup(ctx, backupID)
	if err != nil {
		return false, err
	}

	if *backup.Status == backupErrorStatus {
		return false, errors.New("backup is in error state")
	}

	return *backup.Status == backupReadyStatus, nil
}

func (i iaasClient) CreateVolume(ctx context.Context, payload *iaas.CreateVolumePayload) (*iaas.Volume, error) {
	payload.Description = new(VolumeDescription)

	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	volume, err := i.Client.CreateVolume(ctx, i.projectID, i.region).CreateVolumePayload(*payload).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return volume, nil
}

func (i iaasClient) DeleteVolume(ctx context.Context, volumeID string) error {
	used, err := i.diskIsUsed(ctx, volumeID)
	if err != nil {
		return err
	}
	if used {
		return fmt.Errorf("cannot delete the volume %q, it's still attached to a node", volumeID)
	}

	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	err = i.Client.DeleteVolume(ctx, i.projectID, i.region, volumeID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return err
	}

	return nil
}

func (i iaasClient) AttachVolume(ctx context.Context, serverID, volumeID string, payload iaas.AddVolumeToServerPayload) (string, error) {
	volume, err := i.GetVolume(ctx, volumeID)
	if err != nil {
		return "", err
	}

	if volume.ServerId != nil && serverID == *volume.ServerId {
		klog.V(4).Infof("Disk %s is already attached to instance %s", volumeID, serverID)
		return *volume.Id, nil
	}

	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	if _, err = i.Client.
		AddVolumeToServer(ctx, i.projectID, i.region, serverID, volumeID).
		AddVolumeToServerPayload(payload).
		Execute(); err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return "", stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return "", err
	}

	return volume.GetId(), nil
}

func (i iaasClient) GetVolume(ctx context.Context, volumeID string) (*iaas.Volume, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	volume, err := i.Client.GetVolume(ctx, i.projectID, i.region, volumeID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return volume, nil
}

func (i iaasClient) GetVolumesByName(ctx context.Context, volName string) ([]iaas.Volume, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	volumes, err := i.Client.ListVolumes(ctx, i.projectID, i.region).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	filterMap := map[string]string{"Name": volName}
	filteredVolumes := FilterVolumes(volumes.Items, filterMap)

	return filteredVolumes, nil
}

func (i iaasClient) GetVolumeByName(ctx context.Context, name string) (*iaas.Volume, error) {
	vols, err := i.GetVolumesByName(ctx, name)
	if err != nil {
		return nil, err
	}

	if len(vols) == 0 {
		return nil, stackiterrors.ErrNotFound
	}

	if len(vols) > 1 {
		return nil, fmt.Errorf("found %d volumes with name %q", len(vols), name)
	}

	return &vols[0], nil
}

func (i iaasClient) ListVolumes(ctx context.Context, _ int, _ string) ([]iaas.Volume, string, error) {
	// TODO: Add support for pagination when IaaS adds it
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	volumes, err := i.Client.ListVolumes(ctx, i.projectID, i.region).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, "", stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, "", err
	}

	return volumes.Items, "", nil
}

func (i iaasClient) ExpandVolume(ctx context.Context, volumeID, volumeStatus string, payload iaas.ResizeVolumePayload) error {
	switch volumeStatus {
	case VolumeAttachedStatus, VolumeAvailableStatus:
		var httpResp *http.Response
		ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

		err := i.Client.
			ResizeVolume(ctx, i.projectID, i.region, volumeID).
			ResizeVolumePayload(payload).
			Execute()
		if err != nil {
			if httpResp != nil {
				reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
				return stackiterrors.WrapErrorWithResponseID(err, reqID)
			}

			return err
		}

		return nil

	default:
		return fmt.Errorf("volume cannot be resized, when status is %s", volumeStatus)
	}
}

func (i iaasClient) WaitVolumeTargetStatus(ctx context.Context, volumeID string, tStatus []string) error {
	backoff := wait.Backoff{
		Duration: operationFinishInitDelay,
		Factor:   operationFinishFactor,
		Steps:    operationFinishSteps,
	}

	waitErr := wait.ExponentialBackoff(backoff, func() (bool, error) {
		vol, err := i.GetVolume(ctx, volumeID)
		if err != nil {
			return false, err
		}
		if slices.Contains(tStatus, *vol.Status) {
			return true, nil
		}
		for _, eState := range volumeErrorStates {
			if *vol.Status == eState {
				return false, fmt.Errorf("volume is in Error State : %s", ptr.Deref(vol.Status, ""))
			}
		}
		return false, nil
	})

	if wait.Interrupted(waitErr) {
		waitErr = fmt.Errorf("timeout on waiting for volume %s status to be in %v", volumeID, tStatus)
	}

	return waitErr
}

func (i iaasClient) WaitDiskAttached(ctx context.Context, instanceID, volumeID string) error {
	backoff := wait.Backoff{
		Duration: diskAttachInitDelay,
		Factor:   diskAttachFactor,
		Steps:    diskAttachSteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		attached, err := i.diskIsAttached(ctx, instanceID, volumeID)
		if err != nil && !stackiterrors.IsNotFound(err) {
			// if this is a race condition indicate the volume is deleted
			// during sleep phase, ignore the error and return attach=false
			return false, err
		}
		return attached, nil
	})

	if wait.Interrupted(err) {
		err = fmt.Errorf("volume %q failed to be attached within the allowed time", volumeID)
	}

	return err
}

func (i iaasClient) WaitDiskDetached(ctx context.Context, instanceID, volumeID string) error {
	backoff := wait.Backoff{
		Duration: diskDetachInitDelay,
		Factor:   diskDetachFactor,
		Steps:    diskDetachSteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		attached, err := i.diskIsAttached(ctx, instanceID, volumeID)
		if err != nil {
			return false, err
		}
		return !attached, nil
	})

	if wait.Interrupted(err) {
		err = fmt.Errorf("volume %q failed to detach within the allowed time", volumeID)
	}

	return err
}

func (i iaasClient) DetachVolume(ctx context.Context, serverID, volumeID string) error {
	volume, err := i.GetVolume(ctx, volumeID)
	if err != nil {
		return err
	}

	if *volume.Status == VolumeAvailableStatus {
		klog.V(2).Infof("Volume: %s has been detached from compute: %s ", *volume.Id, serverID)
		return nil
	}

	if *volume.Status != VolumeAttachedStatus {
		return fmt.Errorf("can not detach volume %s, its status is %s", *volume.Name, *volume.Status)
	}

	if volume.ServerId != nil && *volume.ServerId == serverID {
		var httpResp *http.Response
		ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

		if err := i.Client.RemoveVolumeFromServer(ctx, i.projectID, i.region, serverID, volumeID).Execute(); err != nil {
			if httpResp != nil {
				reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
				return stackiterrors.WrapErrorWithResponseID(
					fmt.Errorf("failed to detach volume %s from compute %s : %w", *volume.Name, serverID, err),
					reqID,
				)
			}

			return err
		}

		klog.V(2).Infof("Successfully detached volume: %s from compute: %s", *volume.Id, serverID)
	}

	return nil
}

func (i iaasClient) WaitVolumeTargetStatusWithCustomBackoff(ctx context.Context, volumeID string, tStatus []string, backoff *wait.Backoff) error {
	waitErr := wait.ExponentialBackoff(*backoff, func() (bool, error) {
		vol, err := i.GetVolume(ctx, volumeID)
		if err != nil {
			return false, err
		}
		if slices.Contains(tStatus, *vol.Status) {
			return true, nil
		}
		for _, eState := range volumeErrorStates {
			if *vol.Status == eState {
				return false, fmt.Errorf("volume is in error state: %s", *vol.Status)
			}
		}
		return false, nil
	})

	if wait.Interrupted(waitErr) {
		waitErr = fmt.Errorf("timeout on waiting for volume %s status to be in %v", volumeID, tStatus)
	}

	return waitErr
}

// diskIsAttached queries if a volume is attached to a compute instance
func (i iaasClient) diskIsAttached(ctx context.Context, instanceID, volumeID string) (bool, error) {
	volume, err := i.GetVolume(ctx, volumeID)
	if err != nil {
		return false, err
	}

	if volume.ServerId != nil && *volume.ServerId == instanceID {
		return true, nil
	}
	return false, nil
}

// diskIsUsed returns true whether a disk is attached to any node
func (i iaasClient) diskIsUsed(ctx context.Context, volumeID string) (bool, error) {
	volume, err := i.GetVolume(ctx, volumeID)
	if err != nil {
		return false, err
	}

	diskUsed := volume.ServerId != nil && *volume.ServerId != ""

	return diskUsed, nil
}
