package client_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	"go.uber.org/mock/gomock"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/client"
	mock "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/client/mock"
)

var _ = Describe("Server", func() {
	var (
		mockCtrl       *gomock.Controller
		mockIaaSClient *mock.MockIaaSClient
	)

	const (
		serverID = "server-uuid-123"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockIaaSClient = mock.NewMockIaaSClient(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("GetServer", func() {
		It("returns a server on success", func() {
			mockIaaSClient.EXPECT().
				GetServer(gomock.Any(), gomock.Any()).
				Return(&iaas.Server{Id: new(serverID), Name: "my-server"}, nil)

			server, err := mockIaaSClient.GetServer(context.Background(), "my-server")
			Expect(err).ToNot(HaveOccurred())
			Expect(*server.Id).To(Equal(serverID))
		})

		It("returns ErrorNotFound when API returns 404", func() {
			mockIaaSClient.EXPECT().
				GetServer(gomock.Any(), gomock.Any()).
				Return(nil, client.ErrorNotFound)

			_, err := mockIaaSClient.GetServer(context.Background(), "my-server")
			Expect(err).To(HaveOccurred())
		})
	})

	Context("CreateServer", func() {
		It("successfully creates a server", func() {
			mockIaaSClient.EXPECT().
				CreateServer(gomock.Any(), gomock.Any()).
				Return(iaas.Server{
					Id: new(serverID), Name: "new-server"}, nil)
			payload := iaas.CreateServerPayload{Name: "new-server"}

			server, err := mockIaaSClient.CreateServer(context.Background(), payload)
			Expect(err).ToNot(HaveOccurred())
			Expect(*server.Id).To(Equal(serverID))
		})
	})

	Context("DeleteServer", func() {
		It("deletes the server successfully", func() {
			mockIaaSClient.EXPECT().
				DeleteServer(gomock.Any(), gomock.Any()).
				Return(nil)

			err := mockIaaSClient.DeleteServer(context.Background(), serverID)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("ListServers", func() {
		It("returns a list of servers with details", func() {
			mockItems := []iaas.Server{
				{Id: new("id-1"), Name: "server-1"},
			}

			mockIaaSClient.EXPECT().
				ListServers(gomock.Any()).
				Return(&iaas.ServerListResponse{Items: mockItems}, nil)

			resp, err := mockIaaSClient.ListServers(context.Background())
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(HaveLen(1))
			items := *resp
			Expect(items).To(HaveLen(1))
			Expect(*items[0].Id).To(Equal("id-1"))
		})
	})

	Context("UpdateServer", func() {
		It("updates server properties", func() {
			mockIaaSClient.EXPECT().
				UpdateServer(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Server{Id: new(serverID), Name: "updated-name"}, nil)

			updatePayload := iaas.UpdateServerPayload{Name: new("updated-name")}
			server, err := mockIaaSClient.UpdateServer(context.Background(), serverID, updatePayload)
			Expect(err).ToNot(HaveOccurred())
			Expect(server.Name).To(Equal("updated-name"))
		})
	})
})

var _ = Describe("Snapshot", func() {
	var (
		mockCtrl       *gomock.Controller
		mockIaaSClient *mock.MockIaaSClient
	)

	const (
		snapshotID = "snapshot-uuid-123"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockIaaSClient = mock.NewMockIaaSClient(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("CreateSnapshot", func() {
		It("successfully creates a snapshot", func() {
			mockIaaSClient.EXPECT().
				CreateSnapshot(gomock.Any(), gomock.Any()).
				Return(&iaas.Snapshot{Id: new(snapshotID)}, nil)

			payload := iaas.CreateSnapshotPayload{Name: new("new-snapshot")}
			snapshot, err := mockIaaSClient.CreateSnapshot(context.Background(), payload)
			Expect(err).ToNot(HaveOccurred())
			Expect(*snapshot.Id).To(Equal(snapshotID))
		})
	})

	Context("ListSnapshots", func() {
		It("returns a filtered list of snapshots", func() {
			mockItems := []iaas.Snapshot{
				{Id: new("id-1"), Name: new("snap-1")},
				{Id: new("id-2"), Name: new("snap-2")},
			}

			mockIaaSClient.EXPECT().
				ListSnapshots(gomock.Any(), gomock.Any()).
				Return(&iaas.SnapshotListResponse{Items: mockItems}, nil)

			resp, err := mockIaaSClient.ListSnapshots(context.Background(), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(HaveLen(2))
		})
	})

	Context("GetSnapshot", func() {
		It("returns a specific snapshot on success", func() {
			mockIaaSClient.EXPECT().
				GetSnapshot(gomock.Any(), gomock.Any()).
				Return(&iaas.Snapshot{Id: new(snapshotID), Status: new("AVAILABLE")}, nil)

			snapshot, err := mockIaaSClient.GetSnapshot(context.Background(), snapshotID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*snapshot.Id).To(Equal(snapshotID))
		})
	})

	Context("DeleteSnapshot", func() {
		It("deletes the snapshot successfully", func() {
			mockIaaSClient.EXPECT().
				DeleteSnapshot(gomock.Any(), gomock.Any()).
				Return(nil)

			err := mockIaaSClient.DeleteSnapshot(context.Background(), snapshotID)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("WaitSnapshotReady", func() {
		It("returns the current status of the snapshot", func() {
			// WaitSnapshotReady internally calls GetSnapshot
			mockIaaSClient.EXPECT().
				WaitSnapshotReady(gomock.Any(), snapshotID).
				Return(new("READY"), nil)

			status, err := mockIaaSClient.WaitSnapshotReady(context.Background(), snapshotID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*status).To(Equal("READY"))
		})

		It("returns an error if the snapshot retrieval fails", func() {
			mockIaaSClient.EXPECT().
				WaitSnapshotReady(gomock.Any(), snapshotID).
				Return(nil, fmt.Errorf("api error"))

			status, err := mockIaaSClient.WaitSnapshotReady(context.Background(), snapshotID)
			Expect(err).To(HaveOccurred())
			Expect(status).To(BeNil())
		})
	})
})

var _ = Describe("Backup", func() {
	var (
		mockCtrl       *gomock.Controller
		mockIaaSClient *mock.MockIaaSClient
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockIaaSClient = mock.NewMockIaaSClient(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("buildCreateBackupPayload", func() {
		DescribeTable("successful payload variants",
			func(name, volID, snapshotID string, tags map[string]string, expectedPayload iaas.CreateBackupPayload) {
				actualPayload, err := client.BuildCreateBackupPayload(name, volID, snapshotID, tags)
				Expect(err).ToNot(HaveOccurred())
				Expect(actualPayload).To(Equal(expectedPayload))
			},
			Entry("with volume source", "expected-name", "volume-id", "", nil, iaas.CreateBackupPayload{
				Name:        new("expected-name"),
				Description: new(client.BackupDescription),
				Source:      iaas.BackupSource{Type: "volume", Id: "volume-id"},
			}),
			Entry("with snapshot source", "expected-name", "", "snapshot-id", nil, iaas.CreateBackupPayload{
				Name:        new("expected-name"),
				Description: new(client.BackupDescription),
				Source:      iaas.BackupSource{Type: "snapshot", Id: "snapshot-id"},
			}),
		)
	})

	Context("CreateBackup API Calls", func() {
		It("returns backup on successful API call", func() {
			mockIaaSClient.EXPECT().
				CreateBackup(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Backup{Id: new("expected-backup-id")}, nil)

			backup, err := mockIaaSClient.CreateBackup(context.Background(), "expected-name", "volume-id", "", nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(*backup.Id).To(Equal("expected-backup-id"))
		})

		It("returns error when API fails", func() {
			mockIaaSClient.EXPECT().
				CreateBackup(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil, fmt.Errorf("API error"))

			_, err := mockIaaSClient.CreateBackup(context.Background(), "expected-name", "volume-id", "", nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("API error"))
		})
	})

	Context("ListBackups", func() {
		It("returns a filtered list of backups on success", func() {
			mockBackups := &iaas.BackupListResponse{
				Items: []iaas.Backup{
					{Id: new("id-1"), Name: new("backup-1")},
					{Id: new("id-2"), Name: new("backup-2")},
				},
			}

			mockIaaSClient.EXPECT().
				ListBackups(gomock.Any(), gomock.Any()).
				Return(mockBackups.Items, nil)

			backups, err := mockIaaSClient.ListBackups(context.Background(), nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(backups).To(HaveLen(2))
			Expect(*backups[0].Id).To(Equal("id-1"))
		})

		It("returns error when list API fails", func() {
			mockIaaSClient.EXPECT().
				ListBackups(gomock.Any(), gomock.Any()).
				Return(nil, fmt.Errorf("list error"))

			_, err := mockIaaSClient.ListBackups(context.Background(), nil)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("GetBackup", func() {
		It("returns a specific backup", func() {
			mockIaaSClient.EXPECT().
				GetBackup(gomock.Any(), "backup-id").
				Return(&iaas.Backup{Id: new("backup-id")}, nil)

			backup, err := mockIaaSClient.GetBackup(context.Background(), "backup-id")
			Expect(err).ToNot(HaveOccurred())
			Expect(*backup.Id).To(Equal("backup-id"))
		})
	})

	Context("DeleteBackup", func() {
		It("calls delete successfully", func() {
			mockIaaSClient.EXPECT().
				DeleteBackup(gomock.Any(), "backup-id").
				Return(nil)

			err := mockIaaSClient.DeleteBackup(context.Background(), "backup-id")
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns error if delete fails", func() {
			mockIaaSClient.EXPECT().
				DeleteBackup(gomock.Any(), gomock.Any()).
				Return(fmt.Errorf("delete failed"))

			err := mockIaaSClient.DeleteBackup(context.Background(), "any-id")
			Expect(err).To(HaveOccurred())
		})
	})

	Context("WaitBackupReady", func() {
		It("returns the backup status when it becomes ready", func() {
			// WaitBackupReady internally calls GetBackup to return the final status
			mockIaaSClient.EXPECT().
				WaitBackupReady(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(new("Ready"), nil)

			status, err := mockIaaSClient.WaitBackupReady(context.Background(), "backup-id", 10, 60)

			Expect(err).ToNot(HaveOccurred())
			Expect(*status).To(Equal("Ready"))
		})

		It("returns error on timeout or wait failure", func() {
			mockIaaSClient.EXPECT().
				WaitBackupReady(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil, fmt.Errorf("timeout waiting for backup"))

			status, err := mockIaaSClient.WaitBackupReady(context.Background(), "id", 1, 1)
			Expect(err).To(HaveOccurred())
			Expect(status).To(BeNil())
		})
	})
})

var _ = Describe("Volume", func() {
	var (
		mockCtrl       *gomock.Controller
		mockIaaSClient *mock.MockIaaSClient
	)

	const (
		projectID = "project-id"
		region    = "eu01"
		volumeID  = "vol-123"
		serverID  = "server-123"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockIaaSClient = mock.NewMockIaaSClient(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("Volume Lifecycle", func() {
		It("CreateVolume successfully calls the API", func() {
			mockIaaSClient.EXPECT().
				CreateVolume(gomock.Any(), gomock.Any()).
				Return(&iaas.Volume{Id: new(volumeID)}, nil)

			vol, err := mockIaaSClient.CreateVolume(context.Background(), iaas.CreateVolumePayload{})
			Expect(err).ToNot(HaveOccurred())
			Expect(*vol.Id).To(Equal(volumeID))
		})

		It("GetVolume returns a specific volume", func() {
			mockIaaSClient.EXPECT().
				GetVolume(gomock.Any(), gomock.Any()).
				Return(&iaas.Volume{Id: new(volumeID), Name: new("test-vol")}, nil)

			vol, err := mockIaaSClient.GetVolume(context.Background(), volumeID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*vol.Name).To(Equal("test-vol"))
		})

		It("DeleteVolume fails if volume is still attached (diskIsUsed logic)", func() {
			mockIaaSClient.EXPECT().
				GetVolume(gomock.Any(), gomock.Any()).
				Return(&iaas.Volume{Id: new(volumeID), ServerId: new(serverID)}, nil)

			err := mockIaaSClient.DeleteVolume(context.Background(), volumeID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("still attached"))
		})
	})

	Context("Attach/Detach Volume", func() {
		It("AttachVolume calls API when not already attached", func() {
			mockIaaSClient.EXPECT().GetVolume(gomock.Any(), gomock.Any()).
				Return(&iaas.Volume{Id: new(volumeID), ServerId: nil}, nil)

			mockIaaSClient.EXPECT().
				AttachVolume(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(new(volumeID), nil)

			id, err := mockIaaSClient.AttachVolume(context.Background(), serverID, volumeID, iaas.AddVolumeToServerPayload{})
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal(volumeID))
		})

		It("DetachVolume fails if status is not Available", func() {
			mockIaaSClient.EXPECT().GetVolume(gomock.Any(), gomock.Any()).
				Return(&iaas.Volume{Id: new(volumeID), Status: new("CREATING")}, nil)

			err := mockIaaSClient.DetachVolume(context.Background(), serverID, volumeID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("its status is CREATING"))
		})
	})

	Context("ExpandVolume", func() {
		It("successfully resizes an Available volume", func() {
			mockIaaSClient.EXPECT().
				ExpandVolume(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil)

			err := mockIaaSClient.ExpandVolume(context.Background(), volumeID, "available", iaas.ResizeVolumePayload{})
			Expect(err).ToNot(HaveOccurred())
		})

		It("errors when volume is in a bad state for resize", func() {
			err := mockIaaSClient.ExpandVolume(context.Background(), volumeID, "ERROR", iaas.ResizeVolumePayload{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot be resized"))
		})
	})

	Context("Waiting Logic", func() {
		It("WaitVolumeTargetStatus returns nil when target status is reached", func() {
			// Mocking the behavior of WaitVolumeTargetStatus
			mockIaaSClient.EXPECT().
				WaitVolumeTargetStatus(gomock.Any(), volumeID, []string{"available"}).
				Return(nil)

			err := mockIaaSClient.WaitVolumeTargetStatus(context.Background(), volumeID, []string{"available"})
			Expect(err).ToNot(HaveOccurred())
		})

		It("WaitDiskAttached returns error on timeout", func() {
			mockIaaSClient.EXPECT().
				WaitDiskAttached(gomock.Any(), serverID, volumeID).
				Return(fmt.Errorf("timeout"))

			err := mockIaaSClient.WaitDiskAttached(context.Background(), serverID, volumeID)
			Expect(err).To(HaveOccurred())
		})
	})
})
