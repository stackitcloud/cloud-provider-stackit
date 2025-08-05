/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package blockstorage

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	sharedcsi "github.com/stackitcloud/cloud-provider-stackit/pkg/csi"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	stackiterrors "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/errors"
)

type controllerServer struct {
	Driver   *Driver
	Instance stackit.IaasClient
	csi.UnimplementedControllerServer
}

const (
	blockStorageCSIClusterIDKey = "block-storage.csi.stackit.cloud/cluster" //nolint:unused // for later use
)

func (cs *controllerServer) validateVolumeCapabilities(req []*csi.VolumeCapability) error {
	for _, volCap := range req {
		if volCap.GetAccessMode().GetMode() != cs.Driver.vcap[0].GetMode() {
			return fmt.Errorf("volume access mode %s not supported", volCap.GetAccessMode().GetMode().String())
		}
	}
	return nil
}

//nolint:gocyclo,funlen // This function is complex and should be broken down further, but it's ok for now.
func (cs *controllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(4).Infof("CreateVolume: called with args %+v", protosanitizer.StripSecrets(req))

	cloud := cs.Instance

	// Volume Name
	volName := req.GetName()
	volCapabilities := req.GetVolumeCapabilities()
	volParams := req.GetParameters()

	if volName == "" {
		return nil, status.Error(codes.InvalidArgument, "[CreateVolume] missing Volume Name")
	}

	if volCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "[CreateVolume] missing Volume capability")
	}

	if err := cs.validateVolumeCapabilities(volCapabilities); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Volume Size - Default is 1 GiB
	volSizeBytes := 1 * GIBIBYTE
	if req.GetCapacityRange() != nil {
		volSizeBytes = req.GetCapacityRange().GetRequiredBytes()
	}
	volSizeGB := roundUpSize(volSizeBytes, GIBIBYTE)

	// Volume Type
	volType := volParams["type"]

	var volAvailability string
	if cs.Driver.withTopology {
		// First check if volAvailability is already specified, if not get preferred from Topology
		// Required, incase vol AZ is different from node AZ
		volAvailability = volParams["availability"]
		if volAvailability == "" {
			accessibleTopologyReq := req.GetAccessibilityRequirements()
			// Check from Topology
			if accessibleTopologyReq != nil {
				volAvailability = sharedcsi.GetAZFromTopology(topologyKey, accessibleTopologyReq)
			}
		}
	}

	// get the PVC annotation
	pvcAnnotations := sharedcsi.GetPVCAnnotations(cs.Driver.pvcLister, volParams)
	for k, v := range pvcAnnotations {
		klog.V(4).Infof("CreateVolume: retrieved %q pvc annotation: %s: %s", k, v, volName)
	}

	// Verify a volume with the provided name doesn't already exist for this tenant
	vols, err := cloud.GetVolumesByName(ctx, volName)
	if err != nil {
		klog.Errorf("Failed to query for existing Volume during CreateVolume: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get volumes: %v", err)
	}

	if len(vols) == 1 {
		if volSizeGB != *vols[0].Size {
			return nil, status.Error(codes.AlreadyExists, "Volume Already exists with same name and different capacity")
		}
		if *vols[0].Status != stackit.VolumeAvailableStatus {
			return nil, status.Error(codes.Internal, fmt.Sprintf("Volume %s is not in available state", *vols[0].Id))
		}
		klog.V(4).Infof("Volume %s already exists in Availability Zone: %s of size %d GiB", *vols[0].Id, *vols[0].AvailabilityZone, *vols[0].Size)
		return getCreateVolumeResponse(&vols[0]), nil
	} else if len(vols) > 1 {
		klog.V(3).Infof("found multiple existing volumes with selected name (%s) during create", volName)
		return nil, status.Error(codes.Internal, "Multiple volumes reported by Cinder with same name")
	}

	// Volume Create
	// TODO: Use once IaaS has extended the label regex to allow for forward slashes and dots
	// properties := map[string]string{blockStorageCSIClusterIDKey: cs.Driver.clusterID}
	properties := map[string]string{}
	// Tag volume with metadata if present: https://github.com/kubernetes-csi/external-provisioner/pull/399
	for _, mKey := range sharedcsi.RecognizedCSIProvisionerParams {
		if v, ok := req.Parameters[mKey]; ok {
			properties[mKey] = v
		}
	}
	content := req.GetVolumeContentSource()
	var sourceVolID string
	var sourceBackupID string
	var sourceSnapshotID string
	var volumeSourceType stackit.VolumeSourceTypes

	if content != nil && content.GetSnapshot() != nil {
		// Backups and Snapshots are the same for Kubernetes
		sourceSnapshotID = content.GetSnapshot().GetSnapshotId()
		sourceBackupID = content.GetSnapshot().GetSnapshotId()
		// By default, we try to clone volumes from snapshots
		volumeSourceType = stackit.SnapshotSource

		snap, err := cloud.GetSnapshotByID(ctx, sourceSnapshotID)
		if stackiterrors.IgnoreNotFound(err) != nil {
			return nil, status.Errorf(codes.Internal, "Failed to retrieve the source snapshot %s: %v", sourceSnapshotID, err)
		}
		// If the snapshot exists but is not yet available, fail.
		if err == nil && *snap.Status != stackit.SnapshotReadyStatus {
			return nil, status.Errorf(codes.Unavailable, "VolumeContentSource Snapshot %s is not yet available. status: %s", sourceSnapshotID, *snap.Status)
		}
		// Only continue checking if the Snapshot is found
		if !stackiterrors.IsNotFound(err) {
			// TODO: Remove cloud.GetVolume() once IaaS adds the AZ field in the response of GetSnapshotByID()
			snapshotVolSrc, err := cloud.GetVolume(ctx, snap.GetVolumeId())
			if err != nil {
				return nil, status.Errorf(codes.Internal, "Failed to retrieve the source volume of snapshot %s: %v", sourceSnapshotID, err)
			}
			if *snapshotVolSrc.AvailabilityZone != volAvailability {
				return nil, status.Errorf(codes.ResourceExhausted, "Volume must be in the same availability zone as source Snapshot. Got %s Required: %s", volAvailability, *snapshotVolSrc.AvailabilityZone)
			}
		}

		// In case a snapshot is not found
		// check if a Backup with the same ID exists
		if stackiterrors.IsNotFound(err) {
			var back *iaas.Backup
			back, err = cloud.GetBackupByID(ctx, sourceBackupID)
			if err != nil {
				// If there is an error getting the backup as well, fail.
				return nil, status.Errorf(codes.NotFound, "VolumeContentSource Snapshot or Backup with ID %s not found", sourceBackupID)
			}
			if *back.Status != stackit.SnapshotReadyStatus {
				// If the backup exists but is not yet available, fail.
				return nil, status.Errorf(codes.Unavailable, "VolumeContentSource Backup %s is not yet available. status: %s", sourceBackupID, *back.Status)
			}
			// If an available backup is found, create the volume from the backup. Implies that a Snapshot was not found.
			volumeSourceType = stackit.BackupSource
		}
	}

	if content != nil && content.GetVolume() != nil {
		sourceVolID = content.GetVolume().GetVolumeId()
		sourceVolume, err := cloud.GetVolume(ctx, sourceVolID)
		if err != nil {
			if stackiterrors.IsNotFound(err) {
				return nil, status.Errorf(codes.NotFound, "Source Volume %s not found", sourceVolID)
			}
			return nil, status.Errorf(codes.Internal, "Failed to retrieve the source volume %s: %v", sourceVolID, err)
		}
		if volAvailability != *sourceVolume.AvailabilityZone {
			return nil, status.Errorf(codes.ResourceExhausted, "Volume must be in the same availability zone as source Volume. Got %s Required: %s", volAvailability, *sourceVolume.AvailabilityZone)
		}
		volumeSourceType = stackit.VolumeSource
	}

	opts := &iaas.CreateVolumePayload{
		Name:             ptr.To(volName),
		PerformanceClass: ptr.To(volType),
		Size:             ptr.To(volSizeGB),
		AvailabilityZone: ptr.To(volAvailability),
		// TODO: IaaS API is crap and does not allow dots or slashes. Waiting for IaaS to update regex.
		// Labels:           ptr.To(util.ConvertMapStringToInterface(properties)),
	}

	// Only set CreateVolumePayload.Source when actually creating volume from source/snapshot/backup
	if volumeSourceType != "" {
		// Again sourceSnapshotID == sourceBackupID
		volumeSourceID := determineSourceIDForSourceType(volumeSourceType, sourceSnapshotID, sourceVolID)
		klog.V(4).Infof("Creating volume from %s source", volumeSourceType)
		opts.Source = &iaas.VolumeSource{
			Id:   ptr.To(volumeSourceID),
			Type: ptr.To(string(volumeSourceType)),
		}
	}

	vol, err := cloud.CreateVolume(ctx, opts)
	if err != nil {
		klog.Errorf("Failed to CreateVolume: %v", err)
		return nil, status.Errorf(codes.Internal, "CreateVolume failed with error %v", err)
	}

	targetStatus := []string{stackit.VolumeAvailableStatus}
	// Recheck after: 0s (immediate), 20s, 45.6s, 78.36s, 120.31s
	err = cloud.WaitVolumeTargetStatusWithCustomBackoff(ctx, *vol.Id, targetStatus,
		&wait.Backoff{
			Duration: 20 * time.Second,
			Steps:    5,
			Factor:   1.28,
		})
	if err != nil {
		klog.Errorf("Failed to WaitVolumeTargetStatus of volume %s: %v", *vol.Id, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("CreateVolume Volume %s failed getting available in time: %v", *vol.Id, err))
	}

	klog.V(4).Infof("CreateVolume: Successfully created volume %s in Availability Zone: %s of size %d GiB", *vol.Id, *vol.AvailabilityZone, *vol.Size)

	return getCreateVolumeResponse(vol), nil
}

