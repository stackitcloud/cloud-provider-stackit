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

var _ = Describe("Snapshot", func() {
	var (
		err           error
		mockCtrl      *gomock.Controller
		mockAPI       *mock.MockDefaultApi
		stackitClient IaasClient
		config        *Config
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockAPI = mock.NewMockDefaultApi(mockCtrl)
	})

	Context("ListSnapshot", func() {
		const projectID = "project-id"

		snapShotListResponse := iaas.SnapshotListResponse{
			Items: &[]iaas.Snapshot{
				{
					Id:       ptr.To("fake-snapshot"),
					Name:     ptr.To("fake-snapshot"),
					VolumeId: ptr.To("some-special-volume"),
					Status:   ptr.To("ERROR"),
				},
				{
					Id:       ptr.To("fake-snapshot2"),
					Name:     ptr.To("fake-snapshot2"),
					VolumeId: ptr.To("some-special-volume"),
					Status:   ptr.To("AVAILABLE"),
				},
				{
					Id:       ptr.To("wrong snapshot"),
					Name:     ptr.To("wrong snapshot"),
					VolumeId: ptr.To("another-special-volume"),
					Status:   ptr.To("AVAILABLE"),
				},
			},
		}

		BeforeEach(func() {
			config = &Config{
				Global: GlobalOpts{
					ProjectID: projectID,
				},
			}
			stackitClient, err = CreateSTACKITProvider(mockAPI, config)
			Expect(err).ToNot(HaveOccurred())
		})

		DescribeTable("should return a filtered list of snapshots",
			func(filters map[string]string, expectedSnaps []iaas.Snapshot) {
				listRequest := mock.NewMockApiListSnapshotsRequest(mockCtrl)
				listRequest.EXPECT().Execute().Return(&snapShotListResponse, nil)
				mockAPI.EXPECT().ListSnapshots(gomock.Any(), config.Global.ProjectID).Return(listRequest)

				snaps, _, err := stackitClient.ListSnapshots(context.Background(), filters)
				Expect(err).ToNot(HaveOccurred())
				Expect(snaps).To(Equal(expectedSnaps))
			},
			Entry("filter by VolumeID",
				map[string]string{"VolumeID": "some-special-volume"},
				[]iaas.Snapshot{
					{
						Id:       ptr.To("fake-snapshot"),
						Name:     ptr.To("fake-snapshot"),
						VolumeId: ptr.To("some-special-volume"),
						Status:   ptr.To("ERROR"),
					},
					{
						Id:       ptr.To("fake-snapshot2"),
						Name:     ptr.To("fake-snapshot2"),
						VolumeId: ptr.To("some-special-volume"),
						Status:   ptr.To("AVAILABLE"),
					},
				},
			),
			Entry("filter by name",
				map[string]string{"Name": "fake-snapshot"},
				[]iaas.Snapshot{
					{
						Id:       ptr.To("fake-snapshot"),
						Name:     ptr.To("fake-snapshot"),
						VolumeId: ptr.To("some-special-volume"),
						Status:   ptr.To("ERROR"),
					},
				},
			),
			Entry("filter by status and name",
				map[string]string{"Name": "fake-snapshot2", "Status": "AVAILABLE"},
				[]iaas.Snapshot{
					{
						Id:       ptr.To("fake-snapshot2"),
						Name:     ptr.To("fake-snapshot2"),
						VolumeId: ptr.To("some-special-volume"),
						Status:   ptr.To("AVAILABLE"),
					},
				},
			),
			Entry("no filters",
				map[string]string{},
				*snapShotListResponse.Items,
			),
		)
	})
})
