package stackit

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/stackiterrors"
	"github.com/stackitcloud/stackit-sdk-go/core/runtime"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	sdkWait "github.com/stackitcloud/stackit-sdk-go/services/iaas/wait"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

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

var volumeErrorStates = [...]string{"ERROR", "ERROR_RESIZING", "ERROR_DELETING"}

func (os *iaasClient) CreateVolume(ctx context.Context, payload *iaas.CreateVolumePayload) (*iaas.Volume, error) {
	payload.Description = ptr.To(VolumeDescription)
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	req, err := os.iaas.CreateVolume(ctxWithHTTPResp, os.projectID, os.region).CreateVolumePayload(*payload).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return nil, err
	}

	return req, nil
}

func (os *iaasClient) DeleteVolume(ctx context.Context, volumeID string) error {
	used, err := os.diskIsUsed(ctx, volumeID)
	if err != nil {
		return err
	}
	if used {
		return fmt.Errorf("cannot delete the volume %q, it's still attached to a node", volumeID)
	}

	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	err = os.iaas.DeleteVolume(ctxWithHTTPResp, os.projectID, os.region, volumeID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return err
	}

	return err
}

func (os *iaasClient) AttachVolume(ctx context.Context, instanceID, volumeID string) (string, error) {
	volume, err := os.GetVolume(ctx, volumeID)
	if err != nil {
		return "", err
	}

	if volume.ServerId != nil && instanceID == *volume.ServerId {
		klog.V(4).Infof("Disk %s is already attached to instance %s", volumeID, instanceID)
		return *volume.Id, nil
	}
	payload := iaas.AddVolumeToServerPayload{
		DeleteOnTermination: ptr.To(false),
	}
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	_, err = os.iaas.AddVolumeToServer(ctxWithHTTPResp, os.projectID, os.region, instanceID, volumeID).AddVolumeToServerPayload(payload).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return "", stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return "", err
	}
	return *volume.Id, err
}

