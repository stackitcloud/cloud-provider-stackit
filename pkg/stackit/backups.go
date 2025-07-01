package stackit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/stackitcloud/stackit-sdk-go/core/runtime"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas/wait"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/util"
	csiError "github.com/stackitcloud/cloud-provider-stackit/pkg/util/errors"
)

const (
	backupReadyStatus                    = "AVAILABLE"
	backupErrorStatus                    = "error"
	backupDescription                    = "Created by STACKIT CSI driver"
	BackupMaxDurationSecondsPerGBDefault = 20
	BackupMaxDurationPerGB               = "backup-max-duration-seconds-per-gb"
	backupBaseDurationSeconds            = 30
	backupReadyCheckIntervalSeconds      = 7
)

func (os *iaasClient) CreateBackup(ctx context.Context, name, volID, snapshotID string, tags map[string]string) (*iaas.Backup, error) {
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
		Name: ptr.To(name),
		Source: &iaas.BackupSource{
			Type: ptr.To(string(backupSource)),
			Id:   ptr.To(backupSourceID),
		},
	}
	if tags != nil {
		opts.Labels = ptr.To(util.ConvertMapStringToInterface(tags))
	}
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	backup, err := os.iaas.CreateBackup(ctxWithHTTPResp, os.projectID).CreateBackupPayload(opts).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(wait.XRequestIDHeader)
			return nil, csiError.WrapErrorWithResponseID(err, reqID)
		}
		return nil, err
	}

	return backup, nil
}

func (os *iaasClient) ListBackups(ctx context.Context, filters map[string]string) ([]iaas.Backup, error) {
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	// TODO: Add API filter once available.
	backups, err := os.iaas.ListBackups(ctxWithHTTPResp, os.projectID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(wait.XRequestIDHeader)
			return nil, csiError.WrapErrorWithResponseID(err, reqID)
		}
		return nil, err
	}

	filteredBackups := filterBackups(*backups.Items, filters)
	return filteredBackups, nil
}

func (os *iaasClient) DeleteBackup(ctx context.Context, backupID string) error {
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	err := os.iaas.DeleteBackup(ctxWithHTTPResp, os.projectID, backupID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(wait.XRequestIDHeader)
			return csiError.WrapErrorWithResponseID(err, reqID)
		}
		return err
	}
	return nil
}

func (os *iaasClient) GetBackupByID(ctx context.Context, backupID string) (*iaas.Backup, error) {
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	backup, err := os.iaas.GetBackupExecute(ctxWithHTTPResp, os.projectID, backupID)
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(wait.XRequestIDHeader)
			return nil, csiError.WrapErrorWithResponseID(err, reqID)
		}
		return nil, err
	}
	return backup, nil
}

func (os *iaasClient) WaitBackupReady(ctx context.Context, backupID string, snapshotSize int64, backupMaxDurationSecondsPerGB int) (*string, error) {
	var err error

	duration := time.Duration(int64(backupMaxDurationSecondsPerGB)*snapshotSize + backupBaseDurationSeconds)
	err = os.waitBackupReadyWithContext(backupID, duration)
	if errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("timeout, Backup %s is still not Ready: %v", backupID, err)
	}

	back, _ := os.GetBackupByID(ctx, backupID)

	if back != nil {
		return back.Status, err
	}
	return ptr.To("Failed to get backup status"), err
}

func (os *iaasClient) waitBackupReadyWithContext(backupID string, duration time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), duration*time.Second)
	defer cancel()
	var done bool
	var err error
	ticker := time.NewTicker(backupReadyCheckIntervalSeconds * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			done, err = os.backupIsReady(ctx, backupID)
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
func (os *iaasClient) backupIsReady(ctx context.Context, backupID string) (bool, error) {
	backup, err := os.GetBackupByID(ctx, backupID)
	if err != nil {
		return false, err
	}

	if *backup.Status == backupErrorStatus {
		return false, errors.New("backup is in error state")
	}

	return *backup.Status == backupReadyStatus, nil
}
