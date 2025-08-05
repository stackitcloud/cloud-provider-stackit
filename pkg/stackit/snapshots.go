package stackit

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/stackitcloud/stackit-sdk-go/core/runtime"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	sdkWait "github.com/stackitcloud/stackit-sdk-go/services/iaas/wait"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"

	stackiterrors "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/errors"
)

const (
	SnapshotReadyStatus = "AVAILABLE"
	snapReadyDuration   = 1 * time.Second
	snapReadyFactor     = 1.2
	snapReadySteps      = 10

	snapshotDescription      = "Created by STACKIT CSI driver"
	SnapshotType             = "type"
	SnapshotAvailabilityZone = "availability"
)

func (os *iaasClient) CreateSnapshot(ctx context.Context, name, volID string, tags map[string]string) (*iaas.Snapshot, error) {
	opts := iaas.CreateSnapshotPayload{
		VolumeId: ptr.To(volID),
		Name:     ptr.To(name),
	}
	if tags != nil {
		opts.Labels = ptr.To(map[string]interface{}(labelsFromTags(tags)))
	}
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	snapshot, err := os.iaas.CreateSnapshot(ctxWithHTTPResp, os.projectID).CreateSnapshotPayload(opts).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return nil, err
	}

	return snapshot, nil
}

func (os *iaasClient) ListSnapshots(ctx context.Context, filters map[string]string) ([]iaas.Snapshot, string, error) {
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	// TODO: Add API filter once available.
	snaps, err := os.iaas.ListSnapshots(ctxWithHTTPResp, os.projectID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, "", stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return nil, "", err
	}

	filteredSnaps := filterSnapshots(*snaps.Items, filters)
	return filteredSnaps, "", nil
}

func (os *iaasClient) DeleteSnapshot(ctx context.Context, snapID string) error {
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	err := os.iaas.DeleteSnapshotExecute(ctxWithHTTPResp, os.projectID, snapID)
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return err
	}
	return nil
}

func (os *iaasClient) GetSnapshotByID(ctx context.Context, snapshotID string) (*iaas.Snapshot, error) {
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	snap, err := os.iaas.GetSnapshotExecute(ctxWithHTTPResp, os.projectID, snapshotID)
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return nil, err
	}
	return snap, nil
}

func (os *iaasClient) WaitSnapshotReady(ctx context.Context, snapshotID string) (*string, error) {
	backoff := wait.Backoff{
		Duration: snapReadyDuration,
		Factor:   snapReadyFactor,
		Steps:    snapReadySteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		ready, err := os.snapshotIsReady(ctx, snapshotID)
		if err != nil {
			return false, err
		}
		return ready, nil
	})

	if wait.Interrupted(err) {
		err = fmt.Errorf("timeout, Snapshot %s is still not Ready %v", snapshotID, err)
	}

	snap, _ := os.GetSnapshotByID(ctx, snapshotID)

	if snap != nil {
		return snap.Status, err
	}
	return ptr.To("Failed to get snapshot status"), err
}

func (os *iaasClient) snapshotIsReady(ctx context.Context, snapshotID string) (bool, error) {
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	snap, err := os.iaas.GetSnapshotExecute(ctxWithHTTPResp, os.projectID, snapshotID)
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return false, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return false, err
	}

	return *snap.Status == SnapshotReadyStatus, nil
}