func (cs *controllerServer) ControllerModifyVolume(_ context.Context, _ *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.V(4).Infof("DeleteVolume: called with args %+v", protosanitizer.StripSecrets(req))

	cloud := cs.Instance

	// Volume Delete
	volID := req.GetVolumeId()
	if volID == "" {
		return nil, status.Error(codes.InvalidArgument, "DeleteVolume Volume ID must be provided")
	}
	err := cloud.DeleteVolume(ctx, volID)
	if err != nil {
		if stackiterrors.IsNotFound(err) {
			klog.V(3).Infof("Volume %s is already deleted.", volID)
			return &csi.DeleteVolumeResponse{}, nil
		}
		klog.Errorf("Failed to DeleteVolume: %v", err)
		return nil, status.Errorf(codes.Internal, "DeleteVolume failed with error %v", err)
	}

	klog.V(4).Infof("DeleteVolume: Successfully deleted volume %s", volID)

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	klog.V(4).Infof("ControllerPublishVolume: called with args %+v", protosanitizer.StripSecrets(req))

	cloud := cs.Instance

	// Volume Attach
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()
	volumeCapability := req.GetVolumeCapability()

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] Volume ID must be provided")
	}
	if instanceID == "" {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] Instance ID must be provided")
	}
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] Volume capability must be provided")
	}

	_, err := cloud.GetVolume(ctx, volumeID)
	if err != nil {
		if stackiterrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "[ControllerPublishVolume] Volume %s not found", volumeID)
		}
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] get volume failed with error %v", err)
	}

	_, err = cloud.GetInstanceByID(ctx, instanceID)
	if err != nil {
		if stackiterrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "[ControllerPublishVolume] Instance %s not found", instanceID)
		}
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] GetInstanceByID failed with error %v", err)
	}

	_, err = cloud.AttachVolume(ctx, instanceID, volumeID)
	if err != nil {
		klog.Errorf("Failed to AttachVolume: %v", err)
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] Attach Volume failed with error %v", err)
	}

	err = cloud.WaitDiskAttached(ctx, instanceID, volumeID)
	if err != nil {
		klog.Errorf("Failed to WaitDiskAttached: %v", err)
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] failed to attach volume: %v", err)
	}

	klog.V(4).Infof("ControllerPublishVolume %s on %s is successful", volumeID, instanceID)

	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (cs *controllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) { //nolint:lll // looks weird when shortened
	klog.V(4).Infof("ControllerUnpublishVolume: called with args %+v", protosanitizer.StripSecrets(req))

	cloud := cs.Instance

	// Volume Detach
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "[ControllerUnpublishVolume] Volume ID must be provided")
	}
	_, err := cloud.GetInstanceByID(ctx, instanceID)
	if err != nil {
		if stackiterrors.IsNotFound(err) {
			klog.V(3).Infof("ControllerUnpublishVolume assuming volume %s is detached, because node %s does not exist", volumeID, instanceID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "[ControllerUnpublishVolume] GetInstanceByID failed with error %v", err)
	}

	err = cloud.DetachVolume(ctx, instanceID, volumeID)
	if err != nil {
		if stackiterrors.IsNotFound(err) {
			klog.V(3).Infof("ControllerUnpublishVolume assuming volume %s is detached, because it does not exist", volumeID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		klog.Errorf("Failed to DetachVolume: %v", err)
		return nil, status.Errorf(codes.Internal, "ControllerUnpublishVolume Detach Volume failed with error %v", err)
	}

	err = cloud.WaitDiskDetached(ctx, instanceID, volumeID)
	if err != nil {
		klog.Errorf("Failed to WaitDiskDetached: %v", err)
		if stackiterrors.IsNotFound(err) {
			klog.V(3).Infof("ControllerUnpublishVolume assuming volume %s is detached, because it was deleted in the meanwhile", volumeID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "ControllerUnpublishVolume failed with error %v", err)
	}

	klog.V(4).Infof("ControllerUnpublishVolume %s on %s", volumeID, instanceID)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *controllerServer) createVolumeEntries(vlist []iaas.Volume) []*csi.ListVolumesResponse_Entry {
	entries := make([]*csi.ListVolumesResponse_Entry, len(vlist))
	for i, v := range vlist {
		entries[i] = &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      *v.Id,
				CapacityBytes: *v.Size * GIBIBYTE,
			},
		}
		if v.ServerId != nil {
			entries[i].Status = &csi.ListVolumesResponse_VolumeStatus{
				PublishedNodeIds: []string{*v.ServerId},
			}
		}
	}
	return entries
}

func (cs *controllerServer) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	klog.V(4).Infof("ListVolumes: called with %+#v request", req)

	if req.MaxEntries < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "[ListVolumes] Invalid max entries request %v, must not be negative ", req.MaxEntries)
	}
	maxEntries := int(req.MaxEntries)
	var err error

	cloud := cs.Instance

	var volumeList []iaas.Volume
	// TODO: There is not pagination for listing volumes so we will just pass empty to startingToken
	// It's not used anyway.
	volumeList, _, err = cloud.ListVolumes(ctx, maxEntries, "")
	if err != nil {
		klog.Errorf("Failed to ListVolumes: %v", err)
		if stackiterrors.IsInvalidError(err) {
			return nil, status.Errorf(codes.Aborted, "[ListVolumes] Invalid request: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "ListVolumes failed with error %v", err)
	}
	volumeEntries := cs.createVolumeEntries(volumeList)

	klog.V(4).Infof("ListVolumes: completed with %d entries", len(volumeEntries))
	return &csi.ListVolumesResponse{
		Entries:   volumeEntries,
		NextToken: "",
	}, nil
}

//nolint:gocyclo,funlen // This function is complex and should be broken down further, but it's ok for now.
func (cs *controllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	klog.V(4).Infof("CreateSnapshot: called with args %+v", protosanitizer.StripSecrets(req))

	cloud := cs.Instance

	name := req.Name
	volumeID := req.GetSourceVolumeId()
	snapshotType := req.Parameters[stackit.SnapshotType]
	filters := map[string]string{"Name": name}
	backupMaxDurationSecondsPerGB := stackit.BackupMaxDurationSecondsPerGBDefault

	// Current time, used for CreatedAt
	var ctime *timestamppb.Timestamp
	// Size of the created snapshot, used to calculate the amount of time to wait for the backup to finish
	var snapSize int64
	// If true, skips creating a snapshot because a backup already exists
	var backupAlreadyExists bool
	var snap *iaas.Snapshot
	var backup *iaas.Backup
	var backups []iaas.Backup
	var err error

	// Set snapshot type to 'snapshot' by default
	if snapshotType == "" {
		snapshotType = "snapshot"
	}

	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "Snapshot name must be provided in CreateSnapshot request")
	}

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeID must be provided in CreateSnapshot request")
	}

	// Verify snapshot type has a valid value
	if snapshotType != "snapshot" && snapshotType != "backup" {
		return nil, status.Error(codes.InvalidArgument, "Snapshot type must be 'backup', 'snapshot' or not defined")
	}

	// Prechecks in case of a backup
	if snapshotType == "backup" {
		// Get a list of backups with the provided name
		backups, err = cloud.ListBackups(ctx, filters)
		if err != nil {
			klog.Errorf("Failed to query for existing Backup during CreateSnapshot: %v", err)
			return nil, status.Error(codes.Internal, "Failed to get backups")
		}
		// If more than one backup with the provided name exists, fail
		if len(backups) > 1 {
			klog.Errorf("found multiple existing backups with selected name (%s) during create", name)
			return nil, status.Error(codes.Internal, "Multiple backups reported by Cinder with same name")
		}

		if len(backups) == 1 {
			backup = &backups[0]
			// Verify the existing backup has the same VolumeID, otherwise it belongs to another volume
			if *backup.VolumeId != volumeID {
				klog.Errorf("found existing backup for volumeID (%s) but different source volume ID (%s)", volumeID, *backup.VolumeId)
				return nil, status.Error(codes.AlreadyExists, "Backup with given name already exists, with different source volume ID")
			}

			// If a backup of the volume already exists, skip creating the snapshot
			backupAlreadyExists = true
			klog.V(3).Infof("Found existing backup %s from volume with ID: %s", name, volumeID)
		}

		// Get the max duration to wait in seconds per GB of snapshot and fail if parsing fails
		if item, ok := (req.Parameters)[stackit.BackupMaxDurationPerGB]; ok {
			backupMaxDurationSecondsPerGB, err = strconv.Atoi(item)
			if err != nil {
				klog.Errorf("Setting backup-max-duration-seconds-per-gb failed due to a parsing error: %v", err)
				return nil, status.Error(codes.Internal, "Failed to parse backup-max-duration-seconds-per-gb")
			}
		}
	}

	// Create the snapshot if the backup does not already exist and wait for it to be ready
	if !backupAlreadyExists {
		snap, err = cs.createSnapshot(ctx, cloud, name, volumeID, req.Parameters)
		if err != nil {
			return nil, err
		}

		ctime = timestamppb.New(*snap.CreatedAt)
		if err = ctime.CheckValid(); err != nil {
			klog.Errorf("Error to convert time to timestamp: %v", err)
		}

		snap.Status, err = cloud.WaitSnapshotReady(ctx, *snap.Id)
		if err != nil {
			klog.Errorf("Failed to WaitSnapshotReady: %v", err)
			return nil, status.Errorf(codes.Internal, "CreateSnapshot failed with error: %v. Current snapshot status: %v", err, snap.Status)
		}

		snapSize = *snap.Size
	}

	// Early exit
	if snapshotType == "snapshot" {
		return &csi.CreateSnapshotResponse{
			Snapshot: &csi.Snapshot{
				SnapshotId:     *snap.Id,
				SizeBytes:      *snap.Size * GIBIBYTE,
				SourceVolumeId: *snap.VolumeId,
				CreationTime:   ctime,
				ReadyToUse:     true,
			},
		}, nil
	}

	// snapshotType == backup
	// If snapshotType is 'backup', create a backup from the snapshot and delete the snapshot.
	if !backupAlreadyExists {
		backup, err = cs.createBackup(ctx, cloud, name, volumeID, snap, req.Parameters)
		if err != nil {
			return nil, err
		}
	}

	ctime = timestamppb.New(*backup.CreatedAt)
	if err := ctime.CheckValid(); err != nil {
		klog.Errorf("Error to convert time to timestamp: %v", err)
	}

	backup.Status, err = cloud.WaitBackupReady(ctx, *backup.Id, snapSize, backupMaxDurationSecondsPerGB)
	if err != nil {
		klog.Errorf("Failed to WaitBackupReady: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("CreateBackup failed with error %v. Current backups status: %s", err, *backup.Status))
	}

	// Necessary to get all the backup information, including size.
	backup, err = cloud.GetBackupByID(ctx, *backup.Id)
	if err != nil {
		klog.Errorf("Failed to GetBackupByID after backup creation: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("GetBackupByID failed with error %v", err))
	}

	err = cloud.DeleteSnapshot(ctx, *backup.SnapshotId)
	if err != nil && !stackiterrors.IsNotFound(err) {
		klog.Errorf("Failed to DeleteSnapshot: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("DeleteSnapshot failed with error %v", err))
	}

	return &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     *backup.Id,
			SizeBytes:      *backup.Size * GIBIBYTE,
			SourceVolumeId: *backup.VolumeId,
			CreationTime:   ctime,
			ReadyToUse:     true,
		},
	}, nil
}

