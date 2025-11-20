package blockstorage

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi/util"
	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
)

var _ = Describe("ControllerServer test", Ordered, func() {
	var (
		fakeCs             *controllerServer
		iaasClient         *stackit.MockIaasClient
		FakeEndpoint       = "tcp://127.0.0.1:10000"
		FakeCluster        = "cluster"
		expandTargetStatus = []string{stackit.VolumeAvailableStatus, stackit.VolumeAttachedStatus}
		stdCapRange        = &csi.CapacityRange{
			RequiredBytes: util.GIBIBYTE * 20,
		}
		stdSnapParams = map[string]string{
			"type": "snapshot",
		}
		stdVolCap = &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		}
		stdVolCaps = []*csi.VolumeCapability{
			stdVolCap,
		}
	)

	BeforeEach(func() {
		d := NewDriver(&DriverOpts{Endpoint: FakeEndpoint, ClusterID: FakeCluster})

		mockCtrl := gomock.NewController(GinkgoT())
		iaasClient = stackit.NewMockIaasClient(mockCtrl)

		fakeCs = NewControllerServer(d, iaasClient)
	})

	Describe("CreateVolume", func() {
		It("should create a volume with minimal information", func() {
			req := &csi.CreateVolumeRequest{
				Name:               "new volume",
				VolumeCapabilities: stdVolCaps,
				CapacityRange:      stdCapRange,
			}

			iaasClient.EXPECT().GetVolumesByName(gomock.Any(), "new volume").Return([]iaas.Volume{}, nil)

			iaasClient.EXPECT().CreateVolume(gomock.Any(), gomock.Any()).Return(&iaas.Volume{
				Id:               ptr.To("volume-id"),
				Name:             ptr.To("new volume"),
				AvailabilityZone: ptr.To("eu01"),
				Size:             ptr.To(int64(20)),
			}, nil)
			iaasClient.EXPECT().WaitVolumeTargetStatusWithCustomBackoff(gomock.Any(), "volume-id", gomock.Any(), gomock.Any()).Return(nil)

			resp, err := fakeCs.CreateVolume(context.Background(), req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Volume.VolumeId).To(Equal("volume-id"))
			Expect(resp.Volume.CapacityBytes).To(Equal(util.GIBIBYTE * 20))
		})

		It("should not accept an empty volume name", func() {
			req := &csi.CreateVolumeRequest{
				Name: "",
			}

			_, err := fakeCs.CreateVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("missing Volume Name"))
		})

		It("should not accept empty volume capabilities", func() {
			req := &csi.CreateVolumeRequest{
				Name:               "volume name",
				VolumeCapabilities: nil,
			}

			_, err := fakeCs.CreateVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("missing Volume capability"))
		})

		It("should prefer the availability zone defined in VolumeParameters", func() {
			req := &csi.CreateVolumeRequest{
				Name:               "volume name",
				VolumeCapabilities: stdVolCaps,
				Parameters: map[string]string{
					"type":         "perf1",
					"availability": "zone-from-parameters",
				},
				AccessibilityRequirements: &csi.TopologyRequirement{
					Requisite: []*csi.Topology{
						{Segments: map[string]string{topologyKey: "zone-from-accessibility-reqs"}},
					},
				},
			}

			iaasClient.EXPECT().GetVolumesByName(gomock.Any(), "volume name").Return([]iaas.Volume{}, nil)

			iaasClient.EXPECT().CreateVolume(gomock.Any(), gomock.Any()).Return(&iaas.Volume{
				Id:               ptr.To("volume-id"),
				Name:             ptr.To("volume name"),
				AvailabilityZone: ptr.To("zone-from-parameters"),
				Size:             ptr.To(int64(20)),
			}, nil)
			iaasClient.EXPECT().WaitVolumeTargetStatusWithCustomBackoff(gomock.Any(), "volume-id", gomock.Any(), gomock.Any()).Return(nil)

			_, err := fakeCs.CreateVolume(context.Background(), req)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should use the availability zone defined in AccessibilityRequirements as fallback", func() {
			req := &csi.CreateVolumeRequest{
				Name:               "volume name",
				VolumeCapabilities: stdVolCaps,
				Parameters: map[string]string{
					"type": "perf1",
				},
				AccessibilityRequirements: &csi.TopologyRequirement{
					Requisite: []*csi.Topology{
						{Segments: map[string]string{topologyKey: "zone-from-accessibility-reqs"}},
					},
				},
			}

			iaasClient.EXPECT().GetVolumesByName(gomock.Any(), "volume name").Return([]iaas.Volume{}, nil)

			iaasClient.EXPECT().CreateVolume(gomock.Any(), gomock.Any()).Return(&iaas.Volume{
				Id:               ptr.To("volume-id"),
				Name:             ptr.To("volume name"),
				AvailabilityZone: ptr.To("zone-from-accessibility-reqs"),
				Size:             ptr.To(int64(20)),
			}, nil)
			iaasClient.EXPECT().WaitVolumeTargetStatusWithCustomBackoff(gomock.Any(), "volume-id", gomock.Any(), gomock.Any()).Return(nil)

			_, err := fakeCs.CreateVolume(context.Background(), req)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should fail when looking for existing volumes fails", func() {
			req := &csi.CreateVolumeRequest{
				Name:               "new volume",
				VolumeCapabilities: stdVolCaps,
				CapacityRange:      stdCapRange,
			}

			iaasClient.EXPECT().GetVolumesByName(gomock.Any(), "new volume").Return(nil, fmt.Errorf("injected error"))

			_, err := fakeCs.CreateVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.Internal))
			Expect(err.Error()).To(ContainSubstring("injected error"))
		})

		It("should use an existing volume if it fits the requirements", func() {
			req := &csi.CreateVolumeRequest{
				Name:               "new volume",
				VolumeCapabilities: stdVolCaps,
				CapacityRange:      stdCapRange,
			}

			iaasClient.EXPECT().GetVolumesByName(gomock.Any(), "new volume").Return([]iaas.Volume{
				{
					Id:               ptr.To("existing-available-volume-id"),
					Name:             ptr.To("new volume"),
					Size:             ptr.To(int64(20)),
					Status:           ptr.To(stackit.VolumeAvailableStatus),
					AvailabilityZone: ptr.To("eu01"),
				},
			}, nil)

			resp, err := fakeCs.CreateVolume(context.Background(), req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Volume.VolumeId).To(Equal("existing-available-volume-id"))
			Expect(resp.Volume.CapacityBytes).To(Equal(util.GIBIBYTE * 20))
		})

		It("should fail if a volume exists but does not fit in size", func() {
			req := &csi.CreateVolumeRequest{
				Name:               "new volume",
				VolumeCapabilities: stdVolCaps,
				CapacityRange:      stdCapRange,
			}

			iaasClient.EXPECT().GetVolumesByName(gomock.Any(), "new volume").Return([]iaas.Volume{
				{
					Id:               ptr.To("existing-available-volume-id"),
					Name:             ptr.To("new volume"),
					Size:             ptr.To(int64(30)),
					Status:           ptr.To(stackit.VolumeAvailableStatus),
					AvailabilityZone: ptr.To("eu01"),
				},
			}, nil)

			_, err := fakeCs.CreateVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.AlreadyExists))
		})

		It("should fail if a volume exists but is not available", func() {
			req := &csi.CreateVolumeRequest{
				Name:               "new volume",
				VolumeCapabilities: stdVolCaps,
				CapacityRange:      stdCapRange,
			}

			iaasClient.EXPECT().GetVolumesByName(gomock.Any(), "new volume").Return([]iaas.Volume{
				{
					Id:               ptr.To("existing-available-volume-id"),
					Name:             ptr.To("new volume"),
					Size:             ptr.To(int64(20)),
					Status:           ptr.To(stackit.VolumeAttachedStatus),
					AvailabilityZone: ptr.To("eu01"),
				},
			}, nil)

			_, err := fakeCs.CreateVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.Internal))
			Expect(err.Error()).To(ContainSubstring("is not in available state"))
		})

		It("should fail if more than one volume with the same name are available", func() {
			req := &csi.CreateVolumeRequest{
				Name:               "new volume",
				VolumeCapabilities: stdVolCaps,
				CapacityRange:      stdCapRange,
			}

			iaasClient.EXPECT().GetVolumesByName(gomock.Any(), "new volume").Return([]iaas.Volume{
				{
					Id: ptr.To("volume-0"),
				},
				{
					Id: ptr.To("volume-1"),
				},
			}, nil)

			_, err := fakeCs.CreateVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.Internal))
			Expect(err.Error()).To(ContainSubstring("Multiple volumes reported by Cinder with same name"))
		})

		Context("content source", func() {
			var req *csi.CreateVolumeRequest

			BeforeEach(func() {
				req = &csi.CreateVolumeRequest{
					Name:               "new volume",
					VolumeCapabilities: stdVolCaps,
					CapacityRange:      stdCapRange,
					AccessibilityRequirements: &csi.TopologyRequirement{
						Requisite: []*csi.Topology{
							{
								Segments: map[string]string{topologyKey: "eu01"},
							},
						},
					},
				}

				iaasClient.EXPECT().GetVolumesByName(gomock.Any(), "new volume").Return([]iaas.Volume{}, nil)
			})

			It("should use a snapshot if a snapshot ID is provided as content source and the snapshot is available", func() {
				req.VolumeContentSource = &csi.VolumeContentSource{
					Type: &csi.VolumeContentSource_Snapshot{
						Snapshot: &csi.VolumeContentSource_SnapshotSource{
							SnapshotId: "snapshot-id",
						},
					},
				}

				iaasClient.EXPECT().GetSnapshotByID(gomock.Any(), "snapshot-id").Return(&iaas.Snapshot{
					Id:       ptr.To("snapshot-id"),
					Status:   ptr.To("AVAILABLE"),
					VolumeId: ptr.To("snapshot-volume-id"),
				}, nil)
				iaasClient.EXPECT().GetVolume(gomock.Any(), "snapshot-volume-id").Return(&iaas.Volume{
					Id:               ptr.To("snapshot-volume-id"),
					AvailabilityZone: ptr.To("eu01"),
				}, nil)
				iaasClient.EXPECT().CreateVolume(gomock.Any(), gomock.Any()).
					Do(func(_ context.Context, opts *iaas.CreateVolumePayload) {
						Expect(*opts.Source.Id).To(Equal("snapshot-id"))
						Expect(*opts.Source.Type).To(Equal("snapshot"))
					}).
					Return(&iaas.Volume{
						Id:               ptr.To("volume-id"),
						Name:             ptr.To("new volume"),
						AvailabilityZone: ptr.To("eu01"),
						Size:             ptr.To(int64(20)),
					}, nil)
				iaasClient.EXPECT().WaitVolumeTargetStatusWithCustomBackoff(gomock.Any(), "volume-id", gomock.Any(), gomock.Any()).Return(nil)

				_, err := fakeCs.CreateVolume(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should fail if a snapshot ID is provided as content source and the snapshot cannot be retrieved", func() {
				req.VolumeContentSource = &csi.VolumeContentSource{
					Type: &csi.VolumeContentSource_Snapshot{
						Snapshot: &csi.VolumeContentSource_SnapshotSource{
							SnapshotId: "snapshot-id",
						},
					},
				}

				iaasClient.EXPECT().GetSnapshotByID(gomock.Any(), "snapshot-id").Return(nil, fmt.Errorf("injected error"))

				_, err := fakeCs.CreateVolume(context.Background(), req)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("injected error"))
			})

			It("should fail if a snapshot ID is provided as content source and the snapshot is not available", func() {
				req.VolumeContentSource = &csi.VolumeContentSource{
					Type: &csi.VolumeContentSource_Snapshot{
						Snapshot: &csi.VolumeContentSource_SnapshotSource{
							SnapshotId: "snapshot-id",
						},
					},
				}

				iaasClient.EXPECT().GetSnapshotByID(gomock.Any(), "snapshot-id").Return(&iaas.Snapshot{
					Id:     ptr.To("snapshot-id"),
					Status: ptr.To("creating"),
				}, nil)

				_, err := fakeCs.CreateVolume(context.Background(), req)
				Expect(err).To(HaveOccurred())
				Expect(status.Code(err)).To(Equal(codes.Unavailable))
				Expect(err.Error()).To(ContainSubstring("is not yet available"))
			})

			It("should use a backup as fallback if a snapshot ID is provided as content source and the snapshot is not found", func() {
				req.VolumeContentSource = &csi.VolumeContentSource{
					Type: &csi.VolumeContentSource_Snapshot{
						Snapshot: &csi.VolumeContentSource_SnapshotSource{
							SnapshotId: "snapshot-id",
						},
					},
				}

				iaasClient.EXPECT().GetSnapshotByID(gomock.Any(), "snapshot-id").Return(nil,
					&oapierror.GenericOpenAPIError{
						StatusCode: http.StatusNotFound,
					})
				iaasClient.EXPECT().GetBackupByID(gomock.Any(), "snapshot-id").Return(&iaas.Backup{
					Status:           ptr.To("AVAILABLE"),
					AvailabilityZone: ptr.To("eu01"),
				}, nil)
				iaasClient.EXPECT().CreateVolume(gomock.Any(), gomock.Any()).
					Do(func(_ context.Context, opts *iaas.CreateVolumePayload) {
						Expect(*opts.Source.Id).To(Equal("snapshot-id"))
						Expect(*opts.Source.Type).To(Equal("backup"))
					}).
					Return(&iaas.Volume{
						Id:               ptr.To("volume-id"),
						Name:             ptr.To("new volume"),
						AvailabilityZone: ptr.To("eu01"),
						Size:             ptr.To(int64(20)),
					}, nil)
				iaasClient.EXPECT().WaitVolumeTargetStatusWithCustomBackoff(gomock.Any(), "volume-id", gomock.Any(), gomock.Any()).Return(nil)

				_, err := fakeCs.CreateVolume(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should fail if the snapshot has a different AZ than the volume", func() {
				req.VolumeContentSource = &csi.VolumeContentSource{
					Type: &csi.VolumeContentSource_Snapshot{
						Snapshot: &csi.VolumeContentSource_SnapshotSource{
							SnapshotId: "snapshot-id",
						},
					},
				}
				req.AccessibilityRequirements = &csi.TopologyRequirement{
					Requisite: []*csi.Topology{
						{Segments: map[string]string{topologyKey: "some-other-zone"}},
					},
				}

				iaasClient.EXPECT().GetSnapshotByID(gomock.Any(), "snapshot-id").Return(&iaas.Snapshot{
					Id:       ptr.To("snapshot-id"),
					VolumeId: ptr.To("volume-id"),
					Status:   ptr.To("AVAILABLE"),
				}, nil)
				iaasClient.EXPECT().GetVolume(gomock.Any(), "volume-id").Return(&iaas.Volume{
					Status:           ptr.To("AVAILABLE"),
					AvailabilityZone: ptr.To("eu01"),
					Id:               ptr.To("volume-id"),
				}, nil)

				_, err := fakeCs.CreateVolume(context.Background(), req)
				Expect(err).To(HaveOccurred())
				Expect(status.Code(err)).To(Equal(codes.ResourceExhausted))
				Expect(err.Error()).To(ContainSubstring("must be in the same availability zone as source"))
			})

			It("should fail if the snapshot and the backup can both not be found", func() {
				req.VolumeContentSource = &csi.VolumeContentSource{
					Type: &csi.VolumeContentSource_Snapshot{
						Snapshot: &csi.VolumeContentSource_SnapshotSource{
							SnapshotId: "snapshot-id",
						},
					},
				}

				iaasClient.EXPECT().GetSnapshotByID(gomock.Any(), "snapshot-id").Return(nil,
					&oapierror.GenericOpenAPIError{
						StatusCode: http.StatusNotFound,
					})
				iaasClient.EXPECT().GetBackupByID(gomock.Any(), "snapshot-id").Return(nil,
					&oapierror.GenericOpenAPIError{
						StatusCode: http.StatusNotFound,
					})

				_, err := fakeCs.CreateVolume(context.Background(), req)
				Expect(err).To(HaveOccurred())
				Expect(status.Code(err)).To(Equal(codes.NotFound))
				Expect(err.Error()).To(ContainSubstring("not found"))
			})

			It("should fail if the snapshot cannot be found and the backup is not available", func() {
				req.VolumeContentSource = &csi.VolumeContentSource{
					Type: &csi.VolumeContentSource_Snapshot{
						Snapshot: &csi.VolumeContentSource_SnapshotSource{
							SnapshotId: "snapshot-id",
						},
					},
				}

				iaasClient.EXPECT().GetSnapshotByID(gomock.Any(), "snapshot-id").Return(nil,
					&oapierror.GenericOpenAPIError{
						StatusCode: http.StatusNotFound,
					})
				iaasClient.EXPECT().GetBackupByID(gomock.Any(), "snapshot-id").Return(&iaas.Backup{
					Status: ptr.To("creating"),
				}, nil)

				_, err := fakeCs.CreateVolume(context.Background(), req)
				Expect(err).To(HaveOccurred())
				Expect(status.Code(err)).To(Equal(codes.Unavailable))
				Expect(err.Error()).To(ContainSubstring("is not yet available"))
			})

			It("should use a volume if a volume ID is provided as content source and the volume is available", func() {
				req.VolumeContentSource = &csi.VolumeContentSource{
					Type: &csi.VolumeContentSource_Volume{
						Volume: &csi.VolumeContentSource_VolumeSource{
							VolumeId: "volume-source-id",
						},
					},
				}

				iaasClient.EXPECT().GetVolume(gomock.Any(), "volume-source-id").Return(&iaas.Volume{
					Id:               ptr.To("volume-source-id"),
					Status:           ptr.To("AVAILABLE"),
					AvailabilityZone: ptr.To("eu01"),
				}, nil)
				iaasClient.EXPECT().CreateVolume(gomock.Any(), gomock.Any()).
					Do(func(_ context.Context, opts *iaas.CreateVolumePayload) {
						Expect(*opts.Source.Id).To(Equal("volume-source-id"))
						Expect(*opts.Source.Type).To(Equal("volume"))
					}).
					Return(&iaas.Volume{
						Id:               ptr.To("volume-id"),
						Name:             ptr.To("new volume"),
						AvailabilityZone: ptr.To("eu01"),
						Size:             ptr.To(int64(20)),
					}, nil)
				iaasClient.EXPECT().WaitVolumeTargetStatusWithCustomBackoff(gomock.Any(), "volume-id", gomock.Any(), gomock.Any()).Return(nil)

				_, err := fakeCs.CreateVolume(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should fail a volume ID is provided as content source and the volume cannot be found", func() {
				req.VolumeContentSource = &csi.VolumeContentSource{
					Type: &csi.VolumeContentSource_Volume{
						Volume: &csi.VolumeContentSource_VolumeSource{
							VolumeId: "volume-source-id",
						},
					},
				}

				iaasClient.EXPECT().GetVolume(gomock.Any(), "volume-source-id").Return(nil,
					&oapierror.GenericOpenAPIError{
						StatusCode: http.StatusNotFound,
					})

				_, err := fakeCs.CreateVolume(context.Background(), req)
				Expect(err).To(HaveOccurred())
				Expect(status.Code(err)).To(Equal(codes.NotFound))
				Expect(err.Error()).To(ContainSubstring("Source Volume volume-source-id not found"))
			})

			It("should fail a volume ID is provided as content source and the volume cannot be retrieved", func() {
				req.VolumeContentSource = &csi.VolumeContentSource{
					Type: &csi.VolumeContentSource_Volume{
						Volume: &csi.VolumeContentSource_VolumeSource{
							VolumeId: "volume-source-id",
						},
					},
				}

				iaasClient.EXPECT().GetVolume(gomock.Any(), "volume-source-id").Return(nil,
					fmt.Errorf("injected error"))

				_, err := fakeCs.CreateVolume(context.Background(), req)
				Expect(err).To(HaveOccurred())
				Expect(status.Code(err)).To(Equal(codes.NotFound))
				Expect(err.Error()).To(ContainSubstring("Failed to retrieve the source volume"))
			})
		})

		It("should fail if the final call to CreateVolume fails", func() {
			req := &csi.CreateVolumeRequest{
				Name:               "new volume",
				VolumeCapabilities: stdVolCaps,
				CapacityRange:      stdCapRange,
			}

			iaasClient.EXPECT().GetVolumesByName(gomock.Any(), "new volume").Return([]iaas.Volume{}, nil)

			iaasClient.EXPECT().CreateVolume(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("injected error"))

			_, err := fakeCs.CreateVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.Internal))
			Expect(err.Error()).To(ContainSubstring("CreateVolume failed with error injected error"))
		})

		It("should fail if the created volume is not available within time", func() {
			req := &csi.CreateVolumeRequest{
				Name:               "new volume",
				VolumeCapabilities: stdVolCaps,
				CapacityRange:      stdCapRange,
			}

			iaasClient.EXPECT().GetVolumesByName(gomock.Any(), "new volume").Return([]iaas.Volume{}, nil)

			iaasClient.EXPECT().CreateVolume(gomock.Any(), gomock.Any()).Return(&iaas.Volume{
				Id:               ptr.To("volume-id"),
				Name:             ptr.To("new volume"),
				AvailabilityZone: ptr.To("eu01"),
				Size:             ptr.To(int64(20)),
			}, nil)
			iaasClient.EXPECT().WaitVolumeTargetStatusWithCustomBackoff(gomock.Any(), "volume-id", gomock.Any(), gomock.Any()).
				Return(fmt.Errorf("injected error"))

			_, err := fakeCs.CreateVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.Internal))
			Expect(err.Error()).To(ContainSubstring("failed getting available in time"))
		})
	})

	Describe("DeleteVolume", func() {
		It("should return error when Volume ID is not provided", func() {
			req := &csi.DeleteVolumeRequest{}
			_, err := fakeCs.DeleteVolume(context.Background(), req)
			Expect(err).Should(Equal(status.Error(codes.InvalidArgument, "DeleteVolume Volume ID must be provided")))
		})
		It("should succeed when Volume ID is provided", func() {
			req := &csi.DeleteVolumeRequest{
				VolumeId: "fake",
			}
			iaasClient.EXPECT().DeleteVolume(gomock.Any(), req.VolumeId).Return(nil)
			_, err := fakeCs.DeleteVolume(context.Background(), req)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})
	Describe("ListVolumes", func() {
		It("should correctly produce ListVolumesResponse_Entry response", func() {
			req := &csi.ListVolumesRequest{
				MaxEntries:    10,
				StartingToken: "",
			}
			expectedVolumeResponseList := []*csi.ListVolumesResponse_Entry{
				{
					Volume: &csi.Volume{
						VolumeId:      "fake",
						CapacityBytes: 10 * util.GIBIBYTE,
					},
				},
				{
					Volume: &csi.Volume{
						VolumeId:      "fake1",
						CapacityBytes: 10 * util.GIBIBYTE,
					},
					Status: &csi.ListVolumesResponse_VolumeStatus{
						PublishedNodeIds: []string{"serverID"},
					},
				},
			}
			// Pagination is not supported by the API yet, so the arguments are ignored
			iaasClient.EXPECT().ListVolumes(gomock.Any(), gomock.Any(), gomock.Any()).Return([]iaas.Volume{
				{
					Id:     ptr.To("fake"),
					Status: ptr.To("AVAILABLE"),
					Name:   ptr.To("fake"),
					Size:   ptr.To(int64(10)),
				},
				{
					Id:       ptr.To("fake1"),
					Status:   ptr.To("AVAILABLE"),
					Name:     ptr.To("fake1"),
					Size:     ptr.To(int64(10)),
					ServerId: ptr.To("serverID"),
				},
			}, "", nil)
			resp, err := fakeCs.ListVolumes(context.Background(), req)
			Expect(err).Should(Not(HaveOccurred()))
			Expect(resp.GetEntries()).Should(Equal(expectedVolumeResponseList))
		})
	})
	Describe("ControllerPublishVolume", func() {
		It("should successfully attach volume to node", func() {
			req := &csi.ControllerPublishVolumeRequest{
				VolumeId:         "fake",
				NodeId:           "fake",
				VolumeCapability: stdVolCap,
			}
			iaasClient.EXPECT().GetVolume(gomock.Any(), req.VolumeId).Return(&iaas.Volume{}, nil)
			iaasClient.EXPECT().GetInstanceByID(gomock.Any(), "fake").Return(&iaas.Server{}, nil)
			iaasClient.EXPECT().AttachVolume(gomock.Any(), req.NodeId, req.VolumeId).Return(req.VolumeId, nil)
			iaasClient.EXPECT().WaitDiskAttached(gomock.Any(), req.NodeId, req.VolumeId).Return(nil)
			_, err := fakeCs.ControllerPublishVolume(context.Background(), req)
			Expect(err).To(Not(HaveOccurred()))
		})
	})
	Describe("ControllerUnpublishVolume", func() {
		It("should successfully detach volume from node", func() {
			req := &csi.ControllerUnpublishVolumeRequest{
				VolumeId: "fake",
				NodeId:   "fake",
			}
			iaasClient.EXPECT().GetInstanceByID(gomock.Any(), "fake").Return(&iaas.Server{}, nil)
			iaasClient.EXPECT().DetachVolume(gomock.Any(), req.NodeId, req.VolumeId).Return(nil)
			iaasClient.EXPECT().WaitDiskDetached(gomock.Any(), req.NodeId, req.VolumeId).Return(nil)
			_, err := fakeCs.ControllerUnpublishVolume(context.Background(), req)
			Expect(err).To(Not(HaveOccurred()))
		})
	})
	Describe("ControllerGetVolume", func() {
		It("should get volume successfully", func() {
			req := &csi.ControllerGetVolumeRequest{
				VolumeId: "fake",
			}
			expectedVol := &iaas.Volume{
				ServerId: ptr.To("fake"),
				Size:     ptr.To(100 * util.GIBIBYTE),
			}
			iaasClient.EXPECT().GetVolume(gomock.Any(), req.VolumeId).Return(expectedVol, nil)
			resp, err := fakeCs.ControllerGetVolume(context.Background(), req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetPublishedNodeIds()[0]).To(Equal(expectedVol.GetServerId()))
			Expect(resp.GetStatus().GetPublishedNodeIds()).To(HaveLen(1))
		})
	})
	Describe("ControllerExpandVolume", func() {
		It("should expand volume successfully", func() {
			req := &csi.ControllerExpandVolumeRequest{
				VolumeId:      "fake",
				CapacityRange: stdCapRange,
			}
			volSizeGB := util.RoundUpSize(req.GetCapacityRange().GetRequiredBytes(), util.GIBIBYTE)
			iaasClient.EXPECT().GetVolume(gomock.Any(), req.VolumeId).Return(&iaas.Volume{
				Size:   ptr.To(int64(10)),
				Status: ptr.To(stackit.VolumeAvailableStatus),
			}, nil)
			iaasClient.EXPECT().ExpandVolume(gomock.Any(), req.VolumeId, stackit.VolumeAvailableStatus, volSizeGB).Return(nil)
			iaasClient.EXPECT().WaitVolumeTargetStatus(gomock.Any(), req.VolumeId, expandTargetStatus).Return(nil)
			_, err := fakeCs.ControllerExpandVolume(context.Background(), req)
			Expect(err).To(Not(HaveOccurred()))
		})
		It("should return error when volume status is not available", func() {
			req := &csi.ControllerExpandVolumeRequest{
				VolumeId:      "fake",
				CapacityRange: stdCapRange,
			}
			volSizeGB := util.RoundUpSize(req.GetCapacityRange().GetRequiredBytes(), util.GIBIBYTE)
			iaasClient.EXPECT().GetVolume(gomock.Any(), req.VolumeId).Return(&iaas.Volume{
				Size:   ptr.To(int64(10)),
				Status: ptr.To("ERROR"),
			}, nil)
			iaasClient.EXPECT().ExpandVolume(gomock.Any(), req.VolumeId, "ERROR", volSizeGB).Return(fmt.Errorf("volume cannot be resized, when status is ERROR"))
			_, err := fakeCs.ControllerExpandVolume(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(status.Convert(err).Code()).To(Equal(codes.Internal))
			Expect(status.Convert(err).Message()).To(ContainSubstring("volume cannot be resized, when status is ERROR"))
		})
	})
	Describe("CreateSnapshot", func() {
		Context("Backup", func() {
			var req *csi.CreateSnapshotRequest
			JustBeforeEach(func() {
				req = &csi.CreateSnapshotRequest{
					SourceVolumeId: "fake",
					Name:           "fake-snapshot",
					Parameters:     map[string]string{"type": "backup"},
				}
			})
			It("should create backup successfully", func() {
				// TODO: Use once IaaS has extended the label regex to allow for forward slashes and dots
				// properties := map[string]string{blockStorageCSIClusterIDKey: "cluster"}
				properties := map[string]string{}
				expectedSnap := &iaas.Snapshot{
					Id:        ptr.To("fake-snapshot"),
					Name:      ptr.To("fake-snapshot"),
					Status:    ptr.To("AVAILABLE"),
					Size:      ptr.To(int64(10)),
					CreatedAt: ptr.To(time.Now()),
				}
				expectedBackup := &iaas.Backup{
					Id:         ptr.To("fake-backup"),
					Name:       ptr.To("fake-backup"),
					Status:     ptr.To("AVAILABLE"),
					SnapshotId: ptr.To("fake-snapshot"),
					Size:       ptr.To(int64(10)),
					VolumeId:   ptr.To(req.GetSourceVolumeId()),
					CreatedAt:  ptr.To(time.Now()),
				}

				iaasClient.EXPECT().ListBackups(gomock.Any(), gomock.Any()).Return([]iaas.Backup{}, nil)

				// Backups are created from snapshots
				iaasClient.EXPECT().ListSnapshots(gomock.Any(), gomock.Any()).Return([]iaas.Snapshot{}, "", nil)
				iaasClient.EXPECT().CreateSnapshot(gomock.Any(), "fake-snapshot", req.SourceVolumeId, properties).Return(expectedSnap, nil)
				iaasClient.EXPECT().WaitSnapshotReady(gomock.Any(), "fake-snapshot").Return(expectedSnap.Status, nil)

				// Actually create the backup from the snapshot
				iaasClient.EXPECT().CreateBackup(gomock.Any(), "fake-snapshot", req.GetSourceVolumeId(), "fake-snapshot", gomock.Any()).Return(expectedBackup, nil)
				iaasClient.EXPECT().WaitBackupReady(gomock.Any(), "fake-backup", *expectedSnap.Size, stackit.BackupMaxDurationSecondsPerGBDefault).
					Return(ptr.To("AVAILABLE"), nil)
				iaasClient.EXPECT().GetBackupByID(gomock.Any(), "fake-backup").Return(expectedBackup, nil)

				// Remove the snapshot after the backup is created
				iaasClient.EXPECT().DeleteSnapshot(gomock.Any(), "fake-snapshot").Return(nil)

				_, err := fakeCs.CreateSnapshot(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())
			})
			It("should skip snapshot creation when backup already exists", func() {
				expectedBackup := &iaas.Backup{
					Id:         ptr.To("fake-backup"),
					Name:       ptr.To("fake-backup"),
					Status:     ptr.To("AVAILABLE"),
					SnapshotId: ptr.To("fake-snapshot"),
					Size:       ptr.To(int64(10)),
					VolumeId:   ptr.To(req.GetSourceVolumeId()),
					CreatedAt:  ptr.To(time.Now()),
				}

				iaasClient.EXPECT().ListBackups(gomock.Any(), gomock.Any()).Return([]iaas.Backup{*expectedBackup}, nil)
				iaasClient.EXPECT().WaitBackupReady(gomock.Any(), "fake-backup", int64(0), stackit.BackupMaxDurationSecondsPerGBDefault).Return(ptr.To("AVAILABLE"), nil)
				iaasClient.EXPECT().GetBackupByID(gomock.Any(), "fake-backup").Return(expectedBackup, nil)

				// Remove the snapshot after the backup is created
				iaasClient.EXPECT().DeleteSnapshot(gomock.Any(), *expectedBackup.SnapshotId).Return(nil)

				_, err := fakeCs.CreateSnapshot(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())
			})
			It("should find multiple backups and report with error", func() {
				iaasClient.EXPECT().ListBackups(gomock.Any(), gomock.Any()).Return([]iaas.Backup{
					{
						Id: ptr.To("fake-snapshot"),
					},
					{
						Id: ptr.To("fake-snapshot2"),
					},
				}, nil)
				_, err := fakeCs.CreateSnapshot(context.Background(), req)
				Expect(err).To(HaveOccurred())
				Expect(status.Convert(err).Code()).To(Equal(codes.Internal))
				Expect(status.Convert(err).Message()).To(ContainSubstring("Multiple backups reported by Cinder with same name"))
			})
			It("should return error when backup is found with same name but different volSourceId", func() {
				expectedBackup := &iaas.Backup{
					Id:       ptr.To("fake-backup"),
					VolumeId: ptr.To("another-fake"),
				}

				iaasClient.EXPECT().ListBackups(gomock.Any(), gomock.Any()).Return([]iaas.Backup{*expectedBackup}, nil)
				_, err := fakeCs.CreateSnapshot(context.Background(), req)
				Expect(err).To(HaveOccurred())
				Expect(status.Convert(err).Code()).To(Equal(codes.AlreadyExists))
				Expect(status.Convert(err).Message()).To(ContainSubstring("Backup with given name already exists, with different source volume ID"))
			})
			It("should honor custom wait time for backup creation", func() {
				req.Parameters = map[string]string{
					stackit.BackupMaxDurationPerGB: "120",
					stackit.SnapshotType:           "backup",
				}

				customWaitTime, err := strconv.Atoi((req.Parameters)[stackit.BackupMaxDurationPerGB])
				Expect(err).To(Not(HaveOccurred()))

				// TODO: Use once IaaS has extended the label regex to allow for forward slashes and dots
				// properties := map[string]string{blockStorageCSIClusterIDKey: "cluster"}
				properties := map[string]string{}
				expectedSnap := &iaas.Snapshot{
					Id:        ptr.To("fake-snapshot"),
					Name:      ptr.To("fake-snapshot"),
					Status:    ptr.To("AVAILABLE"),
					Size:      ptr.To(int64(10)),
					CreatedAt: ptr.To(time.Now()),
				}
				expectedBackup := &iaas.Backup{
					Id:         ptr.To("fake-backup"),
					Name:       ptr.To("fake-backup"),
					Status:     ptr.To("AVAILABLE"),
					SnapshotId: ptr.To("fake-snapshot"),
					Size:       ptr.To(int64(10)),
					VolumeId:   ptr.To(req.GetSourceVolumeId()),
					CreatedAt:  ptr.To(time.Now()),
				}

				iaasClient.EXPECT().ListBackups(gomock.Any(), gomock.Any()).Return([]iaas.Backup{}, nil)

				// Backups are created from snapshots
				iaasClient.EXPECT().ListSnapshots(gomock.Any(), gomock.Any()).Return([]iaas.Snapshot{}, "", nil)
				iaasClient.EXPECT().CreateSnapshot(gomock.Any(), "fake-snapshot", req.SourceVolumeId, properties).Return(expectedSnap, nil)
				iaasClient.EXPECT().WaitSnapshotReady(gomock.Any(), "fake-snapshot").Return(expectedSnap.Status, nil)

				// Actually create the backup from the snapshot
				iaasClient.EXPECT().CreateBackup(gomock.Any(), "fake-snapshot", req.GetSourceVolumeId(), "fake-snapshot", gomock.Any()).Return(expectedBackup, nil)
				iaasClient.EXPECT().WaitBackupReady(gomock.Any(), "fake-backup", *expectedSnap.Size, customWaitTime).Return(ptr.To("AVAILABLE"), nil)
				iaasClient.EXPECT().GetBackupByID(gomock.Any(), "fake-backup").Return(expectedBackup, nil)

				// Remove the snapshot after the backup is created
				iaasClient.EXPECT().DeleteSnapshot(gomock.Any(), "fake-snapshot").Return(nil)

				_, err = fakeCs.CreateSnapshot(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())
			})
		})
		Context("Snapshot", func() {
			var req *csi.CreateSnapshotRequest
			JustBeforeEach(func() {
				req = &csi.CreateSnapshotRequest{
					SourceVolumeId: "fake",
					Name:           "fake-snapshot",
					Parameters:     stdSnapParams,
				}
			})
			It("should create snapshot successfully", func() {
				expectedSnap := &iaas.Snapshot{
					Id:        ptr.To("fake-snapshot"),
					Name:      ptr.To("fake-snapshot45"),
					VolumeId:  ptr.To("fake"),
					Size:      ptr.To(int64(10)),
					CreatedAt: ptr.To(time.Now()),
				}
				// TODO: Use once IaaS has extended the label regex to allow for forward slashes and dots
				// properties := map[string]string{blockStorageCSIClusterIDKey: "cluster"}
				properties := map[string]string{}

				// TODO: Again filters are not implemented yet by the API
				iaasClient.EXPECT().ListSnapshots(gomock.Any(), gomock.Any()).Return([]iaas.Snapshot{}, "", nil)
				iaasClient.EXPECT().CreateSnapshot(gomock.Any(), "fake-snapshot", req.SourceVolumeId, properties).Return(expectedSnap, nil)
				iaasClient.EXPECT().WaitSnapshotReady(gomock.Any(), "fake-snapshot").Return(expectedSnap.Status, nil)
				_, err := fakeCs.CreateSnapshot(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())
			})
			It("should return snapshot without creating a new one when already exists", func() {
				expectedSnap := &iaas.Snapshot{
					Id:        ptr.To("fake-snapshot"),
					Name:      ptr.To("fake-snapshot45"),
					VolumeId:  ptr.To("fake"),
					Status:    ptr.To("AVAILABLE"),
					Size:      ptr.To(int64(10)),
					CreatedAt: ptr.To(time.Now()),
				}

				// TODO: Again filters are not implemented yet by the API
				iaasClient.EXPECT().ListSnapshots(gomock.Any(), gomock.Any()).Return([]iaas.Snapshot{*expectedSnap}, "", nil)
				iaasClient.EXPECT().WaitSnapshotReady(gomock.Any(), "fake-snapshot").Return(ptr.To("AVAILABLE"), nil)
				_, err := fakeCs.CreateSnapshot(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())
			})
			It("should fail when we find more than one snapshot with the same name", func() {
				// TODO: Again filters are not implemented yet by the API
				iaasClient.EXPECT().ListSnapshots(gomock.Any(), gomock.Any()).Return([]iaas.Snapshot{
					{
						Id: ptr.To("fake-snapshot"),
					},
					{
						Id: ptr.To("fake-snapshot2"),
					},
				}, "", nil)
				_, err := fakeCs.CreateSnapshot(context.Background(), req)
				Expect(err).To(HaveOccurred())
				Expect(status.Convert(err).Code()).To(Equal(codes.Internal))
				Expect(status.Convert(err).Message()).To(ContainSubstring("Multiple snapshots reported by Cinder with same name"))
			})
			It("should fail when snapshot name already exists but with different volume id", func() {
				// TODO: Again filters are not implemented yet by the API
				iaasClient.EXPECT().ListSnapshots(gomock.Any(), gomock.Any()).Return([]iaas.Snapshot{
					{
						Id:       ptr.To("fake-snapshot"),
						VolumeId: ptr.To("something-different"),
					},
				}, "", nil)
				_, err := fakeCs.CreateSnapshot(context.Background(), req)
				Expect(err).To(HaveOccurred())
				Expect(status.Convert(err).Code()).To(Equal(codes.AlreadyExists))
				Expect(status.Convert(err).Message()).To(ContainSubstring("Snapshot with given name already exists, with different source volume ID"))
			})
		})
	})
	Describe("ListSnapshots", func() {
		It("should successfully list only one specific snapshot when SnapshotId in request != 0", func() {
			req := &csi.ListSnapshotsRequest{
				SnapshotId: "special-snapshot",
			}
			snapShotCreationTime := time.Now()
			expectedSnapshotListResponse := []*csi.ListSnapshotsResponse_Entry{
				{
					Snapshot: &csi.Snapshot{
						SnapshotId:     "special-snapshot",
						SizeBytes:      10 * util.GIBIBYTE,
						CreationTime:   timestamppb.New(snapShotCreationTime),
						SourceVolumeId: "fake",
						ReadyToUse:     true,
					},
				},
			}
			iaasClient.EXPECT().GetSnapshotByID(gomock.Any(), "special-snapshot").Return(&iaas.Snapshot{
				Id:        ptr.To("special-snapshot"),
				VolumeId:  ptr.To("fake"),
				Size:      ptr.To(int64(10)),
				CreatedAt: ptr.To(snapShotCreationTime),
			}, nil)
			resp, err := fakeCs.ListSnapshots(context.Background(), req)
			Expect(err).To(Not(HaveOccurred()))
			Expect(resp.GetEntries()).Should(Equal(expectedSnapshotListResponse))
			Expect(resp.GetEntries()).To(HaveLen(1))
		})
		It("should successfully list snapshots", func() {
			req := &csi.ListSnapshotsRequest{}

			snapShotCreationTime := time.Now()

			expectedSnapshotListResponse := []*csi.ListSnapshotsResponse_Entry{
				{
					Snapshot: &csi.Snapshot{
						SnapshotId:     "fake-snapshot",
						SizeBytes:      10 * util.GIBIBYTE,
						CreationTime:   timestamppb.New(snapShotCreationTime),
						SourceVolumeId: "something-different",
						ReadyToUse:     true,
					},
				},
			}

			iaasClient.EXPECT().ListSnapshots(gomock.Any(), gomock.Any()).Return([]iaas.Snapshot{{
				Id:        ptr.To("fake-snapshot"),
				VolumeId:  ptr.To("something-different"),
				Size:      ptr.To(int64(10)),
				CreatedAt: ptr.To(snapShotCreationTime),
			}}, "", nil)
			resp, err := fakeCs.ListSnapshots(context.Background(), req)
			Expect(err).To(Not(HaveOccurred()))
			Expect(resp.GetEntries()).Should(Equal(expectedSnapshotListResponse))
		})
	})
})
