package stackit

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/lbapi"
	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	"github.com/stackitcloud/stackit-sdk-go/services/loadbalancer"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider/api"
	"k8s.io/utils/ptr"
)

var notYetReadyError = api.NewRetryError("waiting for load balancer to become ready. This error is normal while the load balancer starts.", 10*time.Second).Error()

const (
	// stackitClassName defines the class name that deploys a STACKIT load balancer using the cloud controller manager.
	// Other classes are ignored by the cloud controller manager.
	classNameYawol = "yawol"

	sampleLBName         = "k8s-svc-89ec9a0e-6b00-4e2f-b57b-02e89193093d-echo"
	sampleCredentialsRef = "credentials-12345"
)

var _ = Describe("LoadBalancer", func() {
	var (
		mockClient              *lbapi.MockClient
		lbInModeIgnoreAndObs    *LoadBalancer
		lbInModeIgnore          *LoadBalancer
		lbInModeUpdate          *LoadBalancer
		lbInModeCreateAndUpdate *LoadBalancer
		clusterName             string
		projectID               string
		networkID               string
	)

	BeforeEach(func() {
		clusterName = "my-cluster"
		projectID = "my-project"
		networkID = "my-network"

		ctrl := gomock.NewController(GinkgoT())
		mockClient = lbapi.NewMockClient(ctrl)
		var err error
		lbInModeIgnoreAndObs, err = NewLoadBalancer(mockClient, projectID, networkID, nonStackitClassNameModeIgnore, &MetricsRemoteWrite{
			endpoint: "test-endpoint",
			username: "test-username",
			password: "test-password",
		})
		Expect(err).NotTo(HaveOccurred())
		lbInModeIgnore, err = NewLoadBalancer(mockClient, projectID, networkID, nonStackitClassNameModeIgnore, nil)
		Expect(err).NotTo(HaveOccurred())
		lbInModeUpdate, err = NewLoadBalancer(mockClient, projectID, networkID, nonStackitClassNameModeUpdate, nil)
		Expect(err).NotTo(HaveOccurred())
		lbInModeCreateAndUpdate, err = NewLoadBalancer(mockClient, projectID, networkID, nonStackitClassNameModeUpdateAndCreate, nil)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("GetLoadBalancerName", func() {
		It("should generate the name based on the UID and name", func() {
			name := lbInModeIgnore.GetLoadBalancerName(context.Background(), clusterName, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID:  "00000000-0000-0000-0000-000000000000",
					Name: "my-load-balancer",
				},
			})
			Expect(name).To(Equal("k8s-svc-00000000-0000-0000-0000-000000000000-my-load-balancer"))
		})

		It("should truncate names that are too long", func() {
			name := lbInModeIgnore.GetLoadBalancerName(context.Background(), clusterName, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID:  "00000000-0000-0000-0000-000000000000",
					Name: "lb-tooooo-long-name",
				},
			})
			Expect(name).To(HaveLen(63))
			Expect(name).To(Equal("k8s-svc-00000000-0000-0000-0000-000000000000-lb-tooooo-long-nam"))
		})

		It("should not truncate names that are exactly 63 chars long", func() {
			name := lbInModeIgnore.GetLoadBalancerName(context.Background(), clusterName, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID:  "00000000-0000-0000-0000-000000000000",
					Name: "name-exactly-right",
				},
			})
			Expect(name).To(HaveLen(63))
			Expect(name).To(Equal("k8s-svc-00000000-0000-0000-0000-000000000000-name-exactly-right"))
		})

		It("should produce DNS-compatible names by removing trailing dashes", func() {
			name := lbInModeIgnore.GetLoadBalancerName(context.Background(), clusterName, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID:  "00000000-0000-0000-0000-000000000000",
					Name: "ske-meets-stackit-lb",
				},
			})
			Expect(name).To(HaveLen(62))
			Expect(name).To(Equal("k8s-svc-00000000-0000-0000-0000-000000000000-ske-meets-stackit"))
		})
	})

	Describe("GetLoadBalancer", func() {
		It("should report lb does not exist for non-STACKIT class name mode \"ignore\"", func() {
			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			_, exists, err := lbInModeIgnore.GetLoadBalancer(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
			// Expect no API call to have occurred. Gomock panics on non-declared calls.
		})

		It("should report lb does not exist for empty class name in mode \"ignore\"", func() {
			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			_, exists, err := lbInModeIgnore.GetLoadBalancer(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
			// Expect no API call to have occurred. Gomock panics on non-declared calls.
		})

		It("should report LB does not exist for non-STACKIT class name mode \"update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)

			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			_, exists, err := lbInModeUpdate.GetLoadBalancer(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("should report LB does not exist for empty class name in mode \"update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)

			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			_, exists, err := lbInModeUpdate.GetLoadBalancer(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("should report LB does not exist for non-STACKIT class name mode \"create & update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)

			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			_, exists, err := lbInModeCreateAndUpdate.GetLoadBalancer(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("should report LB does not exist for empty class name in mode \"create & update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)

			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			_, exists, err := lbInModeCreateAndUpdate.GetLoadBalancer(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		convertToLB := func(spec *loadbalancer.CreateLoadBalancerPayload) *loadbalancer.LoadBalancer {
			return &loadbalancer.LoadBalancer{
				Errors:          &[]loadbalancer.LoadBalancerError{},
				ExternalAddress: spec.ExternalAddress,
				Listeners:       spec.Listeners,
				Name:            spec.Name,
				Networks:        spec.Networks,
				Options:         spec.Options,
				Status:          ptr.To(lbapi.LBStatusReady),
				TargetPools:     spec.TargetPools,
				Version:         ptr.To("current-version"),
			}
		}

		DescribeTable("should report status for external LB",
			func(hasExternalAddress bool) {
				svc := minimalLoadBalancerService()
				spec, _, err := lbSpecFromService(svc, []*corev1.Node{}, networkID, nil)
				Expect(err).NotTo(HaveOccurred())
				myLb := convertToLB(spec)
				if !hasExternalAddress {
					// LB has no external address yet
					myLb.ExternalAddress = nil
				}
				mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(myLb, nil)

				status, found, err := lbInModeCreateAndUpdate.GetLoadBalancer(context.Background(), clusterName, svc)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				if hasExternalAddress {
					Expect(status.Ingress).To(Equal([]corev1.LoadBalancerIngress{
						{IP: *spec.ExternalAddress},
					}))
				} else {
					Expect(status.Ingress).To(BeNil())
				}
			},
			Entry("when externalAddress is set", true),
			Entry("when externalAddress is not set", false),
		)

		DescribeTable("should report status for internal LB",
			func(hasPrivateAddress bool) {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						UID: "00000000-0000-0000-0000-000000000000",
						Annotations: map[string]string{
							"lb.stackit.cloud/internal-lb":  "true",
							"yawol.stackit.cloud/className": classNameStackit,
						},
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
					},
				}
				spec, _, err := lbSpecFromService(svc, []*corev1.Node{}, networkID, nil)
				Expect(err).NotTo(HaveOccurred())
				myLb := convertToLB(spec)
				Expect(myLb.ExternalAddress).To(BeNil())
				if hasPrivateAddress {
					myLb.PrivateAddress = ptr.To("10.20.30.40")
				}
				mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(myLb, nil)

				status, found, err := lbInModeCreateAndUpdate.GetLoadBalancer(context.Background(), clusterName, svc)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				if hasPrivateAddress {
					Expect(status.Ingress).To(Equal([]corev1.LoadBalancerIngress{
						{IP: *myLb.PrivateAddress},
					}))
				} else {
					Expect(status.Ingress).To(BeNil())
				}
			},
			Entry("when PrivateAddress is set", true),
			Entry("when PrivateAddress is not set", false),
		)
	})

	Describe("EnsureLoadBalancer", func() {
		It("should report implemented elsewhere for non-STACKIT class name mode \"ignore\"", func() {
			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			_, err := lbInModeIgnore.EnsureLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).To(MatchError(cloudprovider.ImplementedElsewhere))
		})

		It("should report implemented elsewhere for empty class name in mode \"ignore\"", func() {
			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			_, err := lbInModeIgnore.EnsureLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).To(MatchError(cloudprovider.ImplementedElsewhere))
		})

		It("should report implemented elsewhere for non-STACKIT class name mode \"update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)

			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			_, err := lbInModeUpdate.EnsureLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).To(MatchError(cloudprovider.ImplementedElsewhere))
		})

		It("should report implemented elsewhere for empty class name in mode \"update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)

			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			_, err := lbInModeUpdate.EnsureLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).To(MatchError(cloudprovider.ImplementedElsewhere))
		})

		It("ensure load balancer should trigger load balancer creation if LB doesn't exist", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)
			mockClient.EXPECT().CreateLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(&loadbalancer.LoadBalancer{}, nil)

			_, err := lbInModeIgnore.EnsureLoadBalancer(context.Background(), clusterName, minimalLoadBalancerService(), []*corev1.Node{})
			Expect(err).To(MatchError(notYetReadyError))
			// Expected CreateLoadBalancer to have been called.
		})

		It("should create a load balancer with observability configured", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)
			mockClient.EXPECT().ListCredentials(gomock.Any(), projectID).Return(&loadbalancer.ListCredentialsResponse{
				Credentials: &[]loadbalancer.CredentialsResponse{},
			}, nil)
			// TODO: match payload
			mockClient.EXPECT().CreateCredentials(gomock.Any(), projectID, gomock.Any()).MinTimes(1).
				DoAndReturn(func(ctx context.Context, projectID string, payload loadbalancer.CreateCredentialsPayload) (*loadbalancer.CreateCredentialsResponse, error) {
					return &loadbalancer.CreateCredentialsResponse{
						Credential: &loadbalancer.CredentialsResponse{
							CredentialsRef: utils.Ptr("my-credential-ref"),
							DisplayName:    utils.Ptr(*payload.DisplayName),
							Username:       utils.Ptr(*payload.Username),
						},
					}, nil
				})
			mockClient.EXPECT().CreateLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(&loadbalancer.LoadBalancer{}, nil)

			_, err := lbInModeIgnoreAndObs.EnsureLoadBalancer(context.Background(), clusterName, minimalLoadBalancerService(), []*corev1.Node{})
			Expect(err).To(MatchError(notYetReadyError))
			// Expected CreateCredentials to have been called.
			// Expected CreateLoadBalancer to have been called.
		})

		It("should update observability credential if credentials are specified in load balancer", func() {
			svc := minimalLoadBalancerService()
			spec, _, err := lbSpecFromService(svc, []*corev1.Node{}, networkID, &loadbalancer.LoadbalancerOptionObservability{
				Metrics: &loadbalancer.LoadbalancerOptionMetrics{
					CredentialsRef: utils.Ptr(sampleCredentialsRef),
					PushUrl:        &lbInModeIgnoreAndObs.metricsRemoteWrite.endpoint,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			myLb := &loadbalancer.LoadBalancer{
				Errors:          &[]loadbalancer.LoadBalancerError{},
				ExternalAddress: spec.ExternalAddress,
				Listeners:       spec.Listeners,
				Name:            spec.Name,
				Networks:        spec.Networks,
				Options:         spec.Options,
				PrivateAddress:  spec.PrivateAddress,
				Status:          ptr.To(lbapi.LBStatusReady),
				TargetPools:     spec.TargetPools,
				Version:         ptr.To("current-version"),
				PlanId:          ptr.To("p10"),
			}

			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(myLb, nil)
			mockClient.EXPECT().UpdateCredentials(gomock.Any(), projectID, sampleCredentialsRef, gomock.Any()).MinTimes(1).Return(nil)

			_, err = lbInModeIgnoreAndObs.EnsureLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).NotTo(HaveOccurred())
			// Expected UpdateCredentials to have been called.
		})

		It("ensure load balancer should trigger load balancer creation if LB doesn't exist for non-STACKIT class name mode \"create & update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)
			mockClient.EXPECT().CreateLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(&loadbalancer.LoadBalancer{}, nil)

			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			_, err := lbInModeCreateAndUpdate.EnsureLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).To(MatchError(notYetReadyError))
		})

		It("ensure load balancer should trigger load balancer creation if LB doesn't exist for empty class name in mode \"create & update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)
			mockClient.EXPECT().CreateLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(&loadbalancer.LoadBalancer{}, nil)

			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			_, err := lbInModeCreateAndUpdate.EnsureLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).To(MatchError(notYetReadyError))
		})

		It("should update the load balancer if the service changed", func() {
			svc := minimalLoadBalancerService()
			spec, _, err := lbSpecFromService(svc, []*corev1.Node{}, networkID, nil)
			Expect(err).NotTo(HaveOccurred())
			myLb := &loadbalancer.LoadBalancer{
				Errors:          &[]loadbalancer.LoadBalancerError{},
				ExternalAddress: spec.ExternalAddress,
				Listeners:       spec.Listeners,
				Name:            spec.Name,
				Networks:        spec.Networks,
				Options:         spec.Options,
				PrivateAddress:  spec.PrivateAddress,
				Status:          ptr.To(lbapi.LBStatusReady),
				TargetPools:     spec.TargetPools,
				Version:         ptr.To("current-version"),
			}

			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(myLb, nil)
			// For simplicity, we return the original load balancer. In reality, the updated load balancer should be returned.
			mockClient.EXPECT().UpdateLoadBalancer(
				gomock.Any(),
				projectID,
				lbInModeIgnore.GetLoadBalancerName(context.Background(), clusterName, svc),
				versionMatcher("current-version"),
			).MinTimes(1).Return(myLb, nil)

			svc = svc.DeepCopy()
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:     "a-port",
				Protocol: corev1.ProtocolTCP,
				Port:     80,
				NodePort: 1234,
			})

			_, err = lbInModeIgnore.EnsureLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).NotTo(HaveOccurred())
			// Expect UpdateLoadBalancer to have been called.
		})

		// This only happens when nodes have changed while the controller wasn't running.
		// If the controller is watching, then UpdateLoadBalancer is called instead.
		It("should update the load balancer if the nodes change", func() {
			nodeA := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "nodeA"},
				// Nodes need an internal address, otherwise they will be ignored.
				Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.1"}}},
			}
			nodeB := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "nodeB"},
				Status:     corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.1"}}},
			}

			svc := minimalLoadBalancerService()
			// We need at least one port for nodes to have an effect.
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:     "a-port",
				Protocol: corev1.ProtocolTCP,
				Port:     80,
				NodePort: 1234,
			})
			spec, _, err := lbSpecFromService(svc, []*corev1.Node{nodeA}, networkID, nil)
			Expect(err).NotTo(HaveOccurred())
			myLb := &loadbalancer.LoadBalancer{
				Errors:          &[]loadbalancer.LoadBalancerError{},
				ExternalAddress: spec.ExternalAddress,
				Listeners:       spec.Listeners,
				Name:            spec.Name,
				Networks:        spec.Networks,
				Options:         spec.Options,
				PrivateAddress:  spec.PrivateAddress,
				Status:          ptr.To(lbapi.LBStatusReady),
				TargetPools:     spec.TargetPools,
				Version:         ptr.To("current-version"),
			}

			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(myLb, nil)
			// For simplicity, we return the original load balancer. In reality, the updated load balancer should be returned.
			mockClient.EXPECT().UpdateLoadBalancer(
				gomock.Any(),
				projectID,
				lbInModeIgnore.GetLoadBalancerName(context.Background(), clusterName, svc),
				versionMatcher("current-version"),
			).MinTimes(1).Return(myLb, nil)

			_, err = lbInModeIgnore.EnsureLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{nodeA, nodeB})
			Expect(err).NotTo(HaveOccurred())
			// Expect UpdateLoadBalancer to have been called.
		})

		It("should delete observability credentials and delete reference from load balancer if controller is not configured (monitoring extension disabled)", func() {
			svc := minimalLoadBalancerService()
			spec, _, err := lbSpecFromService(svc, []*corev1.Node{}, networkID, &loadbalancer.LoadbalancerOptionObservability{
				Metrics: &loadbalancer.LoadbalancerOptionMetrics{
					CredentialsRef: ptr.To(sampleCredentialsRef),
					PushUrl:        ptr.To("test-endpoint"),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			myLb := &loadbalancer.LoadBalancer{
				Errors:          &[]loadbalancer.LoadBalancerError{},
				ExternalAddress: spec.ExternalAddress,
				Listeners:       spec.Listeners,
				Name:            spec.Name,
				Networks:        spec.Networks,
				Options:         spec.Options,
				PrivateAddress:  spec.PrivateAddress,
				Status:          ptr.To(lbapi.LBStatusReady),
				TargetPools:     spec.TargetPools,
				Version:         ptr.To("current-version"),
			}

			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(myLb, nil)
			// Check order to ensure that the reference is removed before the credentials are removed.
			// The API rejects deletions of used credentials.
			gomock.InOrder(
				// For simplicity, we return the original load balancer. In reality, the updated load balancer should be returned.
				mockClient.EXPECT().UpdateLoadBalancer(
					gomock.Any(),
					projectID,
					lbInModeIgnore.GetLoadBalancerName(context.Background(), clusterName, svc),
					gomock.All(
						versionMatcher("current-version"),
						hasNoObservabilityConfigured(),
					),
				).MinTimes(1).Return(myLb, nil),
				mockClient.EXPECT().DeleteCredentials(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(nil),
			)

			_, err = lbInModeIgnore.EnsureLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).NotTo(HaveOccurred())
			// Expect UpdateLoadBalancer to have been called.
			// Expect DeleteCredentials to have been called.
		})
	})

	Describe("EnsureLoadBalancerDeleted", func() {
		It("should trigger load balancer deletion", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(&loadbalancer.LoadBalancer{}, nil)
			mockClient.EXPECT().ListCredentials(gomock.Any(), projectID).Return(&loadbalancer.ListCredentialsResponse{
				Credentials: &[]loadbalancer.CredentialsResponse{},
			}, nil)
			mockClient.EXPECT().DeleteLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(nil)

			err := lbInModeIgnore.EnsureLoadBalancerDeleted(context.Background(), clusterName, minimalLoadBalancerService())
			Expect(err).NotTo(HaveOccurred())
			// Expect DeleteLoadBalancer to have been called.
		})

		It("should finalize deletion if LB API returns not found", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)

			err := lbInModeIgnore.EnsureLoadBalancerDeleted(context.Background(), clusterName, minimalLoadBalancerService())
			Expect(err).NotTo(HaveOccurred())
			// Expect DeleteLoadBalancer not to have been called.
		})

		It("should finalize deletion if load balancer is state terminating", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(&loadbalancer.LoadBalancer{
				Status: utils.Ptr(lbapi.LBStatusTerminating),
			}, nil)

			err := lbInModeIgnore.EnsureLoadBalancerDeleted(context.Background(), clusterName, minimalLoadBalancerService())
			Expect(err).NotTo(HaveOccurred())
			// Expect DeleteLoadBalancer not to have been called.
		})

		It("should report implemented elsewhere for non-STACKIT class name mode \"ignore\"", func() {
			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			err := lbInModeIgnore.EnsureLoadBalancerDeleted(context.Background(), clusterName, svc)
			Expect(err).To(MatchError(cloudprovider.ImplementedElsewhere))
		})

		It("should report implemented elsewhere for empty class name in mode \"ignore\"", func() {
			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			err := lbInModeIgnore.EnsureLoadBalancerDeleted(context.Background(), clusterName, svc)
			Expect(err).To(MatchError(cloudprovider.ImplementedElsewhere))
		})

		It("should report no error if LB not found for non-STACKIT class name mode \"update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)

			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			err := lbInModeUpdate.EnsureLoadBalancerDeleted(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should report no error if LB not found for empty class name in mode \"update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)

			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			err := lbInModeUpdate.EnsureLoadBalancerDeleted(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should trigger load balancer deletion for non-STACKIT class name mode \"create & update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(&loadbalancer.LoadBalancer{}, nil)
			mockClient.EXPECT().ListCredentials(gomock.Any(), projectID).Return(&loadbalancer.ListCredentialsResponse{
				Credentials: &[]loadbalancer.CredentialsResponse{},
			}, nil)
			mockClient.EXPECT().DeleteLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(nil)

			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			err := lbInModeCreateAndUpdate.EnsureLoadBalancerDeleted(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
			// Expect DeleteLoadBalancer to have been called.
		})

		It("should trigger load balancer deletion for empty class name in mode \"create & update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(&loadbalancer.LoadBalancer{}, nil)
			mockClient.EXPECT().ListCredentials(gomock.Any(), projectID).Return(&loadbalancer.ListCredentialsResponse{
				Credentials: &[]loadbalancer.CredentialsResponse{},
			}, nil)
			mockClient.EXPECT().DeleteLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(nil)

			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			err := lbInModeCreateAndUpdate.EnsureLoadBalancerDeleted(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
			// Expect DeleteLoadBalancer to have been called.
		})

		It("should delete observability credentials of load balancer with static IP", func() {
			svc := minimalLoadBalancerService()
			name := lbInModeCreateAndUpdate.GetLoadBalancerName(context.Background(), "", svc)

			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(&loadbalancer.LoadBalancer{
				Options: &loadbalancer.LoadBalancerOptions{
					Observability: &loadbalancer.LoadbalancerOptionObservability{
						Metrics: &loadbalancer.LoadbalancerOptionMetrics{
							CredentialsRef: ptr.To(sampleCredentialsRef),
							PushUrl:        ptr.To("http://localhost"),
						},
					},
					EphemeralAddress: ptr.To(false),
				},
				ExternalAddress: ptr.To("8.8.4.4"),
				Listeners:       &[]loadbalancer.Listener{},
			}, nil)
			gomock.InOrder(
				mockClient.EXPECT().UpdateLoadBalancer(gomock.Any(), projectID, name, gomock.All(
					hasNoObservabilityConfigured(), externalAddressSet("8.8.4.4"),
				)).MinTimes(1).Return(&loadbalancer.LoadBalancer{}, nil),
				mockClient.EXPECT().DeleteCredentials(gomock.Any(), projectID, sampleCredentialsRef).MinTimes(1).Return(nil),
				mockClient.EXPECT().ListCredentials(gomock.Any(), projectID).Return(&loadbalancer.ListCredentialsResponse{
					Credentials: &[]loadbalancer.CredentialsResponse{},
				}, nil),
				mockClient.EXPECT().DeleteLoadBalancer(gomock.Any(), projectID, name).MinTimes(1).Return(nil),
			)

			err := lbInModeCreateAndUpdate.EnsureLoadBalancerDeleted(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should delete observability credentials of load balancer with ephemeral IP", func() {
			svc := minimalLoadBalancerService()
			// Ensure load balancer is ephemeral.
			delete(svc.Annotations, externalIPAnnotation)
			name := lbInModeCreateAndUpdate.GetLoadBalancerName(context.Background(), "", svc)

			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(&loadbalancer.LoadBalancer{
				Options: &loadbalancer.LoadBalancerOptions{
					Observability: &loadbalancer.LoadbalancerOptionObservability{
						Metrics: &loadbalancer.LoadbalancerOptionMetrics{
							CredentialsRef: ptr.To(sampleCredentialsRef),
							PushUrl:        ptr.To("http://localhost"),
						},
					},
					EphemeralAddress: ptr.To(true),
				},
				ExternalAddress: ptr.To("0.0.0.0 (ephemeral)"),
				Listeners:       &[]loadbalancer.Listener{},
			}, nil)
			gomock.InOrder(
				mockClient.EXPECT().UpdateLoadBalancer(gomock.Any(), projectID, name, gomock.All(
					hasNoObservabilityConfigured(), externalAddressNotSet(), ephemeralAddress(),
				)).MinTimes(1).Return(&loadbalancer.LoadBalancer{}, nil),
				mockClient.EXPECT().DeleteCredentials(gomock.Any(), projectID, sampleCredentialsRef).MinTimes(1).Return(nil),
				mockClient.EXPECT().ListCredentials(gomock.Any(), projectID).Return(&loadbalancer.ListCredentialsResponse{
					Credentials: &[]loadbalancer.CredentialsResponse{},
				}, nil),
				mockClient.EXPECT().DeleteLoadBalancer(gomock.Any(), projectID, name).MinTimes(1).Return(nil),
			)

			err := lbInModeCreateAndUpdate.EnsureLoadBalancerDeleted(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("UpdateLoadBalancer", func() {
		It("should update targets", func() { //nolint:dupl // the class name is stackit
			mockClient.EXPECT().UpdateTargetPool(gomock.Any(), projectID, gomock.Any(), "my-port", gomock.Any()).MinTimes(1)

			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address": "123.124.88.99",
						"yawol.stackit.cloud/className":     classNameStackit,
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-port",
							Protocol: corev1.ProtocolTCP,
							Port:     80,
							NodePort: 8080,
						},
					},
				},
			}
			err := lbInModeIgnore.UpdateLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})

			Expect(err).NotTo(HaveOccurred())
			// Expect UpdateTargetPool to have been called.
		})

		It("should report implemented elsewhere for non-STACKIT class name mode \"ignore\"", func() {
			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			err := lbInModeIgnore.UpdateLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).To(MatchError(cloudprovider.ImplementedElsewhere))
		})

		It("should report implemented elsewhere for empty class name in mode \"ignore\"", func() {
			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			err := lbInModeIgnore.UpdateLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).To(MatchError(cloudprovider.ImplementedElsewhere))
		})

		It("should return no error if LB not found for non-STACKIT class name mode \"update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)

			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			err := lbInModeUpdate.UpdateLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).To(MatchError(cloudprovider.ImplementedElsewhere))
		})

		It("should return no error if LB not found for empty class name in mode \"update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)

			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			err := lbInModeUpdate.UpdateLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).To(MatchError(cloudprovider.ImplementedElsewhere))
		})

		It("should update targets for non-STACKIT class name mode \"create & update\"", func() { //nolint:dupl // the class name is yawol
			mockClient.EXPECT().UpdateTargetPool(gomock.Any(), projectID, gomock.Any(), "my-port", gomock.Any()).MinTimes(1)

			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address": "123.124.88.99",
						"yawol.stackit.cloud/className":     classNameYawol,
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-port",
							Protocol: corev1.ProtocolTCP,
							Port:     80,
							NodePort: 8080,
						},
					},
				},
			}
			err := lbInModeCreateAndUpdate.UpdateLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})

			Expect(err).NotTo(HaveOccurred())
			// Expect UpdateTargetPool to have been called.
		})

		It("should update targets for empty class name in mode \"create & update\"", func() {
			mockClient.EXPECT().UpdateTargetPool(gomock.Any(), projectID, gomock.Any(), "my-port", gomock.Any()).MinTimes(1)

			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address": "123.124.88.99",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-port",
							Protocol: corev1.ProtocolTCP,
							Port:     80,
							NodePort: 8080,
						},
					},
				},
			}
			err := lbInModeCreateAndUpdate.UpdateLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})

			Expect(err).NotTo(HaveOccurred())
			// Expect UpdateTargetPool to have been called.
		})
	})

	Describe("reconcileObservabilityCredentials", func() {
		It("should do nothing if no credentials are in the environment", func() {
			credentialRef, err := lbInModeIgnore.reconcileObservabilityCredentials(context.Background(), nil, "my-loadbalancer")
			Expect(err).NotTo(HaveOccurred())
			Expect(credentialRef).To(BeNil())
		})

		It("should update credentials if they exist", func() {
			pushURL := "test-endpoint"
			mockClient.EXPECT().UpdateCredentials(gomock.Any(), projectID, sampleCredentialsRef, gomock.Any()).MinTimes(1).Return(nil)
			credentialRef, err := lbInModeIgnoreAndObs.reconcileObservabilityCredentials(context.Background(), &loadbalancer.LoadBalancer{
				Name: ptr.To(sampleLBName),
				Options: &loadbalancer.LoadBalancerOptions{
					Observability: &loadbalancer.LoadbalancerOptionObservability{
						Metrics: &loadbalancer.LoadbalancerOptionMetrics{
							CredentialsRef: ptr.To(sampleCredentialsRef),
						},
					},
				},
			}, sampleLBName)
			Expect(err).NotTo(HaveOccurred())
			Expect(*credentialRef).To(Equal(loadbalancer.LoadbalancerOptionObservability{
				Metrics: &loadbalancer.LoadbalancerOptionMetrics{
					CredentialsRef: ptr.To(sampleCredentialsRef),
					PushUrl:        &pushURL,
				},
			}))
		})

		It("should try to update credentials if they exist", func() {
			errTest := errors.New("update credentials test error")
			mockClient.EXPECT().UpdateCredentials(gomock.Any(), projectID, sampleCredentialsRef, gomock.Any()).MinTimes(1).Return(errTest)
			credentialRef, err := lbInModeIgnoreAndObs.reconcileObservabilityCredentials(context.Background(), &loadbalancer.LoadBalancer{
				Name: ptr.To(sampleLBName),
				Options: &loadbalancer.LoadBalancerOptions{
					Observability: &loadbalancer.LoadbalancerOptionObservability{
						Metrics: &loadbalancer.LoadbalancerOptionMetrics{
							CredentialsRef: ptr.To(sampleCredentialsRef),
						},
					},
				},
			}, sampleLBName)
			Expect(err).To(MatchError(errTest))
			Expect(credentialRef).To(BeNil())
		})

		It("should create credentials if they do not exist", func() {
			mockClient.EXPECT().ListCredentials(gomock.Any(), projectID).Return(&loadbalancer.ListCredentialsResponse{
				Credentials: &[]loadbalancer.CredentialsResponse{},
			}, nil)
			mockClient.EXPECT().CreateCredentials(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(&loadbalancer.CreateCredentialsResponse{
				Credential: &loadbalancer.CredentialsResponse{
					CredentialsRef: ptr.To(sampleCredentialsRef),
					DisplayName:    ptr.To(sampleLBName),
					Username:       ptr.To("test-username"),
				},
			}, nil)
			credentialRef, err := lbInModeIgnoreAndObs.reconcileObservabilityCredentials(context.Background(), &loadbalancer.LoadBalancer{
				Name: ptr.To(sampleLBName),
			}, sampleLBName)
			Expect(err).NotTo(HaveOccurred())
			Expect(*credentialRef).To(Equal(loadbalancer.LoadbalancerOptionObservability{
				Metrics: &loadbalancer.LoadbalancerOptionMetrics{
					CredentialsRef: ptr.To(sampleCredentialsRef),
					PushUrl:        ptr.To("test-endpoint"),
				},
			}))
		})

		It("should return error if creating new credentials fails", func() {
			mockClient.EXPECT().ListCredentials(gomock.Any(), projectID).Return(&loadbalancer.ListCredentialsResponse{
				Credentials: &[]loadbalancer.CredentialsResponse{},
			}, nil)
			errTest := errors.New("delete credentials test error")
			mockClient.EXPECT().CreateCredentials(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(nil, errTest)
			credentialRef, err := lbInModeIgnoreAndObs.reconcileObservabilityCredentials(context.Background(), &loadbalancer.LoadBalancer{
				Name: ptr.To(sampleLBName),
			}, sampleLBName)
			Expect(err).To(MatchError(errTest))
			Expect(credentialRef).To(BeNil())
		})
	})

	Describe("cleanUpCredentials", func() {
		It("should delete matching and only matching observability credentials", func() {
			gomock.InOrder(
				mockClient.EXPECT().ListCredentials(gomock.Any(), projectID).Return(&loadbalancer.ListCredentialsResponse{
					Credentials: &[]loadbalancer.CredentialsResponse{
						{
							CredentialsRef: ptr.To("matching-1"),
							DisplayName:    ptr.To("my-loadbalancer"),
							Username:       ptr.To("luke"),
						},
						{
							CredentialsRef: ptr.To("display-name-not-match"),
							DisplayName:    ptr.To("other-loadbalancer"),
							Username:       ptr.To("leia"),
						},
						{
							CredentialsRef: ptr.To("matching-2"),
							DisplayName:    ptr.To("my-loadbalancer"),
							Username:       ptr.To("chewie"),
						},
						{
							CredentialsRef: ptr.To("no-display-name"),
							DisplayName:    nil,
							Username:       ptr.To("han"),
						},
					},
				}, nil).MinTimes(1),
				mockClient.EXPECT().DeleteCredentials(gomock.Any(), projectID, "matching-1").MinTimes(1),
				mockClient.EXPECT().DeleteCredentials(gomock.Any(), projectID, "matching-2").MinTimes(1),
			)
			Expect(lbInModeIgnoreAndObs.cleanUpCredentials(context.Background(), "my-loadbalancer")).To(Succeed())
		})
	})
})

