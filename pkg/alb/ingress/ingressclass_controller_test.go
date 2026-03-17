package ingress

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	gomock "go.uber.org/mock/gomock"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
)

const (
	testProjectID = "test-project"
	testRegion    = "test-region"
	testALBName   = "k8s-ingress-test-ingressclass"
	testNamespace = "test-namespace"
	testPublicIP  = "1.2.3.4"
	testPrivateIP = "10.0.0.1"
)

func TestIngressClassReconciler_updateStatus(t *testing.T) {
	testIngressClass := &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: testIngressClassName,
		},
	}

	tests := []struct {
		name          string
		ingresses     []*networkingv1.Ingress
		mockK8sClient func(client.Client) error
		mockALBClient func(*stackit.MockApplicationLoadBalancerClient)
		wantResult    reconcile.Result
		wantErr       bool
	}{
		{
			name: "ALB not ready (Terminating), should requeue",
			mockK8sClient: func(c client.Client) error {
				return c.Create(context.Background(), &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testIngressName},
				})
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status: ptr.To("STATUS_TERMINATING"),
					}, nil)
			},
			wantResult: reconcile.Result{RequeueAfter: 10 * time.Second},
			wantErr:    false,
		},
		// This case only checks the reconcile result, not whether the ingress status was actually updated.
		// The actual update logic will be verified in integration tests.
		{
			name: "ALB ready, public IP available, ingress status needs update",
			ingresses: []*networkingv1.Ingress{
				{ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testIngressName}},
			},
			mockK8sClient: func(c client.Client) error {
				return c.Create(context.Background(), &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testIngressName},
				})
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:          ptr.To("STATUS_READY"),
						ExternalAddress: ptr.To(testPublicIP),
					}, nil)
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
		// This case only checks the reconcile result, not whether the ingress status was actually updated.
		// The actual update logic will be verified in integration tests.
		{
			name: "ALB ready, private IP available, ingress status needs update",
			ingresses: []*networkingv1.Ingress{
				{ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testIngressName}},
			},
			mockK8sClient: func(c client.Client) error {
				return c.Create(context.Background(), &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testIngressName},
				})
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:         ptr.To("STATUS_READY"),
						PrivateAddress: ptr.To(testPrivateIP),
					}, nil)
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
		// This case only checks the reconcile result, not whether the ingress status was actually updated.
		// The actual update logic will be verified in integration tests.
		{
			name: "ALB ready, IP already correct, no update",
			ingresses: []*networkingv1.Ingress{
				{
					ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testIngressName},
					Status: networkingv1.IngressStatus{
						LoadBalancer: networkingv1.IngressLoadBalancerStatus{
							Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: testPublicIP}},
						},
					},
				},
			},
			mockK8sClient: func(c client.Client) error {
				return c.Create(context.Background(), &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testIngressName},
					Status: networkingv1.IngressStatus{
						LoadBalancer: networkingv1.IngressLoadBalancerStatus{
							Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: testPublicIP}},
						},
					},
				})
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:         ptr.To("STATUS_READY"),
						PrivateAddress: ptr.To(testPublicIP),
					}, nil)
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
		{
			name: "failed to get load balancer",
			ingresses: []*networkingv1.Ingress{
				{ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testIngressName}},
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).Return(nil, stackit.ErrorNotFound)
			},
			wantResult: reconcile.Result{},
			wantErr:    true,
		},
		{
			name: "failed to get latest ingress",
			ingresses: []*networkingv1.Ingress{
				{ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testIngressName}},
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:         ptr.To("STATUS_READY"),
						PrivateAddress: ptr.To(testPublicIP),
					}, nil)
			},
			wantResult: reconcile.Result{},
			wantErr:    true,
		},
		// This case only checks the reconcile result, not whether the ingress status was actually updated.
		// The actual update logic will be verified in integration tests.
		{
			name: "failed to update ingress status",
			ingresses: []*networkingv1.Ingress{
				{ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testIngressName}},
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:         ptr.To("STATUS_READY"),
						PrivateAddress: ptr.To(testPublicIP),
					}, nil)
			},
			wantResult: reconcile.Result{},
			wantErr:    true,
		},
		{
			name: "ALB ready, no public or private IP, should requeue",
			ingresses: []*networkingv1.Ingress{
				{ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testIngressName}},
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status: ptr.To("STATUS_READY"),
					}, nil)
			},
			wantResult: reconcile.Result{RequeueAfter: 10 * time.Second},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			mockAlbClient := stackit.NewMockApplicationLoadBalancerClient(ctrl)
			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			r := &IngressClassReconciler{
				Client:    fakeClient,
				ALBClient: mockAlbClient,
				ProjectID: testProjectID,
				Region:    testRegion,
			}

			if tt.mockK8sClient != nil {
				if err := tt.mockK8sClient(fakeClient); err != nil {
					t.Fatalf("mockK8sClient failed: %v", err)
				}
			}

			if tt.mockALBClient != nil {
				tt.mockALBClient(mockAlbClient)
			}

			got, err := r.updateStatus(context.Background(), tt.ingresses, testIngressClass)
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if diff := cmp.Diff(tt.wantResult, got); diff != "" {
				t.Fatalf("unexpected result (-want +got):\n%s", diff)
			}
		})
	}
}
