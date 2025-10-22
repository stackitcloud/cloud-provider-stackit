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
	"os"
	"path/filepath"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	mountutil "k8s.io/mount-utils"
	utilpath "k8s.io/utils/path"

	sharedcsi "github.com/stackitcloud/cloud-provider-stackit/pkg/csi"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi/util/blockdevice"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi/util/mount"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"
)

type nodeServer struct {
	Driver     *Driver
	Mount      mount.IMount
	Metadata   metadata.IMetadata
	Opts       stackit.BlockStorageOpts
	Topologies map[string]string
	csi.UnimplementedNodeServer
}

func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	klog.V(4).Infof("NodePublishVolume: called with args %+v", protosanitizer.StripSecrets(req))

	volumeID := req.GetVolumeId()
	source := req.GetStagingTargetPath()
	targetPath := req.GetTargetPath()
	volumeCapability := req.GetVolumeCapability()

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Volume ID must be provided")
	}
	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Target Path must be provided")
	}
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Volume Capability must be provided")
	}

	ephemeralVolume := req.GetVolumeContext()[sharedcsi.VolEphemeralKey] == "true"
	if ephemeralVolume {
		// See https://github.com/kubernetes/cloud-provider-openstack/issues/2599
		return nil, status.Error(codes.Unimplemented, "CSI inline ephemeral volumes support is removed in 1.31 release.")
	}

	// In case of ephemeral volume staging path not provided
	if source == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Staging Target Path must be provided")
	}

	mountOptions := []string{"bind"}
	if req.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	} else {
		mountOptions = append(mountOptions, "rw")
	}

	if blk := volumeCapability.GetBlock(); blk != nil {
		return nodePublishVolumeForBlock(ctx, req, ns, mountOptions)
	}

	m := ns.Mount
	// Verify whether mounted
	notMnt, err := m.IsLikelyNotMountPointAttach(targetPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Volume Mount
	if notMnt {
		fsType := "ext4"
		if mnt := volumeCapability.GetMount(); mnt != nil {
			if mnt.FsType != "" {
				fsType = mnt.FsType
			}
		}
		// Mount
		err = m.Mounter().Mount(source, targetPath, fsType, mountOptions)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func nodePublishVolumeForBlock(ctx context.Context, req *csi.NodePublishVolumeRequest, ns *nodeServer, mountOptions []string) (*csi.NodePublishVolumeResponse, error) { //nolint:lll // looks weird when shortened
	klog.V(4).Infof("NodePublishVolumeBlock: called with args %+v", protosanitizer.StripSecrets(req))

	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()
	podVolumePath := filepath.Dir(targetPath)

	m := ns.Mount

	// Do not trust the path provided by cinder, get the real path on node
	source, err := getDevicePath(ctx, volumeID, m)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unable to find Device path for volume: %v", err)
	}

	exists, err := utilpath.Exists(utilpath.CheckFollowSymlink, podVolumePath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !exists {
		if err := m.MakeDir(podVolumePath); err != nil {
			return nil, status.Errorf(codes.Internal, "Could not create dir %q: %v", podVolumePath, err)
		}
	}
	err = m.MakeFile(targetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error in making file %v", err)
	}

	if err := m.Mounter().Mount(source, targetPath, "", mountOptions); err != nil {
		if removeErr := os.Remove(targetPath); removeErr != nil {
			return nil, status.Errorf(codes.Internal, "Could not remove mount target %q: %v", targetPath, err)
		}
		return nil, status.Errorf(codes.Internal, "Could not mount %q at %q: %v", source, targetPath, err)
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnpublishVolume(_ context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	klog.V(4).Infof("NodeUnPublishVolume: called with args %+v", protosanitizer.StripSecrets(req))

	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "[NodeUnpublishVolume] Target Path must be provided")
	}
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "[NodeUnpublishVolume] volumeID must be provided")
	}

	if err := ns.Mount.UnmountPath(targetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "Unmount of targetpath %s failed with error %v", targetPath, err)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	klog.V(4).Infof("NodeStageVolume: called with args %+v", protosanitizer.StripSecrets(req))

	stagingTarget, volumeCapability, volumeContext, volumeID, err := validateNodeStageVolumeRequest(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	m := ns.Mount
	// Do not trust the path provided by cinder, get the real path on node
	devicePath, err := getDevicePath(ctx, volumeID, m)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unable to find Device path for volume: %v", err)
	}

	if blk := volumeCapability.GetBlock(); blk != nil {
		// If block volume, do nothing
		return &csi.NodeStageVolumeResponse{}, nil
	}

	// Verify whether mounted
	notMnt, err := m.IsLikelyNotMountPointAttach(stagingTarget)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Volume Mount
	if notMnt {
		// set default fstype is ext4
		fsType := "ext4"
		var options []string
		if mnt := volumeCapability.GetMount(); mnt != nil {
			if mnt.FsType != "" {
				fsType = mnt.FsType
			}
			mountFlags := mnt.GetMountFlags()
			options = append(options, collectMountOptions(fsType, mountFlags)...)
		}
		// Mount
		err = ns.formatAndMountRetry(devicePath, stagingTarget, fsType, options)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	if required, ok := volumeContext[ResizeRequired]; ok && strings.EqualFold(required, "true") {
		r := mountutil.NewResizeFs(ns.Mount.Mounter().Exec)

		needResize, err := r.NeedResize(devicePath, stagingTarget)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Could not determine if volume %q need to be resized: %v", volumeID, err)
		}

		if needResize {
			klog.V(4).Infof("NodeStageVolume: Resizing volume %q created from a snapshot/volume", volumeID)
			if _, err := r.Resize(devicePath, stagingTarget); err != nil {
				return nil, status.Errorf(codes.Internal, "Could not resize volume %q: %v", volumeID, err)
			}
		}
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func validateNodeStageVolumeRequest(req *csi.NodeStageVolumeRequest) (stagingTarget string, volumeCapability *csi.VolumeCapability, volumeContext map[string]string, volumeID string, err error) { //nolint:lll // looks weird when shortened
	stagingTarget = req.GetStagingTargetPath()
	volumeCapability = req.GetVolumeCapability()
	volumeContext = req.GetVolumeContext()
	volumeID = req.GetVolumeId()
	err = nil

	if volumeID == "" {
		err = status.Error(codes.InvalidArgument, "Volume Id not provided")
		return
	}
	if stagingTarget == "" {
		err = status.Error(codes.InvalidArgument, "Staging target not provided")
		return
	}
	if volumeCapability == nil {
		err = status.Error(codes.InvalidArgument, "NodeStageVolume Volume Capability must be provided")
		return
	}
	return
}

// formatAndMountRetry attempts to format and mount a device at the given path.
// If the initial mount fails, it rescans the device and retries the mount operation.
func (ns *nodeServer) formatAndMountRetry(devicePath, stagingTarget, fsType string, options []string) error {
	m := ns.Mount
	err := m.Mounter().FormatAndMount(devicePath, stagingTarget, fsType, options)
	if err != nil {
		klog.Infof("Initial format and mount failed: %v. Attempting rescan.", err)
		// Attempting rescan if the initial mount fails
		rescanErr := blockdevice.RescanDevice(devicePath)
		if rescanErr != nil {
			klog.Infof("Rescan failed: %v. Returning original mount error.", rescanErr)
			return err
		}
		klog.Infof("Rescan succeeded, retrying format and mount")
		err = m.Mounter().FormatAndMount(devicePath, stagingTarget, fsType, options)
	}
	return err
}

func (ns *nodeServer) NodeUnstageVolume(_ context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	klog.V(4).Infof("NodeUnstageVolume: called with args %+v", protosanitizer.StripSecrets(req))

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume Id not provided")
	}

	stagingTargetPath := req.GetStagingTargetPath()
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume Staging Target Path must be provided")
	}

	err := ns.Mount.UnmountPath(stagingTargetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unmount of targetPath %s failed with error %v", stagingTargetPath, err)
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (ns *nodeServer) NodeGetInfo(ctx context.Context, _ *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	nodeID, err := ns.Metadata.GetInstanceID(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "[NodeGetInfo] unable to retrieve instance id of node %v", err)
	}

	flavor, err := ns.Metadata.GetFlavor(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "[NodeGetInfo] unable to retrieve flavor of node %v", err)
	}

	maxVolumesPerNode := DetermineMaxVolumesByFlavor(flavor)
	// Subtract 1 for root disk and another for configDrive/spare
	maxVolumesPerNode -= 2
	klog.V(4).Infof("Determined node to support %d volumes", maxVolumesPerNode)

	nodeInfo := &csi.NodeGetInfoResponse{
		NodeId:            nodeID,
		MaxVolumesPerNode: maxVolumesPerNode,
	}

	zone, err := ns.Metadata.GetAvailabilityZone(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "[NodeGetInfo] Unable to retrieve availability zone of node %v", err)
	}
	topologyMap := make(map[string]string, len(ns.Topologies)+1)
	topologyMap[topologyKey] = zone
	for k, v := range ns.Topologies {
		topologyMap[k] = v
	}
	nodeInfo.AccessibleTopology = &csi.Topology{Segments: topologyMap}

	return nodeInfo, nil
}

