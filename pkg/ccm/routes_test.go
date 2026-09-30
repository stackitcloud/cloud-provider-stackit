package ccm

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	stackitclient "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/client"
	stackitclientmock "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/client/mock"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
)

var _ = Describe("Routes", func() {
	var (
		mockClient  *stackitclientmock.MockIaaSClient
		r           *Routes
		clusterName string
	)

	const routingTableID = "my-routing-table"

	BeforeEach(func() {
		clusterName = "my-cluster"
		ctrl := gomock.NewController(GinkgoT())
		mockClient = stackitclientmock.NewMockIaaSClient(ctrl)
		r = &Routes{
			iaasClient:     mockClient,
			routingTableID: routingTableID,
		}
	})

	Describe("#CreateRoute", func() {
		Context("no routes present", func() {
			It("should create routes", func(ctx context.Context) {
				cpRoute := &cloudprovider.Route{
					Name:                "my-route",
					TargetNode:          "my-node",
					EnableNodeAddresses: true,
					TargetNodeAddresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeInternalIP,
							Address: "192.168.0.2",
						},
						{
							Type:    corev1.NodeExternalIP,
							Address: "188.100.18.2",
						},
					},
					DestinationCIDR: "10.0.0.0/24",
					Blackhole:       false,
				}

				expectedLabels := stackitclient.Labels{
					labelKeyRouteNameHint: "foo",
					labelKeyRouteNodeName: "my-node",
					labelKeyClusterName:   clusterName,
				}
				expectedIaasRoutes := []iaas.Route{
					{
						Destination: iaas.RouteDestination{
							DestinationCIDRv4: &iaas.DestinationCIDRv4{
								Type:  "cidrv4",
								Value: "10.0.0.0/24",
							},
						},
						Labels: expectedLabels.ToSDK(),
						Nexthop: iaas.RouteNexthop{
							NexthopIPv4: &iaas.NexthopIPv4{
								Type:  "ipv4",
								Value: "192.168.0.2",
							},
						},
					},
				}
				mockClient.EXPECT().GetRoutingTable(ctx, routingTableID).Return(&iaas.RoutingTable{Id: new(routingTableID)}, nil)
				mockClient.EXPECT().ListRoutes(ctx, routingTableID, expectedLabels).Times(1).Return([]iaas.Route{}, nil)
				mockClient.EXPECT().AddRoutes(ctx, routingTableID, expectedIaasRoutes).Times(1).Return(nil)

				Expect(r.CreateRoute(ctx, clusterName, "foo", cpRoute)).NotTo(HaveOccurred())
			})
		})
	})

	Describe("#ListRoutes", func() {
		It("should return the correct cloudprovider routes", func(ctx context.Context) {
			existingIaasRoutes := []iaas.Route{
				// first node
				{
					Destination: iaas.RouteDestination{
						DestinationCIDRv4: &iaas.DestinationCIDRv4{
							Type:  "cidrv4",
							Value: "10.0.0.0/24",
						},
					},
					Labels: stackitclient.Labels{
						labelKeyRouteNameHint: "foo",
						labelKeyRouteNodeName: "node1",
						labelKeyClusterName:   clusterName,
					}.ToSDK(),
					Nexthop: iaas.RouteNexthop{
						NexthopIPv4: &iaas.NexthopIPv4{
							Type:  "ipv4",
							Value: "192.168.0.2",
						},
					},
				},
				// second node
				{
					Destination: iaas.RouteDestination{
						DestinationCIDRv4: &iaas.DestinationCIDRv4{
							Type:  "cidrv4",
							Value: "10.0.1.0/24",
						},
					},
					Labels: stackitclient.Labels{
						labelKeyRouteNameHint: "bar",
						labelKeyRouteNodeName: "node2",
						labelKeyClusterName:   clusterName,
					}.ToSDK(),
					Nexthop: iaas.RouteNexthop{
						NexthopIPv4: &iaas.NexthopIPv4{
							Type:  "ipv4",
							Value: "192.168.0.5",
						},
					},
				},
			}
			mockClient.EXPECT().GetRoutingTable(ctx, routingTableID).Return(&iaas.RoutingTable{Id: new(routingTableID)}, nil)
			mockClient.EXPECT().ListRoutes(ctx, routingTableID, stackitclient.Labels{
				labelKeyClusterName: clusterName,
			}).Times(1).Return(existingIaasRoutes, nil)

			cpRoutes, err := r.ListRoutes(ctx, clusterName)
			Expect(err).NotTo(HaveOccurred())
			Expect(cpRoutes).To(ConsistOf(&cloudprovider.Route{
				TargetNode: "node1",
				TargetNodeAddresses: []corev1.NodeAddress{
					{
						Type:    corev1.NodeInternalIP,
						Address: "192.168.0.2",
					},
				},
				DestinationCIDR: "10.0.0.0/24",
				Blackhole:       false,
			},
				&cloudprovider.Route{
					TargetNode: "node2",
					TargetNodeAddresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeInternalIP,
							Address: "192.168.0.5",
						},
					},
					DestinationCIDR: "10.0.1.0/24",
					Blackhole:       false,
				},
			))
		})
	})

	Describe("#DeleteRoute", func() {
		It("should only delete routes for the node", func(ctx context.Context) {
			routeID := "12345"
			existingIaasRoutes := []iaas.Route{
				{
					Id: &routeID,
					Destination: iaas.RouteDestination{
						DestinationCIDRv4: &iaas.DestinationCIDRv4{
							Type:  "cidrv4",
							Value: "10.0.1.0/24",
						},
					},
					Labels: stackitclient.Labels{
						labelKeyRouteNameHint: "bar",
						labelKeyRouteNodeName: "node1",
						labelKeyClusterName:   clusterName,
					}.ToSDK(),
					Nexthop: iaas.RouteNexthop{
						NexthopIPv4: &iaas.NexthopIPv4{
							Type:  "ipv4",
							Value: "192.168.0.5",
						},
					},
				},
			}
			mockClient.EXPECT().GetRoutingTable(ctx, routingTableID).Return(&iaas.RoutingTable{Id: new(routingTableID)}, nil)
			mockClient.EXPECT().ListRoutes(ctx, routingTableID, stackitclient.Labels{
				labelKeyClusterName:   clusterName,
				labelKeyRouteNodeName: "node1",
			}).Times(1).Return(existingIaasRoutes, nil)
			mockClient.EXPECT().DeleteRoute(gomock.Any(), routingTableID, routeID).Times(1).Return(nil)

			cpRoute := &cloudprovider.Route{
				TargetNode: "node1",
				TargetNodeAddresses: []corev1.NodeAddress{
					{
						Type:    corev1.NodeInternalIP,
						Address: "192.168.0.2",
					},
				},
				DestinationCIDR: "10.0.0.0/24",
				Blackhole:       false,
			}
			Expect(r.DeleteRoute(ctx, clusterName, cpRoute)).To(Succeed())
		})
	})
})
