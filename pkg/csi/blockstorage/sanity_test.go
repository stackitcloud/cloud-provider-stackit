package blockstorage

import (
	"context"
	"math/rand"
	"net/http"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-test/v5/pkg/sanity"
	. "github.com/onsi/ginkgo/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	mountutils "k8s.io/mount-utils"
	exec "k8s.io/utils/exec/testing"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi/util/mount"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"
	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"go.uber.org/mock/gomock"
)

// randString helper (from OpenStack example)
func randString(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

var _ = Describe("CSI sanity test", Ordered, func() {
	Context("Base config", func() {
		var (
			driver         *Driver
			opts           *DriverOpts
			iaasClient     *stackit.MockIaasClient
			mountMock      *mount.MockIMount
			metadataMock   *metadata.MockIMetadata
			FakeEndpoint   string
			FakeCluster    = "cluster"
			FakeInstanceID = "321a8b81-3660-43e5-bab8-6470b65ee4e8"
			FakeDevicePath = "/dev/xxx"
			Socket         string
		)

		Socket = path.Join(os.TempDir(), "csi.sock")
		FakeEndpoint = "unix://" + Socket

		BeforeEach(func() {
			ctrl := gomock.NewController(GinkgoT())

			opts = &DriverOpts{
				ClusterID: FakeCluster,
				Endpoint:  FakeEndpoint,
			}
			driver = NewDriver(opts)
			driver.AddNodeServiceCapabilities(
				[]csi.NodeServiceCapability_RPC_Type{
					csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
				})

			// --- Initialize Mocks ---
			iaasClient = stackit.NewMockIaasClient(ctrl)
			mountMock = mount.NewMockIMount(ctrl)
			metadataMock = metadata.NewMockIMetadata(ctrl)

			// --- Mock State ---
			createdVolumes := make(map[string]*iaas.Volume)
			createdSnapshots := make(map[string]*iaas.Snapshot)
			createdBackups := make(map[string]*iaas.Backup)
			createdInstances := make(map[string]*iaas.Server)

			// --- Mock Mounter Setup ---
			mountPoints := make([]mountutils.MountPoint, 0)
			fakeMounter := mountutils.NewFakeMounter(mountPoints)
			fakeExec := &exec.FakeExec{DisableScripts: true}
			safeMounter := &mountutils.SafeFormatAndMount{
				Interface: fakeMounter,
				Exec:      fakeExec,
			}

			// --- 1. Mock IaaS Client (Volumes) ---

			iaasClient.EXPECT().CreateVolume(
				gomock.Any(), // context
				gomock.Any(), // create options
			).DoAndReturn(func(ctx context.Context, opts *iaas.CreateVolumePayload) (*iaas.Volume, error) {
				size := opts.Size
				if size == nil {
					size = ptr.To(int64(10)) // Default to 10GiB
				}
				newVol := &iaas.Volume{
					Id:               ptr.To("vol-" + randString(8)), // Create a random ID
					Name:             opts.Name,
					Size:             size,
					Status:           ptr.To(stackit.VolumeAvailableStatus),
					AvailabilityZone: opts.AvailabilityZone,
					Source:           opts.Source,
				}
				createdVolumes[*newVol.Id] = newVol // Store the pointer in the map
				return newVol, nil
			}).AnyTimes()

			iaasClient.EXPECT().GetVolume(
				gomock.Any(), // context
				gomock.Any(), // volumeID
			).DoAndReturn(func(ctx context.Context, volumeID string) (*iaas.Volume, error) {
				vol, ok := createdVolumes[volumeID]
				if !ok {
					return nil, &oapierror.GenericOpenAPIError{StatusCode: http.StatusNotFound}
				}
				return vol, nil
			}).AnyTimes()

			iaasClient.EXPECT().GetVolumesByName(
				gomock.Any(), // context
				gomock.Any(), // volName (string)
			).DoAndReturn(func(ctx context.Context, name string) ([]iaas.Volume, error) {
				var found []iaas.Volume
				for _, vol := range createdVolumes {
					if vol.Name != nil && *vol.Name == name {
						found = append(found, *vol) // Append the value
					}
				}
				return found, nil
			}).AnyTimes()

			iaasClient.EXPECT().ListVolumes(
				gomock.Any(), gomock.Any(), gomock.Eq("invalid-token"),
			).Return(nil, "", status.Error(codes.InvalidArgument, "invalid starting token")).AnyTimes()

			iaasClient.EXPECT().ListVolumes(
				gomock.Any(), gomock.Any(), gomock.Eq(""),
			).DoAndReturn(func(ctx context.Context, maxEntries int, token string) ([]iaas.Volume, string, error) {
				var volList []iaas.Volume
				for _, vol := range createdVolumes {
					volList = append(volList, *vol) // Append the value
				}
				return volList, "", nil
			}).AnyTimes()

			iaasClient.EXPECT().DeleteVolume(
				gomock.Any(), // context
				gomock.Any(), // volume ID
			).DoAndReturn(func(ctx context.Context, volID string) error {
				delete(createdVolumes, volID)
				return nil
			}).AnyTimes()

			iaasClient.EXPECT().WaitVolumeTargetStatusWithCustomBackoff(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			).Return(nil).AnyTimes()

			iaasClient.EXPECT().ExpandVolume(
				gomock.Any(), // context
				gomock.Any(), // volumeID
				gomock.Any(), // status
				gomock.Any(), // size
			).Return(nil).AnyTimes()

			iaasClient.EXPECT().WaitVolumeTargetStatus(
				gomock.Any(), // context
				gomock.Any(), // volumeID
				gomock.Any(), // tStatus
			).Return(nil).AnyTimes()

			// --- 2. Mock IaaS Client (Snapshots) ---

			iaasClient.EXPECT().CreateSnapshot(
				gomock.Any(), // context
				gomock.Any(), // name
				gomock.Any(), // volID
				gomock.Any(), // tags
			).DoAndReturn(func(ctx context.Context, name string, volID string, tags map[string]string) (*iaas.Snapshot, error) {
				newSnap := &iaas.Snapshot{
					Id:        ptr.To("snap-" + randString(8)),
					Name:      ptr.To(name),
					Status:    ptr.To(string(stackit.SnapshotReadyStatus)),
					CreatedAt: ptr.To(time.Now()),
					Size:      ptr.To(int64(10)), // 10 GiB
					VolumeId:  ptr.To(volID),
				}
				createdSnapshots[*newSnap.Id] = newSnap
				return newSnap, nil
			}).AnyTimes()

			iaasClient.EXPECT().GetSnapshotByID(
				gomock.Any(), // context
				gomock.Any(), // snapshotID
			).DoAndReturn(func(ctx context.Context, snapshotID string) (*iaas.Snapshot, error) {
				snap, ok := createdSnapshots[snapshotID]
				if !ok {
					return nil, &oapierror.GenericOpenAPIError{StatusCode: http.StatusNotFound}
				}
				return snap, nil
			}).AnyTimes()

			iaasClient.EXPECT().ListSnapshots(
				gomock.Any(), // context
				gomock.Any(), // filters
			).DoAndReturn(func(ctx context.Context, filters map[string]string) ([]iaas.Snapshot, string, error) {
				var snaplist []iaas.Snapshot
				startingToken := filters["Marker"]
				limitfilter := filters["Limit"]
				limit, _ := strconv.Atoi(limitfilter)
				name := filters["Name"]
				volumeID := filters["VolumeID"]

				for _, value := range createdSnapshots {
					if volumeID != "" {
						if value.VolumeId != nil && *value.VolumeId == volumeID {
							snaplist = append(snaplist, *value)
							break
						}
					} else if name != "" {
						if value.Name != nil && *value.Name == name {
							snaplist = append(snaplist, *value)
							break
						}
					} else {
						snaplist = append(snaplist, *value)
					}
				}

				if startingToken != "" {
					t, _ := strconv.Atoi(startingToken)
					if t >= 0 && t < len(snaplist) {
						snaplist = snaplist[t:]
					} else if t >= len(snaplist) {
						snaplist = []iaas.Snapshot{}
					}
				}

				retToken := ""
				if limit != 0 {
					if limit > 0 && limit <= len(snaplist) {
						snaplist = snaplist[:limit]
					}
					retToken = limitfilter
				}
				return snaplist, retToken, nil
			}).AnyTimes()

			iaasClient.EXPECT().DeleteSnapshot(
				gomock.Any(), // context
				gomock.Any(), // snapshotID
			).DoAndReturn(func(ctx context.Context, snapshotID string) error {
				delete(createdSnapshots, snapshotID)
				return nil
			}).AnyTimes()

			iaasClient.EXPECT().WaitSnapshotReady(
				gomock.Any(), // context
				gomock.Any(), // snapshotID
			).Return(
				ptr.To(string(stackit.SnapshotReadyStatus)),
				nil,
			).AnyTimes()

			// --- 3. Mock IaaS Client (Backups) ---

			iaasClient.EXPECT().CreateBackup(
				gomock.Any(), // context
				gomock.Any(), // name
				gomock.Any(), // volID
				gomock.Any(), // snapshotID
				gomock.Any(), // tags
			).DoAndReturn(func(ctx context.Context, name, volID, snapshotID string, tags map[string]string) (*iaas.Backup, error) {
				newBackup := &iaas.Backup{
					Id:         ptr.To("backup-" + randString(8)),
					Name:       ptr.To(name),
					Status:     ptr.To("available"),
					VolumeId:   ptr.To(volID),
					SnapshotId: ptr.To(snapshotID),
					CreatedAt:  ptr.To(time.Now()),
				}
				createdBackups[*newBackup.Id] = newBackup
				return newBackup, nil
			}).AnyTimes()

			iaasClient.EXPECT().GetBackupByID(
				gomock.Any(), // context
				gomock.Any(), // backupID
			).DoAndReturn(func(ctx context.Context, backupID string) (*iaas.Backup, error) {
				backup, ok := createdBackups[backupID]
				if !ok {
					return nil, &oapierror.GenericOpenAPIError{StatusCode: http.StatusNotFound}
				}
				return backup, nil
			}).AnyTimes()

			iaasClient.EXPECT().ListBackups(
				gomock.Any(), // context
				gomock.Any(), // filters
			).DoAndReturn(func(ctx context.Context, filters map[string]string) ([]iaas.Backup, error) {
				var backupList []iaas.Backup
				for _, backup := range createdBackups {
					backupList = append(backupList, *backup)
				}
				return backupList, nil
			}).AnyTimes()

			iaasClient.EXPECT().DeleteBackup(
				gomock.Any(), // context
				gomock.Any(), // backupID
			).DoAndReturn(func(ctx context.Context, backupID string) error {
				delete(createdBackups, backupID)
				return nil
			}).AnyTimes()

			// --- 4. Mock IaaS Client (Instances & Attach/Detach) ---

			iaasClient.EXPECT().GetInstanceByID(
				gomock.Any(), // context
				gomock.Any(), // instanceID
			).DoAndReturn(func(ctx context.Context, instanceID string) (*iaas.Server, error) {
				if _, ok := createdInstances[FakeInstanceID]; !ok {
					createdInstances[FakeInstanceID] = &iaas.Server{}
				}
				server, ok := createdInstances[instanceID]
				if !ok {
					return nil, status.Error(codes.NotFound, "server not found in mock")
				}
				return server, nil
			}).AnyTimes()

			iaasClient.EXPECT().AttachVolume(
				gomock.Any(), // context
				gomock.Any(), // instanceID
				gomock.Any(), // volumeID
			).DoAndReturn(func(ctx context.Context, instanceID string, volumeID string) (string, error) {
				vol, ok := createdVolumes[volumeID]
				if !ok {
					return "", status.Error(codes.NotFound, "volume not found in mock")
				}
				vol.ServerId = ptr.To(instanceID)
				vol.Status = ptr.To("attached")
				return *vol.Id, nil
			}).AnyTimes()

			iaasClient.EXPECT().WaitDiskAttached(
				gomock.Any(), // context
				gomock.Any(), // instanceID
				gomock.Any(), // volumeID
			).Return(nil).AnyTimes()

			iaasClient.EXPECT().DetachVolume(
				gomock.Any(), // context
				gomock.Any(), // instanceID
				gomock.Any(), // volumeID
			).Return(nil).AnyTimes()

			iaasClient.EXPECT().WaitDiskDetached(
				gomock.Any(), // context
				gomock.Any(), // instanceID
				gomock.Any(), // volumeID
			).Return(nil).AnyTimes()

			// --- 5. Mock Metadata Service ---

			metadataMock.EXPECT().GetInstanceID(
				gomock.Any(), // context
			).Return(
				FakeInstanceID, // A fake node ID for the NodeGetInfo test
				nil,            // no error
			).AnyTimes()

			metadataMock.EXPECT().GetFlavor(
				gomock.Any(), // context
			).Return(
				"mock-flavor", // A fake flavor name
				nil,           // no error
			).AnyTimes()

			metadataMock.EXPECT().GetAvailabilityZone(
				gomock.Any(), // context
			).Return(
				"eu01", // A fake availability zone
				nil,    // no error
			).AnyTimes()

			// --- 6. Mock Mount Utilities ---

			mountMock.EXPECT().UnmountPath(
				gomock.Any(), // mountPath
			).DoAndReturn(func(mountPath string) error {
				return os.RemoveAll(mountPath)
			}).AnyTimes()

			mountMock.EXPECT().MakeDir(
				gomock.Any(), // pathname
			).Return(nil).AnyTimes()

			mountMock.EXPECT().MakeFile(
				gomock.Any(), // pathname
			).Return(nil).AnyTimes()

			mountMock.EXPECT().GetDevicePath(
				gomock.Any(), // volumeID
			).Return(FakeDevicePath, nil).AnyTimes()

			mountMock.EXPECT().GetDeviceStats(
				gomock.Any(), // path
			).DoAndReturn(func(path string) (*mount.DeviceStats, error) {
				return &mount.DeviceStats{
					Block:      true,
					TotalBytes: 1000,
				}, nil
			}).AnyTimes()

			mountMock.EXPECT().GetMountFs(
				gomock.Any(), //volumePath
			).DoAndReturn(func(volumePath string) ([]byte, error) {
				args := []string{"-o", "source", "--first-only", "--noheadings", "--target", volumePath}
				return safeMounter.Exec.Command("findmnt", args...).CombinedOutput()
			}).AnyTimes()

			mountMock.EXPECT().IsLikelyNotMountPointAttach(
				gomock.Any(), // targetpath
			).DoAndReturn(func(mountPath string) (bool, error) {
				// This complex mock is needed for the sanity test.
				// It checks if the path exists, and if not, *creates it*
				// to simulate what NodeStageVolume is expected to do.
				notMnt, err := safeMounter.IsLikelyNotMountPoint(mountPath)
				if err != nil {
					if os.IsNotExist(err) {
						// Create the directory on the real filesystem
						if errMkdir := os.MkdirAll(mountPath, 0750); errMkdir != nil {
							return false, errMkdir
						}
						// Successfully created the dir, so it's not a mount point
						return true, nil
					}
					// It was some other error
					return false, err
				}
				// Path existed, return its original status
				return notMnt, nil
			}).AnyTimes()

			mountMock.EXPECT().Mounter().Return(safeMounter).AnyTimes()

			// --- Driver Setup & Run ---
			driver.SetupControllerService(iaasClient)
			driver.SetupNodeService(mountMock, metadataMock, stackit.BlockStorageOpts{})

			go func() {
				defer GinkgoRecover()
				driver.Run()
			}()
		})

		AfterEach(func() {
			os.Remove(Socket)
		})

		Describe("CSI sanity", func() {
			config := sanity.NewTestConfig()
			config.Address = FakeEndpoint

			sanity.GinkgoTest(&config)
		})

	})
})
