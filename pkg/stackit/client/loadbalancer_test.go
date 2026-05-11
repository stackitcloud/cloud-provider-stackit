package client

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"
	"go.uber.org/mock/gomock"

	mock "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/client/mock"
)

var _ = Describe("LoadBalancer", func() {
	var (
		mockCtrl     *gomock.Controller
		mockLBClient *mock.MockLoadBalancingClient
	)

	const (
		lbName = "my-lb"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockLBClient = mock.NewMockLoadBalancingClient(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("LoadBalancer Management", func() {
		It("CreateLoadBalancer successfully calls the API", func() {
			payload := loadbalancer.CreateLoadBalancerPayload{Name: new(lbName)}
			mockLBClient.EXPECT().
				CreateLoadBalancer(gomock.Any(), payload).
				Return(&loadbalancer.LoadBalancer{Name: new(lbName)}, nil)

			lb, err := mockLBClient.CreateLoadBalancer(context.Background(), payload)
			Expect(err).ToNot(HaveOccurred())
			Expect(*lb.Name).To(Equal(lbName))
		})

		It("ListLoadBalancers returns a slice of load balancers", func() {
			mockItems := []loadbalancer.LoadBalancer{
				{Name: new("lb-1")},
				{Name: new("lb-2")},
			}
			mockLBClient.EXPECT().
				ListLoadBalancers(gomock.Any()).
				Return(mockItems, nil)

			lbs, err := mockLBClient.ListLoadBalancers(context.Background())
			Expect(err).ToNot(HaveOccurred())
			Expect(lbs).To(HaveLen(2))
			Expect(*lbs[0].Name).To(Equal("lb-1"))
		})

		It("GetLoadBalancer returns a specific LB", func() {
			mockLBClient.EXPECT().
				GetLoadBalancer(gomock.Any(), lbName).
				Return(&loadbalancer.LoadBalancer{Name: new(lbName)}, nil)

			lb, err := mockLBClient.GetLoadBalancer(context.Background(), lbName)
			Expect(err).ToNot(HaveOccurred())
			Expect(*lb.Name).To(Equal(lbName))
		})

		It("UpdateLoadBalancer calls API successfully", func() {
			mockLBClient.EXPECT().
				UpdateLoadBalancer(gomock.Any(), lbName, gomock.Any()).
				Return(nil)

			err := mockLBClient.UpdateLoadBalancer(context.Background(), lbName, loadbalancer.UpdateLoadBalancerPayload{})
			Expect(err).ToNot(HaveOccurred())
		})

		It("DeleteLoadBalancer calls API successfully", func() {
			mockLBClient.EXPECT().
				DeleteLoadBalancer(gomock.Any(), lbName).
				Return(nil)

			err := mockLBClient.DeleteLoadBalancer(context.Background(), lbName)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("Target Pools", func() {
		It("UpdateTargetPool calls API successfully", func() {
			payload := loadbalancer.UpdateTargetPoolPayload{}
			mockLBClient.EXPECT().
				UpdateTargetPool(gomock.Any(), lbName, "pool-1", payload).
				Return(nil)

			err := mockLBClient.UpdateTargetPool(context.Background(), lbName, "pool-1", payload)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("Credentials", func() {
		It("CreateCredentials returns response on success", func() {
			mockLBClient.EXPECT().
				CreateCredentials(gomock.Any(), gomock.Any()).
				Return(&loadbalancer.CreateCredentialsResponse{Credential: &loadbalancer.CredentialsResponse{
					DisplayName: new("cred-1"),
				}}, nil)

			resp, err := mockLBClient.CreateCredentials(context.Background(), loadbalancer.CreateCredentialsPayload{})
			Expect(err).ToNot(HaveOccurred())
			Expect(*resp.Credential.DisplayName).To(Equal("cred-1"))
		})

		It("ListCredentials returns all credentials", func() {
			mockLBClient.EXPECT().
				ListCredentials(gomock.Any()).
				Return(&loadbalancer.ListCredentialsResponse{Credentials: []loadbalancer.CredentialsResponse{{DisplayName: new("cred-1")}}}, nil)

			resp, err := mockLBClient.ListCredentials(context.Background())
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Credentials).To(HaveLen(1))
		})

		It("DeleteCredentials calls API successfully", func() {
			mockLBClient.EXPECT().
				DeleteCredentials(gomock.Any(), "cred-ref").
				Return(nil)

			err := mockLBClient.DeleteCredentials(context.Background(), "cred-ref")
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
