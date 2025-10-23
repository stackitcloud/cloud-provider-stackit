package stackit

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"go.uber.org/mock/gomock"
	"k8s.io/utils/ptr"

	mock "github.com/stackitcloud/cloud-provider-stackit/pkg/mock/iaas"
)

var _ = Describe("Backup", func() {
	var (
		err       error
		mockCtrl  *gomock.Controller
		mockAPI   *mock.MockDefaultApi
		openStack IaasClient
		config    *Config
	)

	const projectID = "project-id"
	const region = "eu01"

	BeforeEach(func() {
		t := GinkgoT()
		mockCtrl = gomock.NewController(t)
		mockAPI = mock.NewMockDefaultApi(mockCtrl)
		t.Setenv("STACKIT_REGION", region)
		Expect(os.Getenv("STACKIT_REGION")).To(Equal(region))
	})

	Context("CreateBackup", func() {
		BeforeEach(func() {
			config = &Config{
				Global: GlobalOpts{
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
					Name: ptr.To("expected-name"),
					Source: &iaas.BackupSource{
						Type: ptr.To("volume"),
						Id:   ptr.To("volume-id"),
					},
					Labels: nil,
				},
			),
			Entry(
				"with snapshot source",
				"", "snapshot-id",
				map[string]string{"tag1": "value1"},
				iaas.CreateBackupPayload{
					Name: ptr.To("expected-name"),
					Source: &iaas.BackupSource{
						Type: ptr.To("snapshot"),
						Id:   ptr.To("snapshot-id"),
					},
					Labels: ptr.To(map[string]any{
						"tag1": "value1",
					}),
				},
			),
		)
	})

	Context("CreateBackup error cases", func() {
		const projectID = "project-id"

		BeforeEach(func() {
			config = &Config{
				Global: GlobalOpts{
					ProjectID: projectID,
				},
			}
			openStack, err = CreateSTACKITProvider(mockAPI, config)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return error when API fails", func() {
			mockAPI.EXPECT().CreateBackup(gomock.Any(), projectID, region).Return(nil).Times(1)

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
			config = &Config{
				Global: GlobalOpts{
					ProjectID: projectID,
				},
			}
			openStack, err = CreateSTACKITProvider(mockAPI, config)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return error when source type is invalid", func() {
			// Test with an invalid source type
			// Mock the API call to return nil, which will trigger our validation
			mockAPI.EXPECT().CreateBackup(gomock.Any(), projectID, region).Return(nil).Times(1)

			backup, err := openStack.CreateBackup(context.Background(), "expected-name", "volume-id", "", nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create backup request"))
			Expect(backup).To(BeNil())
		})

		It("should handle special characters in tags", func() {
			tags := map[string]string{
				"special": "tag with spaces and !@#$%^&*()",
				"normal":  "value",
			}

			expectedPayload := iaas.CreateBackupPayload{
				Name: ptr.To("expected-name"),
				Source: &iaas.BackupSource{
					Type: ptr.To("volume"),
					Id:   ptr.To("volume-id"),
				},
				Labels: ptr.To(map[string]any{
					"special": "tag with spaces and !@#$%^&*()",
					"normal":  "value",
				}),
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
			config = &Config{
				Global: GlobalOpts{
					ProjectID: projectID,
				},
			}
			openStack, err = CreateSTACKITProvider(mockAPI, config)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should handle nil tags", func() {
			expectedPayload := iaas.CreateBackupPayload{
				Name: ptr.To("expected-name"),
				Source: &iaas.BackupSource{
					Type: ptr.To("volume"),
					Id:   ptr.To("volume-id"),
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
				Name: ptr.To("expected-name"),
				Source: &iaas.BackupSource{
					Type: ptr.To("volume"),
					Id:   ptr.To("volume-id"),
				},
				Labels: ptr.To(map[string]any{}),
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
				Name: ptr.To(longName),
				Source: &iaas.BackupSource{
					Type: ptr.To("volume"),
					Id:   ptr.To("volume-id"),
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

func mockCreateBackup(mockCtrl *gomock.Controller, mockAPI *mock.MockDefaultApi, expectedPayload iaas.CreateBackupPayload) {
	const (
		projectID = "project-id"
		region    = "eu01"
	)
	createRequest := mock.NewMockApiCreateBackupRequest(mockCtrl)
	createRequest.EXPECT().CreateBackupPayload(expectedPayload).Return(createRequest)
	createRequest.EXPECT().Execute().Return(&iaas.Backup{Id: ptr.To("expected backup")}, nil)

	mockAPI.EXPECT().CreateBackup(gomock.Any(), projectID, region).Return(createRequest)
}