// WaitVolumeTargetStatusWithCustomBackoff waits for volume to be in target state with custom backoff
func (os *iaasClient) WaitVolumeTargetStatusWithCustomBackoff(ctx context.Context, volumeID string, tStatus []string, backoff *wait.Backoff) error {
	waitErr := wait.ExponentialBackoff(*backoff, func() (bool, error) {
		vol, err := os.GetVolume(ctx, volumeID)
		if err != nil {
			return false, err
		}
		for _, t := range tStatus {
			if *vol.Status == t {
				return true, nil
			}
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

func (os *iaasClient) ListVolumes(ctx context.Context, _ int, _ string) ([]iaas.Volume, string, error) {
	// TODO: Add support for pagination when IaaS adds it
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	volumes, err := os.iaas.ListVolumes(ctxWithHTTPResp, os.projectID, os.region).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, "", stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return nil, "", err
	}

	return *volumes.Items, "", err
}

func (os *iaasClient) WaitDiskAttached(ctx context.Context, instanceID, volumeID string) error {
	backoff := wait.Backoff{
		Duration: diskAttachInitDelay,
		Factor:   diskAttachFactor,
		Steps:    diskAttachSteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		attached, err := os.diskIsAttached(ctx, instanceID, volumeID)
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

func (os *iaasClient) DetachVolume(ctx context.Context, instanceID, volumeID string) error {
	volume, err := os.GetVolume(ctx, volumeID)
	if err != nil {
		return err
	}
	if *volume.Status == VolumeAvailableStatus {
		klog.V(2).Infof("volume: %s has been detached from compute: %s ", *volume.Id, instanceID)
		return nil
	}

	if *volume.Status != VolumeAttachedStatus {
		return fmt.Errorf("can not detach volume %s, its status is %s", *volume.Name, *volume.Status)
	}
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	if volume.ServerId != nil && *volume.ServerId == instanceID {
		err = os.iaas.RemoveVolumeFromServer(ctxWithHTTPResp, os.projectID, os.region, instanceID, volumeID).Execute()
		if err != nil {
			if httpResp != nil {
				reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
				return stackiterrors.WrapErrorWithResponseID(fmt.Errorf("failed to detach volume %s from compute %s : %v", *volume.Id, instanceID, err), reqID)
			}
			return err
		}
		klog.V(2).Infof("Successfully detached volume: %s from compute: %s", *volume.Id, instanceID)
		return nil
	}

	// Disk has no attachments or not attached to provided compute
	return nil
}

func (os *iaasClient) WaitDiskDetached(ctx context.Context, instanceID, volumeID string) error {
	backoff := wait.Backoff{
		Duration: diskDetachInitDelay,
		Factor:   diskDetachFactor,
		Steps:    diskDetachSteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		attached, err := os.diskIsAttached(ctx, instanceID, volumeID)
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

// diskIsUsed returns true whether a disk is attached to any node
func (os *iaasClient) diskIsUsed(ctx context.Context, volumeID string) (bool, error) {
	volume, err := os.GetVolume(ctx, volumeID)
	if err != nil {
		return false, err
	}

	diskUsed := volume.ServerId != nil && *volume.ServerId != ""

	return diskUsed, nil
}

// diskIsAttached queries if a volume is attached to a compute instance
func (os *iaasClient) diskIsAttached(ctx context.Context, instanceID, volumeID string) (bool, error) {
	volume, err := os.GetVolume(ctx, volumeID)
	if err != nil {
		return false, err
	}

	if volume.ServerId != nil && *volume.ServerId == instanceID {
		return true, nil
	}
	return false, nil
}

func (os *iaasClient) GetVolume(ctx context.Context, volumeID string) (*iaas.Volume, error) {
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	vol, err := os.iaas.GetVolume(ctxWithHTTPResp, os.projectID, os.region, volumeID).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return nil, err
	}
	return vol, nil
}

func (os *iaasClient) GetVolumesByName(ctx context.Context, volName string) ([]iaas.Volume, error) {
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)
	// TODO: Add API filter once available.
	volumes, err := os.iaas.ListVolumes(ctxWithHTTPResp, os.projectID, os.region).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}
		return nil, err
	}

	filterMap := map[string]string{"Name": volName}
	filteredVolumes := filterVolumes(*volumes.Items, filterMap)

	return filteredVolumes, nil
}

func (os *iaasClient) GetVolumeByName(ctx context.Context, name string) (*iaas.Volume, error) {
	vols, err := os.GetVolumesByName(ctx, name)
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

func (os *iaasClient) WaitVolumeTargetStatus(ctx context.Context, volumeID string, tStatus []string) error {
	backoff := wait.Backoff{
		Duration: operationFinishInitDelay,
		Factor:   operationFinishFactor,
		Steps:    operationFinishSteps,
	}

	waitErr := wait.ExponentialBackoff(backoff, func() (bool, error) {
		vol, err := os.GetVolume(ctx, volumeID)
		if err != nil {
			return false, err
		}
		for _, t := range tStatus {
			if *vol.Status == t {
				return true, nil
			}
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

func (os *iaasClient) ExpandVolume(ctx context.Context, volumeID, volumeStatus string, newSize int64) error {
	extendOpts := iaas.ResizeVolumePayload{Size: ptr.To(newSize)}
	var httpResp *http.Response
	ctxWithHTTPResp := runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	switch volumeStatus {
	case VolumeAttachedStatus, VolumeAvailableStatus:
		resizeErr := os.iaas.ResizeVolume(ctxWithHTTPResp, os.projectID, os.region, volumeID).ResizeVolumePayload(extendOpts).Execute()
		if resizeErr != nil {
			if httpResp != nil {
				reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
				return stackiterrors.WrapErrorWithResponseID(resizeErr, reqID)
			}
			return resizeErr
		}
		return nil
	default:
		return fmt.Errorf("volume cannot be resized, when status is %s", volumeStatus)
	}
}