func (ns *nodeServer) NodeGetCapabilities(_ context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	klog.V(5).Infof("NodeGetCapabilities called with req: %#v", req)

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: ns.Driver.nscap,
	}, nil
}

func (ns *nodeServer) NodeGetVolumeStats(_ context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	klog.V(4).Infof("NodeGetVolumeStats: called with args %+v", protosanitizer.StripSecrets(req))

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume Id not provided")
	}

	volumePath := req.GetVolumePath()
	if volumePath == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume path not provided")
	}

	exists, err := utilpath.Exists(utilpath.CheckFollowSymlink, req.VolumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check whether volumePath exists: %v", err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "target: %s not found", volumePath)
	}
	stats, err := ns.Mount.GetDeviceStats(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get stats by path: %v", err)
	}

	if stats.Block {
		return &csi.NodeGetVolumeStatsResponse{
			Usage: []*csi.VolumeUsage{
				{
					Total: stats.TotalBytes,
					Unit:  csi.VolumeUsage_BYTES,
				},
			},
		}, nil
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{Total: stats.TotalBytes, Available: stats.AvailableBytes, Used: stats.UsedBytes, Unit: csi.VolumeUsage_BYTES},
			{Total: stats.TotalInodes, Available: stats.AvailableInodes, Used: stats.UsedInodes, Unit: csi.VolumeUsage_INODES},
		},
	}, nil
}

