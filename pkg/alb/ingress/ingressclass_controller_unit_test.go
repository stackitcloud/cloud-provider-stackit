package ingress

/* import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	gomock "go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
)

const (
	testProjectID        = "test-project"
	testRegion           = "test-region"
	testNamespace        = "test-namespace"
	testPublicIP         = "1.2.3.4"
	testPrivateIP        = "10.0.0.1"
	testIngressName      = "test-ingress"
	testIngressClassName = "k8s-ingress-test-ingress-class"
	testIngressClassUID  = "11111111-2222-3333-4444-555555555555"
	testALBName          = testIngressClassUID
)

//nolint:funlen // Just many test cases.
func TestIngressClassReconciler_updateStatus(t *testing.T) {
	testIngressClass := &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: testIngressClassName,
			UID:  testIngressClassUID,
		},
	}

	testService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: testNamespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Port: 8080},
			},
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
				return c.Create(context.Background(), new(testIngress(testIngressClass, testService)))

			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status: new("STATUS_TERMINATING"),
					}, nil)
			},
			wantResult: reconcile.Result{RequeueAfter: 10 * time.Second},
			wantErr:    false,
		},
		{
			name: "ALB ready, public IP available, ingress status needs update",
			ingresses: []*networkingv1.Ingress{
				new(testIngress(testIngressClass, testService)),
			},
			mockK8sClient: func(c client.Client) error {
				return c.Create(context.Background(), new(testIngress(testIngressClass, testService)))
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:          new("STATUS_READY"),
						ExternalAddress: new(testPublicIP),
					}, nil)
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
		{
			name: "ALB ready, private IP available, ingress status needs update",
			ingresses: []*networkingv1.Ingress{
				new(testIngress(testIngressClass, testService)),
			},
			mockK8sClient: func(c client.Client) error {
				return c.Create(context.Background(), new(testIngress(testIngressClass, testService)))
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:         new("STATUS_READY"),
						PrivateAddress: new(testPrivateIP),
					}, nil)
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
		{
			name: "ALB ready, IP already correct, no status update",
			ingresses: []*networkingv1.Ingress{
				new(func() networkingv1.Ingress {
					ing := testIngress(testIngressClass, testService)
					ing.Spec.IngressClassName = new(testIngressClassName)
					ing.Status = networkingv1.IngressStatus{
						LoadBalancer: networkingv1.IngressLoadBalancerStatus{
							Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: testPublicIP}},
						},
					}
					return ing
				}()),
			},
			mockK8sClient: func(c client.Client) error {
				ing := testIngress(testIngressClass, testService)
				ing.Spec.IngressClassName = new(testIngressClassName)
				if err := c.Create(context.Background(), &ing); err != nil {
					return err
				}

				ing.Status = networkingv1.IngressStatus{
					LoadBalancer: networkingv1.IngressLoadBalancerStatus{
						Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: testPublicIP}},
					},
				}
				return c.Status().Update(context.Background(), &ing)
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:          new("STATUS_READY"),
						ExternalAddress: new(testPublicIP),
					}, nil)
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
		{
			name: "failed to get load balancer",
			ingresses: []*networkingv1.Ingress{
				new(testIngress(testIngressClass, testService)),
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).Return(nil, stackit.ErrorNotFound)
			},
			wantResult: reconcile.Result{},
			wantErr:    true,
		},
		{
			name: "ALB ready, no public or private IP, return error",
			ingresses: []*networkingv1.Ingress{
				new(testIngress(testIngressClass, testService)),
			},
			mockALBClient: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status: new("STATUS_READY"),
					}, nil)
			},
			wantResult: reconcile.Result{},
			wantErr:    true,
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
				ALBConfig: stackitconfig.ALBConfig{
					Global: stackitconfig.GlobalOpts{
						ProjectID: testProjectID,
						Region:    testRegion,
					},
				},
			}

			if tt.mockK8sClient != nil {
				if err := tt.mockK8sClient(fakeClient); err != nil {
					t.Fatalf("mockK8sClient failed: %v", err)
				}
			}

			if tt.mockALBClient != nil {
				tt.mockALBClient(mockAlbClient)
			}

			expectedIngress := testIngress(testIngressClass, testService)

			got, err := r.updateStatus(context.Background(), testIngressClass)
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if diff := cmp.Diff(tt.wantResult, got); diff != "" {
				t.Fatalf("unexpected result (-want +got):\n%s", diff)
			}

			if tt.name == "ALB ready, public IP available, ingress status needs update" {
				latestIngress := &networkingv1.Ingress{}
				if err := fakeClient.Get(context.Background(), client.ObjectKey{Namespace: expectedIngress.Namespace, Name: expectedIngress.Name}, latestIngress); err != nil {
					t.Fatalf("failed to fetch ingress: %v", err)
				}
				if len(latestIngress.Status.LoadBalancer.Ingress) == 0 || latestIngress.Status.LoadBalancer.Ingress[0].IP != testPublicIP {
					t.Errorf("Ingress status was not updated with the expected IP!")
				}
			}

		})
	}
}

func TestIngressClassReconciler_Reconcile(t *testing.T) {

	testIngressClass := &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testIngressClassName,
			UID:        testIngressClassUID,
			Finalizers: []string{finalizerName},
		},
		Spec: networkingv1.IngressClassSpec{
			Controller: controllerName,
		},
	}

	tests := []struct {
		name           string
		ingressClass   *networkingv1.IngressClass
		mockALB        func(*stackit.MockApplicationLoadBalancerClient)
		mockCerts      func(*stackit.MockCertificatesClient)
		wantResult     reconcile.Result
		wantErr        bool
		checkFinalizer bool
	}{
		{
			name:         "existing ingress class, happy",
			ingressClass: testIngressClass,
			mockALB: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:          new("STATUS_READY"),
						ExternalAddress: new(testPublicIP),
					}, nil).Times(2)
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
		{
			name: "ingress class doesn't match the controller, should ignore and exit cleanly",
			ingressClass: &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testIngressClassName,
					UID:        testIngressClassUID,
					Finalizers: []string{finalizerName},
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: "unknown-controller",
				},
			},
			mockALB:    nil,
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
		{
			name:         "ingress class has emtpy/mismatched controller specs, should ignore and exit cleanly",
			ingressClass: &networkingv1.IngressClass{},
			mockALB:      nil,
			wantResult:   reconcile.Result{},
			wantErr:      false,
		},
		{
			name:         "ingress class not found, should ignore and exit cleanly",
			ingressClass: nil,
			mockALB:      nil,
			wantResult:   reconcile.Result{},
			wantErr:      false,
		},
		{
			name: "missing finalizer, should add finalizer",
			ingressClass: &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testIngressClassName,
					UID:        testIngressClassUID,
					Finalizers: []string{},
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: controllerName,
				},
			},
			mockALB:        func(m *stackit.MockApplicationLoadBalancerClient) {},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			checkFinalizer: true,
		},
		{
			name: "ALB status not ready, should requeue",
			ingressClass: &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:       testIngressClassName,
					UID:        testIngressClassUID,
					Finalizers: []string{finalizerName},
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: controllerName,
				},
			},

			mockALB: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:          new("STATUS_NOT_READY"),
						ExternalAddress: new(testPublicIP),
					}, nil).Times(2)
			},
			wantResult:     reconcile.Result{RequeueAfter: 10 * time.Second},
			wantErr:        false,
			checkFinalizer: true,
		},
		{
			name:         "ALB does not exist yet, should create a new one successfully",
			ingressClass: testIngressClass,
			mockALB: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(nil, stackit.ErrorNotFound)
				m.EXPECT().
					CreateLoadBalancer(gomock.Any(), testProjectID, testRegion, gomock.Any()).
					Return(&albsdk.LoadBalancer{}, nil)
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:          new("STATUS_READY"),
						ExternalAddress: new(testPublicIP),
					}, nil)
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
		{
			name: "ingress class has deletion timestamp, should run handleIngressClassDeletion",
			ingressClass: &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:              testIngressClassName,
					UID:               testIngressClassUID,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{finalizerName},
				},
				Spec: networkingv1.IngressClassSpec{Controller: controllerName},
			},
			mockALB: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					DeleteLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(nil)
			},
			mockCerts: func(m *stackit.MockCertificatesClient) {
				m.EXPECT().
					ListCertificate(gomock.Any(), testProjectID, testRegion).
					Return(nil, nil)
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
		{
			name:         "ALB configuration has changed, should issue an update call",
			ingressClass: testIngressClass,
			mockALB: func(m *stackit.MockApplicationLoadBalancerClient) {
				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:    new("STATUS_READY"),
						Version:   new("lb-v1"),
						Listeners: []albsdk.Listener{{Port: new(int32(80))}}, // add a listener to empty config
					}, nil)

				m.EXPECT().
					UpdateLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName, gomock.Any()).
					Return(&albsdk.LoadBalancer{}, nil)

				m.EXPECT().
					GetLoadBalancer(gomock.Any(), testProjectID, testRegion, testALBName).
					Return(&albsdk.LoadBalancer{
						Status:          new("STATUS_READY"),
						ExternalAddress: new(testPublicIP),
					}, nil)
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockAlbClient := stackit.NewMockApplicationLoadBalancerClient(ctrl)
			mockCertsClient := stackit.NewMockCertificatesClient(ctrl)

			clientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tt.ingressClass != nil {
				clientBuilder.WithRuntimeObjects(tt.ingressClass)
			}
			fakeClient := clientBuilder.Build()

			r := &IngressClassReconciler{
				Client:            fakeClient,
				ALBClient:         mockAlbClient,
				CertificateClient: mockCertsClient,
				ALBConfig: stackitconfig.ALBConfig{
					Global: stackitconfig.GlobalOpts{
						ProjectID: testProjectID,
						Region:    testRegion,
					},
				},
			}

			if tt.mockALB != nil {
				tt.mockALB(mockAlbClient)
			}
			if tt.mockCerts != nil {
				tt.mockCerts(mockCertsClient)
			}

			req := reconcile.Request{
				NamespacedName: client.ObjectKey{
					Name: testIngressClassName,
				},
			}

			got, err := r.Reconcile(context.Background(), req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Reconcile() - expected error %v, got %v", tt.wantErr, err)
			}
			if diff := cmp.Diff(tt.wantResult, got); diff != "" {
				t.Fatalf("Reconcile() - unexpected result (-want +got):\n%s", diff)
			}

			if tt.checkFinalizer {
				// fetching the absolute latest state of the object directly from the fake K8s API server
				latestClass := &networkingv1.IngressClass{}
				key := client.ObjectKey{Name: testIngressClassName}

				if fetchErr := fakeClient.Get(context.Background(), key, latestClass); fetchErr != nil {
					t.Fatalf("Failed to fetch latest IngressClass state from fake client: %v", fetchErr)
				}

				// assertion: the finalizer string list is no longer empty and contains finalizerName
				if len(latestClass.Finalizers) == 0 {
					t.Errorf("Verification failed: expected IngressClass to have finalizers, but the list is empty")
				} else if latestClass.Finalizers[0] != finalizerName {
					t.Errorf("Verification failed: expected finalizer %q, but found %q", finalizerName, latestClass.Finalizers[0])
				}
			}
		})
	}
}
*/
