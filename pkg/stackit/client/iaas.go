package client

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/stackiterrors"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

type iaasClient struct {
	Client    iaas.DefaultAPI
	projectID string
	orgID     string
	areaID    string
	region    string
}

type IaaSClient interface {
	GetServer(ctx context.Context, serverID string) (*iaas.Server, error)
	GetServerWithDetails(ctx context.Context, serverID string) (*iaas.Server, error)
	ListServers(ctx context.Context) (*[]iaas.Server, error)

	CreateSnapshot(ctx context.Context, payload iaas.CreateSnapshotPayload) (*iaas.Snapshot, error)
	ListSnapshots(ctx context.Context, filters map[string]string) ([]iaas.Snapshot, string, error)
	DeleteSnapshot(ctx context.Context, snapshotID string) error
	GetSnapshot(ctx context.Context, snapshotID string) (*iaas.Snapshot, error)
	WaitSnapshotReady(ctx context.Context, snapshotID string) (*string, error)

	CreateBackup(ctx context.Context, name, volID, snapshotID string, tags map[string]string) (*iaas.Backup, error)
	ListBackups(ctx context.Context, filters map[string]string) ([]iaas.Backup, error)
	DeleteBackup(ctx context.Context, backupID string) error
	GetBackup(ctx context.Context, backupID string) (*iaas.Backup, error)
	WaitBackupReady(ctx context.Context, backupID string, snapshotSize int64, backupMaxDurationSecondsPerGB int) (*string, error)

	CreateVolume(ctx context.Context, payload iaas.CreateVolumePayload) (*iaas.Volume, error)
	DeleteVolume(ctx context.Context, volumeID string) error
	AttachVolume(ctx context.Context, serverID, volumeID string, payload iaas.AddVolumeToServerPayload) (string, error)
	DetachVolume(ctx context.Context, serverID, volumeID string) error
	GetVolume(ctx context.Context, volumeID string) (*iaas.Volume, error)
	GetVolumesByName(ctx context.Context, volName string) ([]iaas.Volume, error)
	ListVolumes(ctx context.Context, _ int, _ string) ([]iaas.Volume, string, error)
	ExpandVolume(ctx context.Context, volumeID, volumeStatus string, payload iaas.ResizeVolumePayload) error
	WaitVolumeTargetStatus(ctx context.Context, volumeID string, tStatus []string) error
	WaitDiskAttached(ctx context.Context, instanceID, volumeID string) error
	WaitDiskDetached(ctx context.Context, instanceID, volumeID string) error
	WaitVolumeTargetStatusWithCustomBackoff(ctx context.Context, volumeID string, tStatus []string, backoff *wait.Backoff) error

	ListRoutes(ctx context.Context, routingTableID string, labels map[string]string) ([]iaas.Route, error)
	AddRoutes(ctx context.Context, routingTableID string, routes []iaas.Route) error
	GetRoutingTable(ctx context.Context, routingTableID string) (*iaas.RoutingTable, error)
	DeleteRoute(ctx context.Context, routingTableID string, routeID string) error
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

func NewIaaSClient(region, projectID, orgID, areaID string, options []sdkconfig.ConfigurationOption) (IaaSClient, error) {
	apiClient, err := iaas.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}
	return &iaasClient{
		Client:    apiClient.DefaultAPI,
		projectID: projectID,
		orgID:     orgID,
		areaID:    areaID,
		region:    region,
	}, nil
}

func (i *iaasClient) GetServer(ctx context.Context, serverID string) (*iaas.Server, error) {
	return withResponseID(ctx, func(ctx context.Context) (*iaas.Server, error) {
		return i.Client.GetServer(ctx, i.projectID, i.region, serverID).Execute()
	})
}

func (i *iaasClient) GetServerWithDetails(ctx context.Context, serverID string) (*iaas.Server, error) {
	return withResponseID(ctx, func(ctx context.Context) (*iaas.Server, error) {
		return i.Client.GetServer(ctx, i.projectID, i.region, serverID).Details(true).Execute()
	})
}