func (cs *controllerServer) createSnapshot(ctx context.Context, cloud stackit.IaasClient, name, volumeID string, parameters map[string]string) (*iaas.Snapshot, error) { //nolint:lll // looks weird when shortened
	filters := map[string]string{}
	filters["Name"] = name

	// List existing snapshots with the same name
	snapshots, _, err := cloud.ListSnapshots(ctx, filters)
	if err != nil {
		klog.Errorf("Failed to query for existing Snapshot during CreateSnapshot: %v", err)
		return nil, status.Error(codes.Internal, "Failed to get snapshots")
	}

	// If more than one snapshot with the provided name exists, fail
	if len(snapshots) > 1 {
		klog.Errorf("found multiple existing snapshots with selected name (%s) during create", name)

		return nil, status.Error(codes.Internal, "Multiple snapshots reported by Cinder with same name")
	}

	// Verify a snapshot with the provided name doesn't already exist for this tenant
	if len(snapshots) == 1 {
		snap := &snapshots[0]
		if *snap.VolumeId != volumeID {
			return nil, status.Error(codes.AlreadyExists, "Snapshot with given name already exists, with different source volume ID")
		}

		// If the snapshot for the correct volume already exists, return it
		klog.V(3).Infof("Found existing snapshot %s from volume with ID: %s", name, volumeID)
		return snap, nil
	}

	// Add cluster ID to the snapshot metadata
	// TODO: Use once IaaS has extended the label regex to allow for forward slashes and dots
	// properties := map[string]string{blockStorageCSIClusterIDKey: cs.Driver.clusterID}
	properties := map[string]string{}

	// see https://github.com/kubernetes-csi/external-snapshotter/pull/375/
	// Also, we don't want to tag every param, but we do honor the RecognizedCSISnapshotterParams
	for _, mKey := range sharedcsi.RecognizedCSISnapshotterParams {
		if v, ok := parameters[mKey]; ok {
			properties[mKey] = v
		}
	}

	snap, err := cloud.CreateSnapshot(ctx, name, volumeID, properties)
	if err != nil {
		klog.Errorf("Failed to Create snapshot: %v", err)
		return nil, status.Errorf(codes.Internal, "CreateSnapshot failed with error %v", err)
	}

	klog.V(3).Infof("CreateSnapshot %s from volume with ID: %s", name, volumeID)

	return snap, nil
}

