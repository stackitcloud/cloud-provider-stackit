package ingress_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/alb/ingress"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	projectID      = "dummy-project-id"
	region         = "eu01"
	networkID      = "my-network"
	controllerName = "stackit.cloud/alb-ingress"
	finalizerName  = "stackit.cloud/alb-ingress"
	targetCertID   = "real-certificate-uuid-abc-123"
)

var _ = Describe("IngressClassReconciler", func() {
	var (
		recorder  *record.FakeRecorder
		namespace *corev1.Namespace

		mockCtrl   *gomock.Controller
		albClient  *stackit.MockApplicationLoadBalancerClient
		certClient *stackit.MockCertificatesClient

		service corev1.Service
		node    corev1.Node

		managedIngressClass *networkingv1.IngressClass
		ignoredIngressClass *networkingv1.IngressClass
		testIngressObj      *networkingv1.Ingress

		mgrContext context.Context
		mgrCancel  context.CancelFunc

		// The dynamic function hook that controls mock behaviors per context
		setupMocks func(m *stackit.MockApplicationLoadBalancerClient)
	)

	BeforeEach(func() {

		mockCtrl = gomock.NewController(GinkgoT())
		recorder = record.NewFakeRecorder(10)

		albClient = stackit.NewMockApplicationLoadBalancerClient(mockCtrl)
		certClient = stackit.NewMockCertificatesClient(mockCtrl)
		mgrContext, mgrCancel = context.WithCancel(context.Background())

		// 1. Define standard operational defaults so 90% of your tests stay empty
		setupMocks = func(m *stackit.MockApplicationLoadBalancerClient) {
			m.EXPECT().
				GetLoadBalancer(gomock.Any(), projectID, region, gomock.Any()).
				Return(&albsdk.LoadBalancer{Status: new("READY")}, nil).
				AnyTimes()
			m.EXPECT().
				UpdateLoadBalancer(gomock.Any(), projectID, region, gomock.Any(), gomock.Any()).
				Return(&albsdk.LoadBalancer{Status: new("READY")}, nil).
				AnyTimes()
		}
		certClient.EXPECT().
			ListCertificate(gomock.Any(), projectID, region). // allow list cert call
			Return(nil, nil).
			AnyTimes()
	})

	// JustBeforeEach triggers right before the 'It' blocks run, ensuring that
	// whatever configuration 'setupMocks' currently holds is armed on the client.
	JustBeforeEach(func() {
		setupMocks(albClient)

		// Safe global wildcard for asynchronous background deletions
		albClient.EXPECT().
			DeleteLoadBalancer(gomock.Any(), projectID, region, gomock.Any()).
			Return(nil).
			AnyTimes()

		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "stackit-alb-ingress-test-",
			},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		service = corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "test-service", Namespace: namespace.Name},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Ports: []corev1.ServicePort{
					{
						Port:     8080,
						NodePort: 30123,
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &service)).To(Succeed())

		node = corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.10.10.10"}},
			},
		}
		Expect(k8sClient.Create(ctx, &node)).To(Succeed())

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: scheme.Scheme,
		})
		Expect(err).NotTo(HaveOccurred())

		reconciler := ingress.IngressClassReconciler{
			Recorder:          recorder,
			Client:            mgr.GetClient(),
			Scheme:            mgr.GetScheme(),
			ALBClient:         albClient,
			CertificateClient: certClient,
			ALBConfig: stackitconfig.ALBConfig{
				Global: stackitconfig.GlobalOpts{
					ProjectID: projectID,
					Region:    region,
				},
				ApplicationLoadBalancer: stackitconfig.ApplicationLoadBalancerOpts{NetworkID: networkID}},
		}

		err = ctrl.NewControllerManagedBy(mgr).
			Named("ingressclass-test-" + namespace.Name).
			For(&networkingv1.IngressClass{}).
			Complete(&reconciler)
		Expect(err).NotTo(HaveOccurred())

		// Start the manager engine in the background
		go func() {
			defer GinkgoRecover()
			err = mgr.Start(mgrContext)
			Expect(err).NotTo(HaveOccurred())
		}()

	})

	AfterEach(func() {
		mgrCancel() // Terminate background manager routines
		mockCtrl.Finish()

		// Clean up infrastructure resources using global client
		_ = k8sClient.Delete(ctx, &service)
		_ = k8sClient.Delete(ctx, &node)
		if managedIngressClass != nil {

			_ = k8sClient.Delete(ctx, managedIngressClass)
		}
		if ignoredIngressClass != nil {
			_ = k8sClient.Delete(ctx, ignoredIngressClass)
		}
		if testIngressObj != nil {
			_ = k8sClient.Delete(ctx, testIngressObj)
		}
	})

	Context("when the IngressClass does NOT point to the ALB controller", func() {
		It("should ignore the IngressClass and not append finalizers", func() {
			ignoredIngressClass = &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "ignored-ingressclass-",
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: "some.other/controller",
				},
			}
			Expect(k8sClient.Create(ctx, ignoredIngressClass)).To(Succeed())

			// Verify the object state after reconciliation
			reconciledIngressClass := &networkingv1.IngressClass{}

			Consistently(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ignoredIngressClass), reconciledIngressClass)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(reconciledIngressClass.Finalizers).To(BeEmpty())
			}, "2s", "200ms").Should(Succeed())

		})
	})

	Context("when the IngressClass points to the ALB controller", func() {

		BeforeEach(func() {
			managedIngressClass = &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "managed-ingressclass-",
					Labels:       map[string]string{"app": "stackit-alb-ingress"},
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: controllerName,
				},
			}

			testIngressObj = testIngress(managedIngressClass, &service)

		})

		JustBeforeEach(func() {
			testIngressObj = testIngress(managedIngressClass, &service)
		})

		Context("When reconciling an IngressClass", func() {
			It("should successfully reconcile the resource and append the finalizer", func() {

				Expect(k8sClient.Create(ctx, managedIngressClass)).To(Succeed())
				// Check if the finalizer was added
				WaitUntilFinalizerAttached(ctx, k8sClient, managedIngressClass)

				reconciledIngressClass := &networkingv1.IngressClass{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(managedIngressClass), reconciledIngressClass)
				Expect(err).NotTo(HaveOccurred())

			})

		})
		Context("When deleting an IngressClass", func() {
			BeforeEach(func() {
				// 1. Point our managed IngressClass definition to include the target testing labels
				managedIngressClass = &networkingv1.IngressClass{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "managed-ingressclass-",
						UID:          "envtest-ic-uid",
						Labels: map[string]string{
							"app": "stackit-alb-ingress",
							"alb-ingress-controller-ingress-class-uid": "target-cloud-alb-id",
						},
					},
					Spec: networkingv1.IngressClassSpec{Controller: controllerName},
				}

				setupMocks = func(m *stackit.MockApplicationLoadBalancerClient) {
					m.EXPECT().
						GetLoadBalancer(gomock.Any(), projectID, region, gomock.Any()).
						Return(&albsdk.LoadBalancer{Status: new("READY")}, nil).
						AnyTimes()
					m.EXPECT().
						UpdateLoadBalancer(gomock.Any(), projectID, region, gomock.Any(), gomock.Any()).
						Return(&albsdk.LoadBalancer{Status: new("READY")}, nil).
						AnyTimes() // "allow background threads update safely without breaking my test"

					m.EXPECT().
						DeleteLoadBalancer(gomock.Any(), projectID, region, gomock.Any()).
						Return(nil).
						Times(1) // Asserts that the controller MUST call this exactly 1 time!

				}

			})

			It("should read the UID label, delete associated ALB and certificate ", func() {

				// should delete the associated ALB and Certificate
				certClient.EXPECT().
					DeleteCertificate(gomock.Any(), projectID, region, targetCertID).
					Return(nil).
					AnyTimes()

				// Publish the labeled IngressClass to the test cluster
				Expect(k8sClient.Create(ctx, managedIngressClass)).To(Succeed())

				// Wait for the controller background loop to notice it and attach the finalizer
				WaitUntilFinalizerAttached(ctx, k8sClient, managedIngressClass)

				//  Issue the Delete call to test the teardown pipeline
				Expect(k8sClient.Delete(ctx, managedIngressClass)).To(Succeed())

				// Verify the finalizer gets scrubbed and the object disappears from the API Server
				Eventually(func(g Gomega) {
					var ic networkingv1.IngressClass
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(managedIngressClass), &ic)

					g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "The object must be deleted completely")
				}, "5s", "200ms").Should(Succeed())
			})
		})
	})

})

func testIngress(class *networkingv1.IngressClass, service *corev1.Service) *networkingv1.Ingress {
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ingress", Namespace: service.Namespace},
		Spec: networkingv1.IngressSpec{
			IngressClassName: new(class.Name),
			Rules: []networkingv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: new(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: service.Name,
											Port: networkingv1.ServiceBackendPort{Number: service.Spec.Ports[0].Port},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// WaitUntilFinalizerAttached blocks until the controller successfully injects our tracking string
func WaitUntilFinalizerAttached(ctx context.Context, cl client.Client, ic *networkingv1.IngressClass) {
	GinkgoHelper() // Tells Ginkgo to report failures on the line that calls this function, not here!

	reconciledIngressClass := &networkingv1.IngressClass{}
	Eventually(func(g Gomega) {
		err := cl.Get(ctx, client.ObjectKeyFromObject(ic), reconciledIngressClass)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(reconciledIngressClass.Finalizers).To(ContainElement(finalizerName))
	}, "5s", "200ms").Should(Succeed())
}