func (i *iaasClient) ListServers(ctx context.Context) (*[]iaas.Server, error) {
	return withResponseID(ctx, func(ctx context.Context) (*[]iaas.Server, error) {
		resp, err := i.Client.ListServers(ctx, i.projectID, i.region).Details(true).Execute()
		if err != nil {
			return nil, err
		}

		return &resp.Items, nil
	})
}

//nolint:gocritic // Payload is passed by value to match the shared IaaSClient interface.
func (i *iaasClient) CreateSnapshot(ctx context.Context, payload iaas.CreateSnapshotPayload) (*iaas.Snapshot, error) {
	return withResponseID(ctx, func(ctx context.Context) (*iaas.Snapshot, error) {
		return i.Client.
			CreateSnapshot(ctx, i.projectID, i.region).
			CreateSnapshotPayload(payload).
			Execute()
	})
}

func (i *iaasClient) ListSnapshots(ctx context.Context, filters map[string]string) ([]iaas.Snapshot, string, error) {
	resp, err := withResponseID(ctx, func(ctx context.Context) (*iaas.SnapshotListResponse, error) {
		return i.Client.ListSnapshotsInProject(ctx, i.projectID, i.region).Execute()
	})
	if err != nil {
		return nil, "", err
	}

	filteredSnapshots := FilterSnapshots(resp.Items, filters)

	return filteredSnapshots, "", nil
}

func (i *iaasClient) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	_, err := withResponseID(ctx, func(ctx context.Context) (any, error) {
		return nil, i.Client.DeleteSnapshot(ctx, i.projectID, i.region, snapshotID).Execute()
	})
	return err
}

func (i *iaasClient) GetSnapshot(ctx context.Context, snapshotID string) (*iaas.Snapshot, error) {
	return withResponseID(ctx, func(ctx context.Context) (*iaas.Snapshot, error) {
		return i.Client.GetSnapshot(ctx, i.projectID, i.region, snapshotID).Execute()
	})
}