func (cs *controllerServer) createBackup(ctx context.Context, cloud stackit.IaasClient, name, volumeID string, snap *iaas.Snapshot, parameters map[string]string) (*iaas.Backup, error) { //nolint:lll // looks weird when shortened
	// Add cluster ID to the snapshot metadata
	// TODO: Use once IaaS has extended the label regex to allow for forward slashes and dots
	// properties := map[string]string{blockStorageCSIClusterIDKey: cs.Driver.clusterID}
	properties := map[string]string{}

	// see https://github.com/kubernetes-csi/external-snapshotter/pull/375/
	// Also, we don't want to tag every param, but we do honor the RecognizedCSISnapshotterParams
	for _, mKey := range append(sharedcsi.RecognizedCSISnapshotterParams, stackit.SnapshotType) {
		if v, ok := parameters[mKey]; ok {
			properties[mKey] = v
		}
	}

	backup, err := cloud.CreateBackup(ctx, name, volumeID, *snap.Id, properties)
	if err != nil {
		klog.Errorf("Failed to Create backup: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("CreateBackup failed with error %v", err))
	}
	klog.V(4).Infof("Backup created: %+v", backup)

	return backup, nil
}

func (cs *controllerServer) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	klog.V(4).Infof("DeleteSnapshot: called with args %+v", protosanitizer.StripSecrets(req))

	cloud := cs.Instance

	id := req.GetSnapshotId()

	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "Snapshot ID must be provided in DeleteSnapshot request")
	}

	// If volumeSnapshot object was linked to a cinder backup, delete the backup.
	back, err := cloud.GetBackupByID(ctx, id)
	if err == nil && back != nil {
		err = cloud.DeleteBackup(ctx, id)
		if err != nil {
			klog.Errorf("Failed to Delete backup: %v", err)
			return nil, status.Error(codes.Internal, fmt.Sprintf("DeleteBackup failed with error %v", err))
		}
	}

	// Delegate the check to stackit itself
	err = cloud.DeleteSnapshot(ctx, id)
	if err != nil {
		if stackiterrors.IsNotFound(err) {
			klog.V(3).Infof("Snapshot %s is already deleted.", id)
			return &csi.DeleteSnapshotResponse{}, nil
		}
		klog.Errorf("Failed to Delete snapshot: %v", err)
		return nil, status.Errorf(codes.Internal, "DeleteSnapshot failed with error %v", err)
	}
	return &csi.DeleteSnapshotResponse{}, nil
}

