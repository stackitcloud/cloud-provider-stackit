package stackit

import (
	"context"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	"go.uber.org/mock/gomock"

	mock "github.com/stackitcloud/cloud-provider-stackit/pkg/mock/iaas"
)

var _ = Describe("Backup", func() {
	var (
		err       error
		mockCtrl  *gomock.Controller
		mockAPI   *mock.MockDefaultAPI
		openStack IaasClient
		config    *stackitconfig.CSIConfig
	)

	const projectID = "project-id"
	const region = "eu01"

	BeforeEach(func() {
		t := GinkgoT()
		mockCtrl = gomock.NewController(t)
		mockAPI = mock.NewMockDefaultAPI(mockCtrl)
		t.Setenv("STACKIT_REGION", region)
		Expect(os.Getenv("STACKIT_REGION")).To(Equal(region))
	})

	Context("buildCreateBackupPayload", func() {
		DescribeTable("successful payload variants",
			func(
				name, volID, snapshotID string,
				tags map[string]string,
				expectedPayload iaas.CreateBackupPayload,
			) {
				actualPayload, err := buildCreateBackupPayload(name, volID, snapshotID, tags)
				Expect(err).ToNot(HaveOccurred())
				Expect(actualPayload).To(Equal(expectedPayload))
			},
			Entry(
				"with volume source and nil tags",
				"expected-name", "volume-id", "", nil,
				iaas.CreateBackupPayload{
					Name:        new("expected-name"),
					Description: new(backupDescription),
					Source: iaas.BackupSource{
						Type: "volume",
						Id:   "volume-id",
					},
					Labels: nil,
				},
			),
			Entry(
				"with snapshot source and special characters in tags",
				"expected-name", "", "snapshot-id",
				map[string]string{
					"special": "tag with spaces and !@#$%^&*()",
					"normal":  "value",
				},
				iaas.CreateBackupPayload{
					Name:        new("expected-name"),
					Description: new(backupDescription),
					Source: iaas.BackupSource{
						Type: "snapshot",
						Id:   "snapshot-id",
					},
					Labels: map[string]any{
						"special": "tag with spaces and !@#$%^&*()",
						"normal":  "value",
					},
				},
			),
			Entry(
				"with empty tags map",
				"expected-name", "volume-id", "", map[string]string{},
				iaas.CreateBackupPayload{
					Name:        new("expected-name"),
					Description: new(backupDescription),
					Source: iaas.BackupSource{
						Type: "volume",
						Id:   "volume-id",
					},
					Labels: map[string]any{},
				},
			),
			Entry(
				"with long backup name",
				"very-long-backup-name-"+string(make([]byte, 200)), "volume-id", "", nil,
				iaas.CreateBackupPayload{
					Name:        new("very-long-backup-name-" + string(make([]byte, 200))),
					Description: new(backupDescription),
					Source: iaas.BackupSource{
						Type: "volume",
						Id:   "volume-id",
					},
				},
			),
			Entry(
				"when both volume and snapshot are provided snapshot wins",
				"expected-name", "volume-id", "snapshot-id", nil,
				iaas.CreateBackupPayload{
					Name:        new("expected-name"),
					Description: new(backupDescription),
					Source: iaas.BackupSource{
						Type: "snapshot",
						Id:   "snapshot-id",
					},
				},
			),
		)

		DescribeTable("validation error variants",
			func(name, volID, snapshotID, expectedError string) {
				actualPayload, err := buildCreateBackupPayload(name, volID, snapshotID, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(expectedError))
				Expect(actualPayload).To(Equal(iaas.CreateBackupPayload{}))
			},
			Entry("empty name", "", "volume-id", "", "backup name cannot be empty"),
			Entry("missing volume and snapshot IDs", "expected-name", "", "", "either volID or snapshotID must be provided"),
		)
	})

	Context("CreateBackup", func() {
		BeforeEach(func() {
			config = &stackitconfig.CSIConfig{
				Global: stackitconfig.GlobalOpts{
					ProjectID: projectID,
				},
			}
			openStack, err = CreateSTACKITProvider(mockAPI, config)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns backup on successful API call and uses configured project and region", func() {
			mockCreateBackup(mockAPI)

			backup, err := openStack.CreateBackup(context.Background(), "expected-name", "volume-id", "", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(backup).ToNot(BeNil())
			Expect(backup.Id).ToNot(BeNil())
			Expect(*backup.Id).To(Equal("expected backup"))
		})

		It("returns error when API fails", func() {
			mockAPI.EXPECT().CreateBackup(gomock.Any(), projectID, region).Return(iaas.ApiCreateBackupRequest{ApiService: mockAPI})
			mockAPI.EXPECT().CreateBackupExecute(gomock.Any()).Return(nil, fmt.Errorf("API error"))

			backup, err := openStack.CreateBackup(context.Background(), "expected-name", "volume-id", "", nil)
			Expect(err).To(HaveOccurred())
			Expect(backup).To(BeNil())
		})

		DescribeTable("returns error when payload is invalid",
			func(name, volID, snapshotID, expectedError string) {
				mockAPI.EXPECT().CreateBackup(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockAPI.EXPECT().CreateBackupExecute(gomock.Any()).Times(0)

				backup, err := openStack.CreateBackup(context.Background(), name, volID, snapshotID, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(expectedError))
				Expect(backup).To(BeNil())
			},
			Entry("empty name", "", "volume-id", "", "backup name cannot be empty"),
			Entry("missing volume and snapshot IDs", "expected-name", "", "", "either volID or snapshotID must be provided"),
		)
	})

})

func mockCreateBackup(mockAPI *mock.MockDefaultAPI) {
	const (
		projectID = "project-id"
		region    = "eu01"
	)

	mockAPI.EXPECT().CreateBackup(gomock.Any(), projectID, region).Return(iaas.ApiCreateBackupRequest{ApiService: mockAPI})
	mockAPI.EXPECT().CreateBackupExecute(gomock.Any()).Return(&iaas.Backup{Id: new("expected backup")}, nil)
}