var _ = DescribeTable("loadBalancerStatus",
	func(lb *loadbalancer.LoadBalancer, svc *corev1.Service, expect *corev1.LoadBalancerStatus) {
		Expect(loadBalancerStatus(lb, svc)).To(Equal(expect))
	},
	Entry("empty address", &loadbalancer.LoadBalancer{}, &corev1.Service{}, &corev1.LoadBalancerStatus{}),
	Entry("address present",
		&loadbalancer.LoadBalancer{ExternalAddress: ptr.To("1.2.3.4")}, &corev1.Service{},
		&corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}}},
	),
	Entry("IP mode proxy",
		&loadbalancer.LoadBalancer{ExternalAddress: ptr.To("1.2.3.4")},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{ipModeProxyAnnotation: "true"}}},
		&corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4", IPMode: ptr.To(corev1.LoadBalancerIPModeProxy)}}},
	),
)

// minimalLoadBalancerService returns a service that is valid for provisioning a load balancer by the CCM.
// It should be used in tests that don't expect a particular configuration.
func minimalLoadBalancerService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			UID: "00000000-0000-0000-0000-000000000000",
			Annotations: map[string]string{
				"lb.stackit.cloud/external-address": "123.124.88.99",
				"yawol.stackit.cloud/className":     classNameStackit,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		},
	}
}

