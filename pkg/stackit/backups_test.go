package stackit

import (
	"context"

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

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockAPI = mock.NewMockDefaultApi(mockCtrl)
	})

	Context("CreateBackup", func() {
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

		DescribeTable("successful call variant",
			func(
				volId, snapshotId string,
				tags map[string]string,
				expectedPayload iaas.CreateBackupPayload) {

				mockCreateBackup(
					mockCtrl,
					mockAPI,
					projectID,
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
})

func mockCreateBackup(mockCtrl *gomock.Controller, mockAPI *mock.MockDefaultApi, projectID string, expectedPayload iaas.CreateBackupPayload) {
	createRequest := mock.NewMockApiCreateBackupRequest(mockCtrl)
	createRequest.EXPECT().CreateBackupPayload(expectedPayload).Return(createRequest)
	createRequest.EXPECT().Execute().Return(&iaas.Backup{Id: ptr.To("expected backup")}, nil)

	mockAPI.EXPECT().CreateBackup(gomock.Any(), projectID).Return(createRequest)
}