func (cs *controllerServer) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	cloud := cs.Instance

	snapshotID := req.GetSnapshotId()
	if snapshotID != "" {
		snap, err := cloud.GetSnapshotByID(ctx, snapshotID)
		if err != nil {
			if stackiterrors.IsNotFound(err) {
				klog.V(3).Infof("Snapshot %s not found", snapshotID)
				return &csi.ListSnapshotsResponse{}, nil
			}
			return nil, status.Errorf(codes.Internal, "Failed to GetSnapshot %s: %v", snapshotID, err)
		}

		ctime := timestamppb.New(*snap.CreatedAt)

		entry := &csi.ListSnapshotsResponse_Entry{
			Snapshot: &csi.Snapshot{
				SizeBytes:      *snap.Size * GIBIBYTE,
				SnapshotId:     *snap.Id,
				SourceVolumeId: *snap.VolumeId,
				CreationTime:   ctime,
				ReadyToUse:     true,
			},
		}

		entries := []*csi.ListSnapshotsResponse_Entry{entry}
		return &csi.ListSnapshotsResponse{
			Entries: entries,
		}, ctime.CheckValid()
	}

	filters := map[string]string{}

	var slist []iaas.Snapshot
	var err error
	var nextPageToken string

	// Add the filters
	if req.GetSourceVolumeId() != "" {
		filters["VolumeID"] = req.GetSourceVolumeId()
	} else {
		filters["Limit"] = strconv.Itoa(int(req.MaxEntries))
		filters["Marker"] = req.StartingToken
	}

	// Only retrieve snapshots that are available
	filters["Status"] = stackit.SnapshotReadyStatus
	slist, nextPageToken, err = cloud.ListSnapshots(ctx, filters)
	if err != nil {
		klog.Errorf("Failed to ListSnapshots: %v", err)
		return nil, status.Errorf(codes.Internal, "ListSnapshots failed with error %v", err)
	}

	sentries := make([]*csi.ListSnapshotsResponse_Entry, 0, len(slist))
	for _, v := range slist {
		ctime := timestamppb.New(*v.CreatedAt)
		if err := ctime.CheckValid(); err != nil {
			klog.Errorf("Error to convert time to timestamp: %v", err)
		}
		sentry := csi.ListSnapshotsResponse_Entry{
			Snapshot: &csi.Snapshot{
				SizeBytes:      *v.Size * GIBIBYTE,
				SnapshotId:     *v.Id,
				SourceVolumeId: *v.VolumeId,
				CreationTime:   ctime,
				ReadyToUse:     true,
			},
		}
		sentries = append(sentries, &sentry)
	}
	return &csi.ListSnapshotsResponse{
		Entries:   sentries,
		NextToken: nextPageToken,
	}, nil
}