// versionMatcher ensures that the given UpdateLoadBalancerPayload has version specified.
func versionMatcher(version string) gomock.Matcher {
	return gomock.Cond(func(x any) bool {
		lb := x.(*loadbalancer.UpdateLoadBalancerPayload)
		if lb.Version == nil {
			return false
		}
		return *lb.Version == version
	})
}

// hasNoObservabilityConfigured ensures that the given UpdateLoadBalancerPayload has no observability specified.
func hasNoObservabilityConfigured() gomock.Matcher {
	return gomock.Cond(func(x any) bool {
		lb := x.(*loadbalancer.UpdateLoadBalancerPayload)
		return lb.Options == nil || lb.Options.Observability == nil
	})
}

// externalAddressSet ensures that the given UpdateLoadBalancerPayload has external address set to address.
func externalAddressSet(address string) gomock.Matcher {
	return gomock.Cond(func(x any) bool {
		lb := x.(*loadbalancer.UpdateLoadBalancerPayload)
		return lb.ExternalAddress != nil && *lb.ExternalAddress == address
	})
}

// externalAddressNotSet ensures that the given UpdateLoadBalancerPayload has no external address set.
func externalAddressNotSet() gomock.Matcher {
	return gomock.Cond(func(x any) bool {
		lb := x.(*loadbalancer.UpdateLoadBalancerPayload)
		return lb.ExternalAddress == nil
	})
}

// ephemeralAddress ensures that the given UpdateLoadBalancerPayload has an ephemeral address enabled.
func ephemeralAddress() gomock.Matcher {
	return gomock.Cond(func(x any) bool {
		lb := x.(*loadbalancer.UpdateLoadBalancerPayload)
		return lb.Options != nil && lb.Options.EphemeralAddress != nil && *lb.Options.EphemeralAddress
	})
}