func (i *iaasClient) WaitSnapshotReady(ctx context.Context, snapshotID string) (*string, error) {
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

func (i *iaasClient) snapshotIsReady(ctx context.Context, snapshotID string) (bool, error) {
	snapshot, err := withResponseID(ctx, func(ctx context.Context) (*iaas.Snapshot, error) {
		return i.Client.GetSnapshot(ctx, i.projectID, i.region, snapshotID).Execute()
	})
	if err != nil {
		return false, err
	}

	return *snapshot.Status == SnapshotReadyStatus, nil
}

func (i *iaasClient) CreateBackup(ctx context.Context, name, volID, snapshotID string, tags map[string]string) (*iaas.Backup, error) {
	payload, err := BuildCreateBackupPayload(name, volID, snapshotID, tags)
	if err != nil {
		return nil, err
	}

	return withResponseID(ctx, func(ctx context.Context) (*iaas.Backup, error) {
		return i.Client.
			CreateBackup(ctx, i.projectID, i.region).
			CreateBackupPayload(payload).
			Execute()
	})
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

func (i *iaasClient) ListBackups(ctx context.Context, filters map[string]string) ([]iaas.Backup, error) {
	resp, err := withResponseID(ctx, func(ctx context.Context) (*iaas.BackupListResponse, error) {
		return i.Client.ListBackups(ctx, i.projectID, i.region).Execute()
	})
	if err != nil {
		return nil, err
	}

	filteredBackups := FilterBackups(resp.Items, filters)

	return filteredBackups, nil
}

func (i *iaasClient) DeleteBackup(ctx context.Context, backupID string) error {
	_, err := withResponseID(ctx, func(ctx context.Context) (any, error) {
		return nil, i.Client.DeleteBackup(ctx, i.projectID, i.region, backupID).Execute()
	})
	return err
}

func (i *iaasClient) GetBackup(ctx context.Context, backupID string) (*iaas.Backup, error) {
	return withResponseID(ctx, func(ctx context.Context) (*iaas.Backup, error) {
		return i.Client.GetBackup(ctx, i.projectID, i.region, backupID).Execute()
	})
}

func (i *iaasClient) WaitBackupReady(ctx context.Context, backupID string, snapshotSize int64, backupMaxDurationSecondsPerGB int) (*string, error) {
	duration := time.Duration(int64(backupMaxDurationSecondsPerGB)*snapshotSize + backupBaseDurationSeconds)
	err := i.waitBackupReadyWithContext(backupID, duration)
	if errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("timeout, Backup %s is still not Ready: %w", backupID, err)
	}

	backup, _ := i.GetBackup(ctx, backupID)
	if backup != nil {
		return backup.Status, err
	}

	return new("Failed to get backup status"), err
}

func (i *iaasClient) waitBackupReadyWithContext(backupID string, duration time.Duration) error {
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
func (i *iaasClient) backupIsReady(ctx context.Context, backupID string) (bool, error) {
	backup, err := i.GetBackup(ctx, backupID)
	if err != nil {
		return false, err
	}

	if *backup.Status == backupErrorStatus {
		return false, errors.New("backup is in error state")
	}

	return *backup.Status == backupReadyStatus, nil
}

//nolint:gocritic // Payload is passed by value to match the shared IaaSClient interface.
func (i *iaasClient) CreateVolume(ctx context.Context, payload iaas.CreateVolumePayload) (*iaas.Volume, error) {
	payload.Description = new(VolumeDescription)

	return withResponseID(ctx, func(ctx context.Context) (*iaas.Volume, error) {
		return i.Client.CreateVolume(ctx, i.projectID, i.region).CreateVolumePayload(payload).Execute()
	})
}

func (i *iaasClient) DeleteVolume(ctx context.Context, volumeID string) error {
	used, err := i.diskIsUsed(ctx, volumeID)
	if err != nil {
		return err
	}
	if used {
		return fmt.Errorf("cannot delete the volume %q, it's still attached to a node", volumeID)
	}

	_, err = withResponseID(ctx, func(ctx context.Context) (any, error) {
		return nil, i.Client.DeleteVolume(ctx, i.projectID, i.region, volumeID).Execute()
	})
	return err
}

func (i *iaasClient) AttachVolume(ctx context.Context, serverID, volumeID string, payload iaas.AddVolumeToServerPayload) (string, error) {
	volume, err := i.GetVolume(ctx, volumeID)
	if err != nil {
		return "", err
	}

	if volume.ServerId != nil && serverID == *volume.ServerId {
		klog.V(4).Infof("Disk %s is already attached to instance %s", volumeID, serverID)
		return *volume.Id, nil
	}

	_, err = withResponseID(ctx, func(ctx context.Context) (any, error) {
		return i.Client.
			AddVolumeToServer(ctx, i.projectID, i.region, serverID, volumeID).
			AddVolumeToServerPayload(payload).
			Execute()
	})
	if err != nil {
		return "", err
	}

	return volume.GetId(), nil
}

func (i *iaasClient) GetVolume(ctx context.Context, volumeID string) (*iaas.Volume, error) {
	return withResponseID(ctx, func(ctx context.Context) (*iaas.Volume, error) {
		return i.Client.GetVolume(ctx, i.projectID, i.region, volumeID).Execute()
	})
}

func (i *iaasClient) GetVolumesByName(ctx context.Context, volName string) ([]iaas.Volume, error) {
	resp, err := withResponseID(ctx, func(ctx context.Context) (*iaas.VolumeListResponse, error) {
		return i.Client.ListVolumes(ctx, i.projectID, i.region).Execute()
	})
	if err != nil {
		return nil, err
	}

	filterMap := map[string]string{"Name": volName}
	filteredVolumes := FilterVolumes(resp.Items, filterMap)

	return filteredVolumes, nil
}

func (i *iaasClient) ListVolumes(ctx context.Context, _ int, _ string) ([]iaas.Volume, string, error) {
	// TODO: Add support for pagination when IaaS adds it
	resp, err := withResponseID(ctx, func(ctx context.Context) (*iaas.VolumeListResponse, error) {
		return i.Client.ListVolumes(ctx, i.projectID, i.region).Execute()
	})
	if err != nil {
		return nil, "", err
	}

	return resp.Items, "", nil
}

func (i *iaasClient) ExpandVolume(ctx context.Context, volumeID, volumeStatus string, payload iaas.ResizeVolumePayload) error {
	switch volumeStatus {
	case VolumeAttachedStatus, VolumeAvailableStatus:
		_, err := withResponseID(ctx, func(ctx context.Context) (any, error) {
			return nil, i.Client.
				ResizeVolume(ctx, i.projectID, i.region, volumeID).
				ResizeVolumePayload(payload).
				Execute()
		})
		return err

	default:
		return fmt.Errorf("volume cannot be resized, when status is %s", volumeStatus)
	}
}

func (i *iaasClient) WaitVolumeTargetStatus(ctx context.Context, volumeID string, tStatus []string) error {
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

func (i *iaasClient) WaitDiskAttached(ctx context.Context, instanceID, volumeID string) error {
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

func (i *iaasClient) WaitDiskDetached(ctx context.Context, instanceID, volumeID string) error {
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

func (i *iaasClient) DetachVolume(ctx context.Context, serverID, volumeID string) error {
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
		_, err := withResponseID(ctx, func(ctx context.Context) (any, error) {
			err := i.Client.RemoveVolumeFromServer(ctx, i.projectID, i.region, serverID, volumeID).Execute()
			if err != nil {
				return nil, fmt.Errorf("failed to detach volume %s from compute %s : %w", *volume.Name, serverID, err)
			}
			return nil, nil
		})
		if err != nil {
			return err
		}

		klog.V(2).Infof("Successfully detached volume: %s from compute: %s", *volume.Id, serverID)
	}

	return nil
}

func (i *iaasClient) WaitVolumeTargetStatusWithCustomBackoff(ctx context.Context, volumeID string, tStatus []string, backoff *wait.Backoff) error {
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
func (i *iaasClient) diskIsAttached(ctx context.Context, instanceID, volumeID string) (bool, error) {
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
func (i *iaasClient) diskIsUsed(ctx context.Context, volumeID string) (bool, error) {
	volume, err := i.GetVolume(ctx, volumeID)
	if err != nil {
		return false, err
	}

	diskUsed := volume.ServerId != nil && *volume.ServerId != ""

	return diskUsed, nil
}

func (i *iaasClient) ListRoutes(ctx context.Context, routingTableID string, labels map[string]string) ([]iaas.Route, error) {
	return withResponseID(ctx, func(ctx context.Context) ([]iaas.Route, error) {
		resp, err := i.Client.ListRoutesOfRoutingTable(ctx, i.orgID, i.areaID, i.region, routingTableID).
			LabelSelector(LabelSelector(labels)).
			Execute()
		if err != nil {
			return nil, err
		}
		return resp.GetItems(), nil
	})
}

func (i *iaasClient) AddRoutes(ctx context.Context, routingTableID string, routes []iaas.Route) error {
	payload := iaas.NewAddRoutesToRoutingTablePayload(routes)
	_, err := withResponseID(ctx, func(ctx context.Context) (any, error) {
		_, err := i.Client.AddRoutesToRoutingTable(ctx, i.orgID, i.areaID, i.region, routingTableID).
			AddRoutesToRoutingTablePayload(*payload).
			Execute()
		return nil, err
	})
	return err
}

func (i *iaasClient) GetRoutingTable(ctx context.Context, routingTableID string) (*iaas.RoutingTable, error) {
	return withResponseID(ctx, func(ctx context.Context) (*iaas.RoutingTable, error) {
		return i.Client.GetRoutingTableOfArea(ctx, i.orgID, i.areaID, i.region, routingTableID).Execute()
	})
}

func (i *iaasClient) DeleteRoute(ctx context.Context, routingTableID, routeID string) error {
	_, err := withResponseID(ctx, func(ctx context.Context) (any, error) {
		return nil, i.Client.DeleteRouteFromRoutingTable(ctx, i.orgID, i.areaID, i.region, routingTableID, routeID).Execute()
	})
	return err
}