// ControllerGetCapabilities implements the default GRPC callout.
// Default supports all capabilities
func (cs *controllerServer) ControllerGetCapabilities(_ context.Context, _ *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	klog.V(5).Infof("Using default ControllerGetCapabilities")

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: cs.Driver.cscap,
	}, nil
}

func (cs *controllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) { //nolint:lll // looks weird when shortened
	cloud := cs.Instance

	reqVolCap := req.GetVolumeCapabilities()

	if len(reqVolCap) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ValidateVolumeCapabilities Volume Capabilities must be provided")
	}
	volumeID := req.GetVolumeId()

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "ValidateVolumeCapabilities Volume ID must be provided")
	}

	_, err := cloud.GetVolume(ctx, volumeID)
	if err != nil {
		if stackiterrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "ValidateVolumeCapabilities Volume %s not found", volumeID)
		}
		return nil, status.Errorf(codes.Internal, "ValidateVolumeCapabilities %v", err)
	}

	for _, volCap := range reqVolCap {
		if volCap.GetAccessMode().GetMode() != cs.Driver.vcap[0].Mode {
			return &csi.ValidateVolumeCapabilitiesResponse{Message: "Requested Volume Capability not supported"}, nil
		}
	}

	// Block Storage CSI driver currently supports one mode only
	resp := &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: cs.Driver.vcap[0],
				},
			},
		},
	}

	return resp, nil
}

