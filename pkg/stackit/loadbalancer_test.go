package stackit

import (
	"context"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"
	"go.uber.org/mock/gomock"
	"k8s.io/utils/ptr"

	mock "github.com/stackitcloud/cloud-provider-stackit/pkg/mock/loadbalancer"
)

var _ = Describe("LBAPI Client", func() {
	var (
		region = "eu01"

		mockCtrl *gomock.Controller
		mockAPI  *mock.MockDefaultAPI
		lbClient LoadbalancerClient
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockAPI = mock.NewMockDefaultAPI(mockCtrl)

		var err error
		lbClient, err = NewLoadbalancerClient(mockAPI, region)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("GetLoadBalancer", func() {
		It("should return the received load balancer instance", func() {
			expectedName := "test LB instance"
			expectedLB := &loadbalancer.LoadBalancer{Name: new(expectedName)}
			mockAPI.EXPECT().GetLoadBalancer(gomock.Any(), "projectID", gomock.Any(), expectedName).
				Return(loadbalancer.ApiGetLoadBalancerRequest{ApiService: mockAPI}).Times(1)
			mockAPI.EXPECT().GetLoadBalancerExecute(gomock.Any()).Return(expectedLB, nil).Times(1)

			actualLB, err := lbClient.GetLoadBalancer(context.Background(), "projectID", expectedName)
			Expect(err).ToNot(HaveOccurred())
			Expect(actualLB).To(Equal(expectedLB))
			actualName := ptr.Deref(actualLB.Name, "")
			Expect(actualName).To(Equal(expectedName))
		})

		It("should use the configured STACKIT region", func() {
			mockAPI.EXPECT().GetLoadBalancer(gomock.Any(), gomock.Any(), region, gomock.Any()).
				Return(loadbalancer.ApiGetLoadBalancerRequest{ApiService: mockAPI}).Times(1)
			mockAPI.EXPECT().GetLoadBalancerExecute(gomock.Any()).Return(&loadbalancer.LoadBalancer{}, nil).Times(1)

			_, err := lbClient.GetLoadBalancer(context.Background(), "projectID", "name")
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return ErrorNotFound if a GenericOpenAPIError with status 404 occurs", func() {
			mockAPI.EXPECT().GetLoadBalancer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(loadbalancer.ApiGetLoadBalancerRequest{ApiService: mockAPI}).Times(1)
			mockAPI.EXPECT().GetLoadBalancerExecute(gomock.Any()).Return(nil, &oapierror.GenericOpenAPIError{StatusCode: http.StatusNotFound}).Times(1)

			actualLB, err := lbClient.GetLoadBalancer(context.Background(), "projectID", "name")
			Expect(actualLB).To(BeNil())
			Expect(err).To(MatchError(ErrorNotFound))
		})
	})
})