func (ns *nodeServer) NodeExpandVolume(_ context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	klog.V(4).Infof("NodeExpandVolume: called with args %+v", protosanitizer.StripSecrets(req))

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}
	volumePath := req.GetVolumePath()
	if volumePath == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume path not provided")
	}
	volumeCapability := req.GetVolumeCapability()

	if volumeCapability != nil {
		if block := volumeCapability.GetBlock(); block != nil {
			// volumeMode: Block is a Noop
			klog.V(4).InfoS("NodeExpandVolume: called. Since it is a block device, ignoring...", "volumeID", volumeID, "volumePath", volumePath)
			return &csi.NodeExpandVolumeResponse{}, nil
		}
	} else {
		// VolumeCapability is nil, check if volumePath point to a block device
		// Prevents trying to-do resize operations on block devices.
		isBlockDevice, err := blockdevice.IsBlockDevice(volumePath)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "Failed to determine device path for volumePath %s: %v", volumePath, err)
		}

		if isBlockDevice {
			blockStats, err := ns.Mount.GetDeviceStats(volumePath)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get block capacity on path %s: %v", volumePath, err)
			}
			klog.V(4).InfoS("NodeExpandVolume: called, since given volumePath is a block device, ignoring...", "volumeID", volumeID, "volumePath", volumePath)
			return &csi.NodeExpandVolumeResponse{CapacityBytes: blockStats.TotalBytes}, nil
		}
	}

	output, err := ns.Mount.GetMountFs(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to find mount file system %s: %v", volumePath, err)
	}

	devicePath := strings.TrimSpace(string(output))
	if devicePath == "" {
		return nil, status.Error(codes.Internal, "Unable to find Device path for volume")
	}

	if ns.Opts.RescanOnResize {
		// comparing current volume size with the expected one
		newSize := req.GetCapacityRange().GetRequiredBytes()
		// Since we only create volumes to the next available GB, there is no need to compare bytes.
		newSize = util.RoundUpSize(newSize, util.GIBIBYTE)
		if err := blockdevice.RescanBlockDeviceGeometry(devicePath, volumePath, newSize); err != nil {
			return nil, status.Errorf(codes.Internal, "Could not verify %q volume size: %v", volumeID, err)
		}
	}

	r := mountutil.NewResizeFs(ns.Mount.Mounter().Exec)
	if _, err := r.Resize(devicePath, volumePath); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not resize volume %q: %v", volumeID, err)
	}
	stats, err := ns.Mount.GetDeviceStats(devicePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get stats by path: %v", err)
	}
	return &csi.NodeExpandVolumeResponse{CapacityBytes: stats.TotalBytes}, nil
}

func getDevicePath(ctx context.Context, volumeID string, m mount.IMount) (string, error) {
	var devicePath string
	devicePath, err := m.GetDevicePath(volumeID)
	if err != nil {
		klog.Warningf("Couldn't get device path from mount: %v", err)
	}

	if devicePath == "" {
		// try to get from metadata service
		klog.Info("Trying to get device path from metadata service")
		devicePath, err = metadata.GetDevicePath(ctx, volumeID)
		if err != nil {
			klog.Errorf("Couldn't get device path from metadata service: %v", err)
			return "", fmt.Errorf("couldn't get device path from metadata service: %v", err)
		}
	}

	return devicePath, nil
}

func collectMountOptions(fsType string, mntFlags []string) []string {
	var options []string
	options = append(options, mntFlags...)

	// By default, xfs does not allow mounting of two volumes with the same filesystem uuid.
	// Force ignore this uuid to be able to mount volume + its clone / restored snapshot on the same node.
	if fsType == "xfs" {
		options = append(options, "nouuid")
	}
	return options
}
