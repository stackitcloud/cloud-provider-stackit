package client

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	"go.uber.org/mock/gomock"

	mock "github.com/stackitcloud/cloud-provider-stackit/pkg/mock/iaas"
)

var _ = Describe("Server", func() {
	var (
		mockCtrl       *gomock.Controller
		mockIaaSClient *mock.MockDefaultAPI
		client         *iaasClient
	)

	const (
		serverID = "server-uuid-123"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockIaaSClient = mock.NewMockDefaultAPI(mockCtrl)

		client = &iaasClient{
			Client: mockIaaSClient,
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("GetServer", func() {
		It("returns a server on success", func() {
			mockIaaSClient.EXPECT().
				GetServer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Server{Id: new(serverID), Name: "my-server"}, nil)

			server, err := client.GetServer(context.Background(), "my-server")
			Expect(err).ToNot(HaveOccurred())
			Expect(*server.Id).To(Equal(serverID))
		})

		It("returns ErrorNotFound when API returns 404", func() {
			mockIaaSClient.EXPECT().
				GetServer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil, ErrorNotFound)

			_, err := client.GetServer(context.Background(), "my-server")
			Expect(err).To(HaveOccurred())
		})
	})

	Context("CreateServer", func() {
		It("successfully creates a server", func() {
			mockIaaSClient.EXPECT().
				CreateServer(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(iaas.Server{
					Id: new(serverID), Name: "new-server"}, nil)
			payload := &iaas.CreateServerPayload{Name: "new-server"}

			server, err := client.CreateServer(context.Background(), payload)
			Expect(err).ToNot(HaveOccurred())
			Expect(*server.Id).To(Equal(serverID))
		})
	})

	Context("DeleteServer", func() {
		It("deletes the server successfully", func() {
			mockIaaSClient.EXPECT().
				DeleteServer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil)

			err := client.DeleteServer(context.Background(), serverID)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("ListServers", func() {
		It("returns a list of servers with details", func() {
			mockItems := []iaas.Server{
				{Id: new("id-1"), Name: "server-1"},
			}

			mockIaaSClient.EXPECT().
				ListServers(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.ServerListResponse{Items: mockItems}, nil)

			resp, err := client.ListServers(context.Background())
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
				UpdateServer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Server{Id: new(serverID), Name: "updated-name"}, nil)

			updatePayload := iaas.UpdateServerPayload{Name: new("updated-name")}
			server, err := client.UpdateServer(context.Background(), serverID, updatePayload)
			Expect(err).ToNot(HaveOccurred())
			Expect(server.Name).To(Equal("updated-name"))
		})
	})
})

var _ = Describe("Snapshot", func() {
	var (
		mockCtrl       *gomock.Controller
		mockIaaSClient *mock.MockDefaultAPI
		client         *iaasClient
	)

	const (
		snapshotID = "snapshot-uuid-123"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockIaaSClient = mock.NewMockDefaultAPI(mockCtrl)

		client = &iaasClient{
			Client: mockIaaSClient,
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("CreateSnapshot", func() {
		It("successfully creates a snapshot", func() {
			mockIaaSClient.EXPECT().
				CreateSnapshot(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Snapshot{Id: new(snapshotID)}, nil)

			payload := &iaas.CreateSnapshotPayload{Name: new("new-snapshot")}
			snapshot, err := client.CreateSnapshot(context.Background(), payload)
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
				ListSnapshotsInProject(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.SnapshotListResponse{Items: mockItems}, nil)

			resp, err := client.ListSnapshots(context.Background(), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(HaveLen(2))
		})
	})

	Context("GetSnapshot", func() {
		It("returns a specific snapshot on success", func() {
			mockIaaSClient.EXPECT().
				GetSnapshot(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Snapshot{Id: new(snapshotID), Status: new("AVAILABLE")}, nil)

			snapshot, err := client.GetSnapshot(context.Background(), snapshotID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*snapshot.Id).To(Equal(snapshotID))
		})
	})

	Context("DeleteSnapshot", func() {
		It("deletes the snapshot successfully", func() {
			mockIaaSClient.EXPECT().
				DeleteSnapshot(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil)

			err := client.DeleteSnapshot(context.Background(), snapshotID)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("WaitSnapshotReady", func() {
		It("returns the current status of the snapshot", func() {
			mockIaaSClient.EXPECT().
				GetSnapshot(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Snapshot{Id: new(snapshotID), Status: new("READY")}, nil)

			status, err := client.WaitSnapshotReady(context.Background(), snapshotID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*status).To(Equal("READY"))
		})

		It("returns an error if the snapshot retrieval fails", func() {
			mockIaaSClient.EXPECT().
				GetSnapshot(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil, fmt.Errorf("api error"))

			status, err := client.WaitSnapshotReady(context.Background(), snapshotID)
			Expect(err).To(HaveOccurred())
			Expect(status).To(BeNil())
		})
	})
})

var _ = Describe("Backup", func() {
	var (
		mockCtrl       *gomock.Controller
		mockIaaSClient *mock.MockDefaultAPI
		client         *iaasClient
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockIaaSClient = mock.NewMockDefaultAPI(mockCtrl)

		client = &iaasClient{
			Client: mockIaaSClient,
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("buildCreateBackupPayload", func() {
		DescribeTable("successful payload variants",
			func(name, volID, snapshotID string, tags map[string]string, expectedPayload iaas.CreateBackupPayload) {
				actualPayload, err := BuildCreateBackupPayload(name, volID, snapshotID, tags)
				Expect(err).ToNot(HaveOccurred())
				Expect(actualPayload).To(Equal(expectedPayload))
			},
			Entry("with volume source", "expected-name", "volume-id", "", nil, iaas.CreateBackupPayload{
				Name:        new("expected-name"),
				Description: new(BackupDescription),
				Source:      iaas.BackupSource{Type: "volume", Id: "volume-id"},
			}),
			Entry("with snapshot source", "expected-name", "", "snapshot-id", nil, iaas.CreateBackupPayload{
				Name:        new("expected-name"),
				Description: new(BackupDescription),
				Source:      iaas.BackupSource{Type: "snapshot", Id: "snapshot-id"},
			}),
		)
	})

	Context("CreateBackup API Calls", func() {
		It("returns backup on successful API call", func() {
			mockIaaSClient.EXPECT().
				CreateBackup(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Backup{Id: new("expected-backup-id")}, nil)

			backup, err := client.CreateBackup(context.Background(), "expected-name", "volume-id", "", nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(*backup.Id).To(Equal("expected-backup-id"))
		})

		It("returns error when API fails", func() {
			mockIaaSClient.EXPECT().
				CreateBackup(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil, fmt.Errorf("API error"))

			_, err := client.CreateBackup(context.Background(), "expected-name", "volume-id", "", nil)
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
				ListBackups(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(mockBackups.Items, nil)

			backups, err := client.ListBackups(context.Background(), nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(backups).To(HaveLen(2))
			Expect(*backups[0].Id).To(Equal("id-1"))
		})

		It("returns error when list API fails", func() {
			mockIaaSClient.EXPECT().
				ListBackups(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil, fmt.Errorf("list error"))

			_, err := client.ListBackups(context.Background(), nil)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("GetBackup", func() {
		It("returns a specific backup", func() {
			mockIaaSClient.EXPECT().
				GetBackup(gomock.Any(), gomock.Any(), gomock.Any(), "backup-id").
				Return(&iaas.Backup{Id: new("backup-id")}, nil)

			backup, err := client.GetBackup(context.Background(), "backup-id")
			Expect(err).ToNot(HaveOccurred())
			Expect(*backup.Id).To(Equal("backup-id"))
		})
	})

	Context("DeleteBackup", func() {
		It("calls delete successfully", func() {
			mockIaaSClient.EXPECT().
				DeleteBackup(gomock.Any(), gomock.Any(), gomock.Any(), "backup-id").
				Return(nil)

			err := client.DeleteBackup(context.Background(), "backup-id")
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns error if delete fails", func() {
			mockIaaSClient.EXPECT().
				DeleteBackup(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(fmt.Errorf("delete failed"))

			err := client.DeleteBackup(context.Background(), "any-id")
			Expect(err).To(HaveOccurred())
		})
	})

	Context("WaitBackupReady", func() {
		It("returns the backup status when it becomes ready", func() {
			mockIaaSClient.EXPECT().
				GetBackup(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Backup{Id: new("backup-id"), Status: new("Ready")}, nil)

			status, err := client.WaitBackupReady(context.Background(), "backup-id", 10, 60)

			Expect(err).ToNot(HaveOccurred())
			Expect(*status).To(Equal("Ready"))
		})

		It("returns error on timeout or wait failure", func() {
			mockIaaSClient.EXPECT().
				GetBackup(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil, fmt.Errorf("timeout waiting for backup"))

			status, err := client.WaitBackupReady(context.Background(), "id", 1, 1)
			Expect(err).To(HaveOccurred())
			Expect(status).To(BeNil())
		})
	})
})

var _ = Describe("Volume", func() {
	var (
		mockCtrl       *gomock.Controller
		mockIaaSClient *mock.MockDefaultAPI
		client         *iaasClient
	)

	const (
		projectID = "project-id"
		region    = "eu01"
		volumeID  = "vol-123"
		serverID  = "server-123"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockIaaSClient = mock.NewMockDefaultAPI(mockCtrl)

		client = &iaasClient{
			Client: mockIaaSClient,
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("Volume Lifecycle", func() {
		It("CreateVolume successfully calls the API", func() {
			mockIaaSClient.EXPECT().
				CreateVolume(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Volume{Id: new(volumeID)}, nil)

			vol, err := client.CreateVolume(context.Background(), &iaas.CreateVolumePayload{})
			Expect(err).ToNot(HaveOccurred())
			Expect(*vol.Id).To(Equal(volumeID))
		})

		It("GetVolume returns a specific volume", func() {
			mockIaaSClient.EXPECT().
				GetVolume(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Volume{Id: new(volumeID), Name: new("test-vol")}, nil)

			vol, err := client.GetVolume(context.Background(), volumeID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*vol.Name).To(Equal("test-vol"))
		})

		It("DeleteVolume fails if volume is still attached (diskIsUsed logic)", func() {
			mockIaaSClient.EXPECT().
				GetVolume(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Volume{Id: new(volumeID), ServerId: new(serverID)}, nil)

			err := client.DeleteVolume(context.Background(), volumeID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("still attached"))
		})
	})

	Context("Attach/Detach Volume", func() {
		It("AttachVolume calls API when not already attached", func() {
			mockIaaSClient.EXPECT().GetVolume(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Volume{Id: new(volumeID), ServerId: nil}, nil)

			id, err := client.AttachVolume(context.Background(), serverID, volumeID, iaas.AddVolumeToServerPayload{})
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal(volumeID))
		})

		It("DetachVolume fails if status is not Available", func() {
			mockIaaSClient.EXPECT().GetVolume(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&iaas.Volume{Id: new(volumeID), Status: new("CREATING")}, nil)

			err := client.DetachVolume(context.Background(), serverID, volumeID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("its status is CREATING"))
		})
	})

	Context("ExpandVolume", func() {
		It("successfully resizes an Available volume", func() {
			mockIaaSClient.EXPECT().
				ResizeVolume(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil)

			err := client.ExpandVolume(context.Background(), volumeID, "available", iaas.ResizeVolumePayload{})
			Expect(err).ToNot(HaveOccurred())
		})

		It("errors when volume is in a bad state for resize", func() {
			mockIaaSClient.EXPECT().
				ResizeVolume(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(fmt.Errorf("volume cannot be resized, when status is %s", "ERROR"))

			err := client.ExpandVolume(context.Background(), volumeID, "ERROR", iaas.ResizeVolumePayload{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot be resized"))
		})
	})

	Context("Waiting Logic", func() {
		It("WaitVolumeTargetStatus returns nil when target status is reached", func() {
			// Mocking the behavior of WaitVolumeTargetStatus
			mockIaaSClient.EXPECT().
				GetVolume(gomock.Any(), volumeID, []string{"available"}, gomock.Any()).
				Return(nil)

			err := client.WaitVolumeTargetStatus(context.Background(), volumeID, []string{"available"})
			Expect(err).ToNot(HaveOccurred())
		})

		It("WaitDiskAttached returns error on timeout", func() {
			mockIaaSClient.EXPECT().
				GetVolume(gomock.Any(), serverID, volumeID, gomock.Any()).
				Return(fmt.Errorf("timeout"))

			err := client.WaitDiskAttached(context.Background(), serverID, volumeID)
			Expect(err).To(HaveOccurred())
		})
	})
})
