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

		DescribeTable("successful call variant",
			func(
				volId, snapshotId string,
				tags map[string]string,
				expectedPayload iaas.CreateBackupPayload) {

				mockCreateBackup(
					mockCtrl,
					mockAPI,
					expectedPayload,
				)

				actualBackup, err := openStack.CreateBackup(context.Background(), "expected-name", volId, snapshotId, tags)
				Expect(err).ToNot(HaveOccurred())

				Expect(actualBackup).ToNot(BeNil())
				Expect(actualBackup.Id).ToNot(BeNil())
				Expect(*actualBackup.Id).To(Equal("expected backup"))
			},
			Entry(
				"with volume source",
				"volume-id", "",
				nil,
				iaas.CreateBackupPayload{
					Name: new("expected-name"),
					Source: iaas.BackupSource{
						Type: "volume",
						Id:   "volume-id",
					},
					Labels: nil,
				},
			),
			Entry(
				"with snapshot source",
				"", "snapshot-id",
				map[string]string{"tag1": "value1"},
				iaas.CreateBackupPayload{
					Name: new("expected-name"),
					Source: iaas.BackupSource{
						Type: "snapshot",
						Id:   "snapshot-id",
					},
					Labels: map[string]any{
						"tag1": "value1",
					},
				},
			),
		)
	})

	Context("CreateBackup error cases", func() {
		const projectID = "project-id"

		BeforeEach(func() {
			config = &stackitconfig.CSIConfig{
				Global: stackitconfig.GlobalOpts{
					ProjectID: projectID,
				},
			}
			openStack, err = CreateSTACKITProvider(mockAPI, config)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return error when API fails", func() {
			mockAPI.EXPECT().CreateBackup(gomock.Any(), projectID, region).Return(iaas.ApiCreateBackupRequest{ApiService: mockAPI})
			mockAPI.EXPECT().CreateBackupExecute(gomock.Any()).Return(nil, fmt.Errorf("API error"))

			backup, err := openStack.CreateBackup(context.Background(), "expected-name", "volume-id", "", nil)
			Expect(err).To(HaveOccurred())
			Expect(backup).To(BeNil())
		})

		It("should return error when both volID and snapshotID are empty", func() {
			backup, err := openStack.CreateBackup(context.Background(), "expected-name", "", "", nil)
			Expect(err).To(HaveOccurred())
			Expect(backup).To(BeNil())
		})

		It("should return error when name is empty", func() {
			backup, err := openStack.CreateBackup(context.Background(), "", "volume-id", "", nil)
			Expect(err).To(HaveOccurred())
			Expect(backup).To(BeNil())
		})
	})

	Context("CreateBackup validation cases", func() {
		const projectID = "project-id"

		BeforeEach(func() {
			config = &stackitconfig.CSIConfig{
				Global: stackitconfig.GlobalOpts{
					ProjectID: projectID,
				},
			}
			openStack, err = CreateSTACKITProvider(mockAPI, config)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return error when source type is invalid", func() {
			mockAPI.EXPECT().CreateBackup(gomock.Any(), projectID, region).Return(iaas.ApiCreateBackupRequest{ApiService: mockAPI})
			mockAPI.EXPECT().CreateBackupExecute(gomock.Any()).Return(nil, fmt.Errorf("API error"))

			backup, err := openStack.CreateBackup(context.Background(), "expected-name", "volume-id", "", nil)
			Expect(err).To(HaveOccurred())
			Expect(backup).To(BeNil())
		})

		It("should handle special characters in tags", func() {
			tags := map[string]string{
				"special": "tag with spaces and !@#$%^&*()",
				"normal":  "value",
			}

			expectedPayload := iaas.CreateBackupPayload{
				Name: new("expected-name"),
				Source: iaas.BackupSource{
					Type: "volume",
					Id:   "volume-id",
				},
				Labels: map[string]any{
					"special": "tag with spaces and !@#$%^&*()",
					"normal":  "value",
				},
			}

			mockCreateBackup(mockCtrl, mockAPI, expectedPayload)

			backup, err := openStack.CreateBackup(context.Background(), "expected-name", "volume-id", "", tags)
			Expect(err).ToNot(HaveOccurred())
			Expect(backup).ToNot(BeNil())
			Expect(backup.Id).ToNot(BeNil())
			Expect(*backup.Id).To(Equal("expected backup"))
		})
	})

	Context("CreateBackup edge cases", func() {
		const projectID = "project-id"

		BeforeEach(func() {
			config = &stackitconfig.CSIConfig{
				Global: stackitconfig.GlobalOpts{
					ProjectID: projectID,
				},
			}
			openStack, err = CreateSTACKITProvider(mockAPI, config)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should handle nil tags", func() {
			expectedPayload := iaas.CreateBackupPayload{
				Name: new("expected-name"),
				Source: iaas.BackupSource{
					Type: "volume",
					Id:   "volume-id",
				},
				Labels: nil,
			}

			mockCreateBackup(mockCtrl, mockAPI, expectedPayload)

			backup, err := openStack.CreateBackup(context.Background(), "expected-name", "volume-id", "", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(backup).ToNot(BeNil())
			Expect(backup.Id).ToNot(BeNil())
			Expect(*backup.Id).To(Equal("expected backup"))
		})

		It("should handle empty tags map", func() {
			expectedPayload := iaas.CreateBackupPayload{
				Name: new("expected-name"),
				Source: iaas.BackupSource{
					Type: "volume",
					Id:   "volume-id",
				},
				Labels: map[string]any{},
			}

			mockCreateBackup(mockCtrl, mockAPI, expectedPayload)

			backup, err := openStack.CreateBackup(context.Background(), "expected-name", "volume-id", "", map[string]string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(backup).ToNot(BeNil())
			Expect(backup.Id).ToNot(BeNil())
			Expect(*backup.Id).To(Equal("expected backup"))
		})

		It("should handle long backup name", func() {
			longName := "very-long-backup-name-" + string(make([]byte, 200))

			expectedPayload := iaas.CreateBackupPayload{
				Name: new(longName),
				Source: iaas.BackupSource{
					Type: "volume",
					Id:   "volume-id",
				},
			}

			mockCreateBackup(mockCtrl, mockAPI, expectedPayload)

			backup, err := openStack.CreateBackup(context.Background(), longName, "volume-id", "", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(backup).ToNot(BeNil())
			Expect(backup.Id).ToNot(BeNil())
			Expect(*backup.Id).To(Equal("expected backup"))
		})
	})
})

func mockCreateBackup(_ *gomock.Controller, mockAPI *mock.MockDefaultAPI, expectedPayload iaas.CreateBackupPayload) {
	const (
		projectID = "project-id"
		region    = "eu01"
	)
	mockAPI.EXPECT().CreateBackup(gomock.Any(), projectID, region).Return(iaas.ApiCreateBackupRequest{ApiService: mockAPI})
	mockAPI.EXPECT().CreateBackupExecute(gomock.Any()).Return(&iaas.Backup{Id: new("expected backup")}, nil)
	_ = expectedPayload
}