func (cs *controllerServer) GetCapacity(_ context.Context, _ *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetCapacity is not yet implemented")
}

func (cs *controllerServer) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	klog.V(4).Infof("ControllerGetVolume: called with args %+v", protosanitizer.StripSecrets(req))

	cloud := cs.Instance
	volumeID := req.GetVolumeId()

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	var volume *iaas.Volume
	var err error
	volume, err = cloud.GetVolume(ctx, volumeID)
	if err != nil {
		if stackiterrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "Volume %s not found", volumeID)
		}
		return nil, status.Errorf(codes.Internal, "ControllerGetVolume failed with error %v", err)
	}

	ventry := csi.ControllerGetVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      volumeID,
			CapacityBytes: *volume.Size * GIBIBYTE,
		},
	}

	volumeStatus := &csi.ControllerGetVolumeResponse_VolumeStatus{}
	volumeStatus.PublishedNodeIds = []string{*volume.ServerId}
	ventry.Status = volumeStatus

	return &ventry, nil
}

func (cs *controllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	klog.V(4).Infof("ControllerExpandVolume: called with args %+v", protosanitizer.StripSecrets(req))

	cloud := cs.Instance

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}
	volCap := req.GetCapacityRange()
	if volCap == nil {
		return nil, status.Error(codes.InvalidArgument, "Capacity range not provided")
	}

	volSizeBytes := req.GetCapacityRange().GetRequiredBytes()
	volSizeGB := roundUpSize(volSizeBytes, GIBIBYTE)
	maxVolSize := volCap.GetLimitBytes()

	if maxVolSize > 0 && maxVolSize < volSizeBytes {
		return nil, status.Error(codes.OutOfRange, "After round-up, volume size exceeds the limit specified")
	}

	volume, err := cloud.GetVolume(ctx, volumeID)
	if err != nil {
		if stackiterrors.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "Volume not found")
		}
		return nil, status.Errorf(codes.Internal, "GetVolume failed with error %v", err)
	}

	if *volume.Size >= volSizeGB {
		// a volume was already resized
		klog.V(2).Infof("Volume %q has been already expanded to %d, requested %d", volumeID, volume.Size, volSizeGB)
		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         *volume.Size * GIBIBYTE,
			NodeExpansionRequired: true,
		}, nil
	}

	err = cloud.ExpandVolume(ctx, volumeID, *volume.Status, volSizeGB)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not resize volume %q to size %v: %v", volumeID, volSizeGB, err)
	}

	// we need wait for the volume to be available or InUse, it might be error_extending in some scenario
	targetStatus := []string{stackit.VolumeAvailableStatus, stackit.VolumeAttachedStatus}
	err = cloud.WaitVolumeTargetStatus(ctx, volumeID, targetStatus)
	if err != nil {
		klog.Errorf("Failed to WaitVolumeTargetStatus of volume %s: %v", volumeID, err)
		return nil, status.Errorf(codes.Internal, "[ControllerExpandVolume] Volume %s not in target state after resize operation: %v", volumeID, err)
	}

	klog.V(4).Infof("ControllerExpandVolume resized volume %v to size %v", volumeID, volSizeGB)

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         volSizeBytes,
		NodeExpansionRequired: true,
	}, nil
}

