package stackit

import (
	"context"
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

const (
	// stackitClassName defines the class name that deploys a STACKIT load balancer using the cloud controller manager.
	// Other classes are ignored by the cloud controller manager.
	classNameYawol = "yawol"
)

var _ = Describe("LoadBalancer", func() {
	var (
		mockClient              *lbapi.MockClient
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
		lbInModeIgnore, err = NewLoadBalancer(mockClient, projectID, networkID, nonStackitClassNameModeIgnore)
		Expect(err).NotTo(HaveOccurred())
		lbInModeUpdate, err = NewLoadBalancer(mockClient, projectID, networkID, nonStackitClassNameModeUpdate)
		Expect(err).NotTo(HaveOccurred())
		lbInModeCreateAndUpdate, err = NewLoadBalancer(mockClient, projectID, networkID, nonStackitClassNameModeUpdateAndCreate)
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
			Expect(err).To(MatchError(api.NewRetryError("waiting for load balancer to become ready", 10*time.Second)))
			// Expected CreateLoadBalancer to have been called.
		})

		It("ensure load balancer should trigger load balancer creation if LB doesn't exist for non-STACKIT class name mode \"create & update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)
			mockClient.EXPECT().CreateLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(&loadbalancer.LoadBalancer{}, nil)

			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			_, err := lbInModeCreateAndUpdate.EnsureLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).To(MatchError(api.NewRetryError("waiting for load balancer to become ready", 10*time.Second)))
		})

		It("ensure load balancer should trigger load balancer creation if LB doesn't exist for empty class name in mode \"create & update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)
			mockClient.EXPECT().CreateLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(&loadbalancer.LoadBalancer{}, nil)

			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			_, err := lbInModeCreateAndUpdate.EnsureLoadBalancer(context.Background(), clusterName, svc, []*corev1.Node{})
			Expect(err).To(MatchError(api.NewRetryError("waiting for load balancer to become ready", 10*time.Second)))
		})

		It("should enable the project if creating load balancer returns not found", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)
			mockClient.EXPECT().CreateLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(nil, lbapi.ErrorNotFound)
			mockClient.EXPECT().GetServiceStatus(gomock.Any(), projectID).Return(lbapi.ProjectStatusDisabled, nil)
			mockClient.EXPECT().EnableService(gomock.Any(), projectID).MinTimes(1).Return(nil)

			_, err := lbInModeIgnore.EnsureLoadBalancer(context.Background(), clusterName, minimalLoadBalancerService(), []*corev1.Node{})
			Expect(err).To(MatchError(api.NewRetryError("waiting for project to become ready after enabling", 10*time.Second)))
			// Expect EnableService to have been called.
		})

		It("should return error if project is not deactivated but load balancer creation returns not found", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(nil, lbapi.ErrorNotFound)
			mockClient.EXPECT().CreateLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(nil, lbapi.ErrorNotFound)
			mockClient.EXPECT().GetServiceStatus(gomock.Any(), projectID).Return(lbapi.ProjectStatus("undefined project status"), nil)

			_, err := lbInModeIgnore.EnsureLoadBalancer(context.Background(), clusterName, minimalLoadBalancerService(), []*corev1.Node{})
			Expect(err).To(HaveOccurred())
		})

		It("should update the load balancer if the service changed", func() {
			svc := minimalLoadBalancerService()
			spec, err := lbSpecFromService(svc, []*corev1.Node{}, networkID)
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
			spec, err := lbSpecFromService(svc, []*corev1.Node{nodeA}, networkID)
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
	})

	Describe("EnsureLoadBalancerDeleted", func() {
		It("should trigger load balancer deletion", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(&loadbalancer.LoadBalancer{}, nil)
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
			mockClient.EXPECT().DeleteLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(nil)

			svc := minimalLoadBalancerService()
			svc.Annotations["yawol.stackit.cloud/className"] = classNameYawol

			err := lbInModeCreateAndUpdate.EnsureLoadBalancerDeleted(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
			// Expect DeleteLoadBalancer to have been called.
		})

		It("should trigger load balancer deletion for empty class name in mode \"create & update\"", func() {
			mockClient.EXPECT().GetLoadBalancer(gomock.Any(), projectID, gomock.Any()).Return(&loadbalancer.LoadBalancer{}, nil)
			mockClient.EXPECT().DeleteLoadBalancer(gomock.Any(), projectID, gomock.Any()).MinTimes(1).Return(nil)

			svc := minimalLoadBalancerService()
			delete(svc.Annotations, "yawol.stackit.cloud/className")

			err := lbInModeCreateAndUpdate.EnsureLoadBalancerDeleted(context.Background(), clusterName, svc)
			Expect(err).NotTo(HaveOccurred())
			// Expect DeleteLoadBalancer to have been called.
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
})

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

func versionMatcher(version string) gomock.Matcher {
	return gomock.Cond(func(x any) bool {
		lb := x.(*loadbalancer.UpdateLoadBalancerPayload)
		if lb.Version == nil {
			return false
		}
		return *lb.Version == version
	})
}
