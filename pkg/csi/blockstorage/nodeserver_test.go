package blockstorage

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	mountutils "k8s.io/mount-utils"

	sharedcsi "github.com/stackitcloud/cloud-provider-stackit/pkg/csi"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi/util/mount"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"
)

var _ = Describe("NodeServer", func() {
	var (
		ns           *nodeServer
		fakeEndpoint = "tcp://127.0.0.1:10000"
		fakeCluster  = "cluster"
		req          *csi.NodePublishVolumeRequest
		mountMock    *mount.MockIMount
		metadataMock *metadata.MockIMetadata
	)

	BeforeEach(func() {
		d := NewDriver(&DriverOpts{Endpoint: fakeEndpoint, ClusterID: fakeCluster})

		ctrl := gomock.NewController(GinkgoT())

		mountMock = mount.NewMockIMount(ctrl)
		mount.MInstance = mountMock

		metadataMock = metadata.NewMockIMetadata(ctrl)
		metadata.MetadataService = metadataMock

		ns = NewNodeServer(
			d,
			mountMock,
			metadataMock,
			stackit.BlockStorageOpts{},
			map[string]string{}, // topologies
		)
	})

	Describe("NodePublishVolume", func() {
		BeforeEach(func() {
			req = &csi.NodePublishVolumeRequest{
				VolumeId:   "volume-id",
				TargetPath: "/target/path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{
							MountFlags: []string{"--foo"},
						},
					},
				},
				StagingTargetPath: "/staging/target/path",
			}
		})

		It("should fail if the volumeID is empty", func() {
			req.VolumeId = ""

			_, err := ns.NodePublishVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(err).To(MatchError(ContainSubstring("Volume ID must be provided")))
		})

		It("should fail if the targetPath is empty", func() {
			req.TargetPath = ""

			_, err := ns.NodePublishVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(err).To(MatchError(ContainSubstring("Target Path must be provided")))
		})

		It("should fail if no volumeCapabilty is provided", func() {
			req.VolumeCapability = nil

			_, err := ns.NodePublishVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(err).To(MatchError(ContainSubstring("Volume Capability must be provided")))
		})

		It("should fail if an ephemeral volume is requested", func() {
			req.VolumeContext = map[string]string{
				sharedcsi.VolEphemeralKey: "true",
			}

			_, err := ns.NodePublishVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.Unimplemented))
			Expect(err).To(MatchError(ContainSubstring("CSI inline ephemeral volumes support is removed")))
		})

		It("should fail if staging target path is empty", func() {
			req.StagingTargetPath = ""

			_, err := ns.NodePublishVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(err).To(MatchError(ContainSubstring("Staging Target Path must be provided")))
		})

		It("should mount successfully, if a mount volume is requests in the volume capabilities", func() {
			mountPoints := make([]mountutils.MountPoint, 0)
			mounter := mountutils.NewFakeMounter(mountPoints)

			mountMock.EXPECT().IsLikelyNotMountPointAttach("/target/path").Return(true, nil)
			mountMock.EXPECT().Mounter().Return(mountutils.NewSafeFormatAndMount(mounter, nil))

			_, err := ns.NodePublishVolume(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(mounter.MountPoints).To(HaveLen(1))
			Expect(mounter.MountPoints[0].Path).To(Equal("/target/path"))
			Expect(mounter.MountPoints[0].Type).To(Equal("ext4"))
		})

		It("should mount successfully, if a block volume is requests in the volume capabilities", func() {
			req.VolumeCapability.AccessType = &csi.VolumeCapability_Block{
				Block: &csi.VolumeCapability_BlockVolume{},
			}

			mountPoints := make([]mountutils.MountPoint, 0)
			mounter := mountutils.NewFakeMounter(mountPoints)

			mountMock.EXPECT().GetDevicePath("volume-id").Return("/dev/ice", nil)
			mountMock.EXPECT().MakeDir("/target").Return(nil)
			mountMock.EXPECT().MakeFile("/target/path").Return(nil)
			mountMock.EXPECT().Mounter().Return(mountutils.NewSafeFormatAndMount(mounter, nil))

			_, err := ns.NodePublishVolume(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(mounter.MountPoints).To(HaveLen(1))
			Expect(mounter.MountPoints[0].Path).To(Equal("/target/path"))
		})

		It("should mount rw by default", func() {
			mountPoints := make([]mountutils.MountPoint, 0)
			mounter := mountutils.NewFakeMounter(mountPoints)

			mountMock.EXPECT().IsLikelyNotMountPointAttach("/target/path").Return(true, nil)
			mountMock.EXPECT().Mounter().Return(mountutils.NewSafeFormatAndMount(mounter, nil))

			_, err := ns.NodePublishVolume(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(mounter.MountPoints[0].Opts).To(Equal([]string{"bind", "rw"}))
		})

		It("should mount ro if requested", func() {
			req.Readonly = true

			mountPoints := make([]mountutils.MountPoint, 0)
			mounter := mountutils.NewFakeMounter(mountPoints)

			mountMock.EXPECT().IsLikelyNotMountPointAttach("/target/path").Return(true, nil)
			mountMock.EXPECT().Mounter().Return(mountutils.NewSafeFormatAndMount(mounter, nil))

			_, err := ns.NodePublishVolume(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(mounter.MountPoints[0].Opts).To(Equal([]string{"bind", "ro"}))
		})
	})

	Describe("NodeUnpublishVolume", func() {})
	Describe("NodeStageVolume", func() {})
	Describe("NodeUnstageVolume", func() {})
	Describe("NodeGetInfo", func() {})
	Describe("NodeGetCapabilities", func() {})
	Describe("NodeGetVolumeStats", func() {})
	Describe("NodeExpandVolume", func() {})
})