func getCreateVolumeResponse(vol *iaas.Volume) *csi.CreateVolumeResponse {
	var volsrc *csi.VolumeContentSource
	var volumeSourceType stackit.VolumeSourceTypes
	volCnx := map[string]string{}

	if vol.Source != nil {
		volumeSourceType = stackit.VolumeSourceTypes(*vol.Source.Type)
		switch volumeSourceType {
		case stackit.VolumeSource:
			volCnx[ResizeRequired] = "true"

			volsrc = &csi.VolumeContentSource{
				Type: &csi.VolumeContentSource_Volume{
					Volume: &csi.VolumeContentSource_VolumeSource{
						VolumeId: *vol.Source.Id,
					},
				},
			}
		case stackit.BackupSource:
			volCnx[ResizeRequired] = "true"

			volsrc = &csi.VolumeContentSource{
				Type: &csi.VolumeContentSource_Snapshot{
					Snapshot: &csi.VolumeContentSource_SnapshotSource{
						SnapshotId: *vol.Source.Id,
					},
				},
			}
		case stackit.SnapshotSource:
			volCnx[ResizeRequired] = "true"

			volsrc = &csi.VolumeContentSource{
				Type: &csi.VolumeContentSource_Snapshot{
					Snapshot: &csi.VolumeContentSource_SnapshotSource{
						SnapshotId: *vol.Source.Id,
					},
				},
			}
		}
	}

	accessibleTopology := []*csi.Topology{
		{
			Segments: map[string]string{topologyKey: ptr.Deref(vol.AvailabilityZone, "")},
		},
	}

	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:           *vol.Id,
			CapacityBytes:      *vol.Size * GIBIBYTE,
			AccessibleTopology: accessibleTopology,
			ContentSource:      volsrc,
			VolumeContext:      volCnx,
		},
	}

	return resp
}

// determineSourceIDForSourceType returns the correct sourceID for the given stackit.VolumeSourceTypes
func determineSourceIDForSourceType(srcType stackit.VolumeSourceTypes, sourceSnapshotID, sourceVolID string) string {
	switch srcType {
	case stackit.BackupSource:
		return sourceSnapshotID
	case stackit.SnapshotSource:
		return sourceSnapshotID
	case stackit.VolumeSource:
		return sourceVolID
	default:
		return ""
	}
}
