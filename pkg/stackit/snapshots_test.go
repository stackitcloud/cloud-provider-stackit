package stackit

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"go.uber.org/mock/gomock"

	mock "github.com/stackitcloud/cloud-provider-stackit/pkg/mock/iaas"
)

var _ = Describe("Snapshot", func() {
	var (
		err           error
		mockCtrl      *gomock.Controller
		mockAPI       *mock.MockDefaultApi
		stackitClient IaasClient
		config        *stackitconfig.CSIConfig
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

	Context("ListSnapshot", func() {

		snapShotListResponse := iaas.SnapshotListResponse{
			Items: &[]iaas.Snapshot{
				{
					Id:       new("fake-snapshot"),
					Name:     new("fake-snapshot"),
					VolumeId: new("some-special-volume"),
					Status:   new("ERROR"),
				},
				{
					Id:       new("fake-snapshot2"),
					Name:     new("fake-snapshot2"),
					VolumeId: new("some-special-volume"),
					Status:   new("AVAILABLE"),
				},
				{
					Id:       new("wrong snapshot"),
					Name:     new("wrong snapshot"),
					VolumeId: new("another-special-volume"),
					Status:   new("AVAILABLE"),
				},
			},
		}

		BeforeEach(func() {
			config = &stackitconfig.CSIConfig{
				Global: stackitconfig.GlobalOpts{
					ProjectID: projectID,
				},
			}
			stackitClient, err = CreateSTACKITProvider(mockAPI, config)
			Expect(err).ToNot(HaveOccurred())
		})

		DescribeTable("should return a filtered list of snapshots",
			func(filters map[string]string, expectedSnaps []iaas.Snapshot) {
				listRequest := mock.NewMockApiListSnapshotsInProjectRequest(mockCtrl)
				listRequest.EXPECT().Execute().Return(&snapShotListResponse, nil)
				mockAPI.EXPECT().ListSnapshotsInProject(gomock.Any(), config.Global.ProjectID, region).Return(listRequest)

				snaps, _, err := stackitClient.ListSnapshots(context.Background(), filters)
				Expect(err).ToNot(HaveOccurred())
				Expect(snaps).To(Equal(expectedSnaps))
			},
			Entry("filter by VolumeID",
				map[string]string{"VolumeID": "some-special-volume"},
				[]iaas.Snapshot{
					{
						Id:       new("fake-snapshot"),
						Name:     new("fake-snapshot"),
						VolumeId: new("some-special-volume"),
						Status:   new("ERROR"),
					},
					{
						Id:       new("fake-snapshot2"),
						Name:     new("fake-snapshot2"),
						VolumeId: new("some-special-volume"),
						Status:   new("AVAILABLE"),
					},
				},
			),
			Entry("filter by name",
				map[string]string{"Name": "fake-snapshot"},
				[]iaas.Snapshot{
					{
						Id:       new("fake-snapshot"),
						Name:     new("fake-snapshot"),
						VolumeId: new("some-special-volume"),
						Status:   new("ERROR"),
					},
				},
			),
			Entry("filter by status and name",
				map[string]string{"Name": "fake-snapshot2", "Status": "AVAILABLE"},
				[]iaas.Snapshot{
					{
						Id:       new("fake-snapshot2"),
						Name:     new("fake-snapshot2"),
						VolumeId: new("some-special-volume"),
						Status:   new("AVAILABLE"),
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
