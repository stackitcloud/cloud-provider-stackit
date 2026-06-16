package ingress_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/alb/ingress"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/alb/ingress/spec"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/alb/ingress/testutil"
	. "github.com/stackitcloud/cloud-provider-stackit/pkg/alb/ingress/testutil/ingress"
	. "github.com/stackitcloud/cloud-provider-stackit/pkg/alb/ingress/testutil/service"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
	certsdk "github.com/stackitcloud/stackit-sdk-go/services/certificates/v2api"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
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

var _ = FDescribe("IngressClassController", func() {
	var (
		recorder *record.FakeRecorder

		// namespace is the namespace in which all namespaced resources of the test case should go.
		// It is cleaned up automatically when the test ends and all resource deletions will be finalized before the test case completes.
		namespace *corev1.Namespace

		mockCtrl   *gomock.Controller
		albClient  *stackit.MockApplicationLoadBalancerClient
		certClient *stackit.MockCertificatesClient

		node corev1.Node

		mgrContext        context.Context
		mgrCancel         context.CancelFunc
		managerTerminated sync.WaitGroup
	)

	BeforeEach(func(ctx context.Context) {

		mockCtrl = gomock.NewController(GinkgoT())
		recorder = record.NewFakeRecorder(10)

		albClient = stackit.NewMockApplicationLoadBalancerClient(mockCtrl)
		certClient = stackit.NewMockCertificatesClient(mockCtrl)
		mgrContext, mgrCancel = context.WithCancel(context.Background())

		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "stackit-alb-ingress-test-",
			},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func(ctx context.Context) {
			// There is no namespace controller deployed.
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		})

		node = corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.10.10.10"}},
			},
		}
		Expect(k8sClient.Create(ctx, &node)).To(Succeed())
		DeferCleanup(func(ctx context.Context) {
			Expect(k8sClient.Delete(ctx, &node)).To(Succeed())
		})

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

		Expect(reconciler.SetupWithManager(ctx, mgr, namespace.Name)).To(Succeed())

		managerTerminated.Add(1)
		go func() {
			defer GinkgoRecover()
			err = mgr.Start(mgrContext)
			managerTerminated.Done()
			Expect(err).NotTo(HaveOccurred())
		}()
		DeferCleanup(func() {
			mgrCancel()
			// Canceling the context doesn't cause the manager to stop immediately.
			// We have to wait for manager.Start() to return to ensure that the manager doesn't "spill" into the next test case.
			managerTerminated.Wait()
			mockCtrl.Finish()
		})

	})

	Context("when the IngressClass does not match controller", func() {
		It("should ignore the IngressClass and not append finalizers", func(ctx context.Context) {
			ignoredIngressClass := &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "ignored-ingressclass-",
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: "some.other/controller",
				},
			}
			Expect(k8sClient.Create(ctx, ignoredIngressClass)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWaitForKubernetesResource(ctx, k8sClient, ignoredIngressClass)
			})

			Consistently(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ignoredIngressClass), ignoredIngressClass)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(ignoredIngressClass.Finalizers).To(BeEmpty())
			}, "2s", "200ms").Should(Succeed())

		})
	})

	It("should create an empty ALB for an ingress class matching the controller", func(ctx context.Context) {
		certClient.EXPECT().ListCertificate(gomock.Any(), gomock.Any(), gomock.Any()).Return(new(certsdk.ListCertificatesResponse{
			Items: []certsdk.GetCertificateResponse{},
		}), nil).AnyTimes()
		albClient.EXPECT().GetLoadBalancer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, stackit.ErrorNotFound).AnyTimes()
		done := make(chan any)
		albClient.EXPECT().CreateLoadBalancer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _, _ string, _ *albsdk.CreateLoadBalancerPayload) (*albsdk.LoadBalancer, error) {
			// TODO: verify arguments
			close(done)
			return new(albsdk.LoadBalancer{}), nil
		}).MinTimes(1) // TODO: Change to exactly once.

		ingressClass := &networkingv1.IngressClass{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "managed-ingressclass-",
			},
			Spec: networkingv1.IngressClassSpec{
				Controller: controllerName,
			},
		}
		Expect(k8sClient.Create(ctx, ingressClass)).To(Succeed())
		DeferCleanup(func() {
			testutil.DeleteAndWaitForKubernetesResource(ctx, k8sClient, ingressClass)
		})

		WaitUntilFinalizerAttached(ctx, k8sClient, ingressClass)

		Eventually(done).WithTimeout(5 * time.Second).Should(BeClosed())
	})

	// The ALB is already created when BeforeEach completes.
	Context("with IngressClass matching the controller", func() {
		var (
			ingressClass *networkingv1.IngressClass

			getLoadBalancerResponse  *atomic.Pointer[albsdk.LoadBalancer]
			listCertificatesResponse *atomic.Pointer[certsdk.ListCertificatesResponse]
		)

		BeforeEach(func(ctx context.Context) {
			getLoadBalancerResponse = &atomic.Pointer[albsdk.LoadBalancer]{}
			listCertificatesResponse = &atomic.Pointer[certsdk.ListCertificatesResponse]{}
			listCertificatesResponse.Store(&certsdk.ListCertificatesResponse{Items: []certsdk.GetCertificateResponse{}})

			certClient.EXPECT().ListCertificate(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, projectID, region string) (*certsdk.ListCertificatesResponse, error) {
				return listCertificatesResponse.Load(), nil
			}).AnyTimes()

			albClient.EXPECT().GetLoadBalancer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, projectID, region, name string) (*albsdk.LoadBalancer, error) {
				lb := getLoadBalancerResponse.Load()
				if lb == nil {
					return nil, stackit.ErrorNotFound
				}
				return lb, nil
			}).AnyTimes()
			albClient.EXPECT().CreateLoadBalancer(gomock.Any(), projectID, region, gomock.Any()).DoAndReturn(func(_ context.Context, _, _ string, create *albsdk.CreateLoadBalancerPayload) (*albsdk.LoadBalancer, error) {
				// TODO: check name
				response := albsdk.LoadBalancer(*create)
				response.Version = new("version-after-create")
				response.ExternalAddress = new("127.0.0.1")
				response.Status = new(stackit.LBStatusReady)
				getLoadBalancerResponse.Store(&response)
				return &response, nil
			}).Times(1)

			ingressClass = &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "ingressclass-",
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: controllerName,
				},
			}
			Expect(k8sClient.Create(ctx, ingressClass)).To(Succeed())
			// Wait for CreateLoadBalancer to be called, i.e. getLoadBalancerResponse to not be nil.
			Eventually(getLoadBalancerResponse).Should(testutil.HaveAtomicValue[albsdk.LoadBalancer](Not(BeNil())))
		})

		It("should create certificate and referenced in ALB", func(ctx context.Context) {
			updateRequest := &atomic.Pointer[albsdk.UpdateLoadBalancerPayload]{}
			certClient.EXPECT().CreateCertificate(gomock.Any(), projectID, region, gomock.Any()).DoAndReturn(func(ctx context.Context, projectID, region string, certificate *certsdk.CreateCertificatePayload) (*certsdk.GetCertificateResponse, error) {
				fingerprint, err := spec.ValidateTLSCertAndFingerprint([]byte(*certificate.PublicKey), []byte(*certificate.PrivateKey))
				if err != nil {
					return nil, fmt.Errorf("invalid certificate: %w", err)
				}
				response := certsdk.GetCertificateResponse{
					Id:     new("random-certificate-id"),
					Labels: certificate.Labels,
					Data: &certsdk.Data{
						FingerprintSha256: new(fingerprint),
					},
					PublicKey: certificate.PublicKey,
				}
				listCertificatesResponse.Store(&certsdk.ListCertificatesResponse{
					Items: []certsdk.GetCertificateResponse{response},
				})
				return &response, nil
			}).Times(1)
			albClient.EXPECT().UpdateLoadBalancer(gomock.Any(), projectID, region, gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, projectID, region, name string, update *albsdk.UpdateLoadBalancerPayload) (*albsdk.LoadBalancer, error) {
				response := albsdk.LoadBalancer(*update)
				response.Version = new("version-after-update")
				response.ExternalAddress = new("127.0.0.1")
				response.Status = new(stackit.LBStatusReady)
				getLoadBalancerResponse.Store(&response)

				updateRequest.Store(update)
				return (*albsdk.LoadBalancer)(update), nil
			}).Times(1)

			secret := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: corev1.NamespaceDefault, Name: "my-tls-cert"},
				Type:       corev1.SecretTypeTLS,
				Data: map[string][]byte{
					corev1.TLSCertKey:       []byte(fixtureTLSPublicKey),
					corev1.TLSPrivateKeyKey: []byte(fixtureTLSPrivateKey),
				},
			}
			Expect(k8sClient.Create(ctx, &secret)).To(Succeed())
			service := Service(corev1.NamespaceDefault, "my-service", WithServiceType(corev1.ServiceTypeNodePort), WithPort("http", 80, 30000, corev1.ProtocolTCP))
			Expect(k8sClient.Create(ctx, &service)).To(Succeed())
			ingress := Ingress(corev1.NamespaceDefault, "my-ingress", WithIngressClass(ingressClass.Name), WithTLSSecret(secret.Name),
				WithRule("my-host.local", WithPath("/", new(networkingv1.PathTypePrefix), service.Name, networkingv1.ServiceBackendPort{Number: 80})),
			)
			Expect(k8sClient.Create(ctx, &ingress)).To(Succeed())

			Eventually(updateRequest).Should(testutil.HaveAtomicValue[albsdk.UpdateLoadBalancerPayload](Not(BeNil())))
			update := updateRequest.Load()
			Expect(update.Version).To(HaveValue(Equal("version-after-create")))
			Expect(update.Listeners[1].Https.CertificateConfig.CertificateIds).To(ConsistOf("random-certificate-id"))
		})

		/* 		Context("When deleting an IngressClass", func() {
			BeforeEach(func() {
				// 1. Point our managed IngressClass definition to include the target testing labels
				managedIngressClass = &networkingv1.IngressClass{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "managed-ingressclass-",
						UID:          "envtest-ic-uid",
						Labels: map[string]string{
							labels.LabelIngressClassUID: "target-cloud-alb-id",
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

			It("should read the UID label, delete associated ALB and certificate ", func(ctx context.Context) {

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
		}) */
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

const (
	fixtureTLSPublicKey = `-----BEGIN CERTIFICATE-----
MIIFmzCCA4OgAwIBAgIUbhg0VsnIT3fREtGHtyj1YYY1mkUwDQYJKoZIhvcNAQEL
BQAwXTELMAkGA1UEBhMCREUxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoM
GEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDEWMBQGA1UEAwwNbXktaG9zdC5sb2Nh
bDAeFw0yNjA2MTYwODU4MzVaFw0yNzA2MTYwODU4MzVaMF0xCzAJBgNVBAYTAkRF
MRMwEQYDVQQIDApTb21lLVN0YXRlMSEwHwYDVQQKDBhJbnRlcm5ldCBXaWRnaXRz
IFB0eSBMdGQxFjAUBgNVBAMMDW15LWhvc3QubG9jYWwwggIiMA0GCSqGSIb3DQEB
AQUAA4ICDwAwggIKAoICAQDBwBCu7Bc77uMgUOslDJUObgG5FZUYWzdo6owK6Qmo
aNfvjmwwkbMHLqu8t6ZNi9UoRTJ1G9GeM8JtPL+bikKu1ZjN2MbO6VHI3xy0Az85
r2/FKta1faFcrV7Vul/zJqAljf4qeTK31mFmZq1is86Q0wYcEf3qnNDafN5ThGT/
F7akDlKTDG1RmyXHw+/90TINZ6q8Rqf5kI3EV63zlrG6iRJ38Dphge8Hk+ZGjURm
qx7Jz2iJkRGbIB53ZDEBk+KWM6K7iUbswmJv4qyat8P7Bv2Iisob9LVhU//852f+
vdmdxoebUn6dGjsNv9lX0qKiEzcE1Lm2SPNIB3bfY5xNnKNjCT7qZ4NXKoeTTwLK
S+gN8zcY3Sdb8kyCKmhIGA4TXsQEyhzG/YwYGE/VgOEgv324VDGB5FcT+VcjZiHD
6nzDfqKH3NkaJ70PsCa4t3scHogkWQLnGMJd2/T+t/L3tVPZaJearexh6RUZJlIW
gCCAMqJPoALKzGrfSHhiy5L+ghpEgSnh4ZiWYxNbPcbGOXygxOZVnNc4y1PNb+vX
hXGU16wSoWQf8cZA0WDKiXLFz7qM6tAS49PJsHalWryE3qO741D/fOgl7Nzsi6MR
0lMsR9pCptIPPmiY/5f6pFxgS08IJFhaxAybCEuroLLBdXBMcD2SmSP7Scm2az09
1QIDAQABo1MwUTAdBgNVHQ4EFgQUjdu/uxlLXaaafQIdx6gZZ45cxgswHwYDVR0j
BBgwFoAUjdu/uxlLXaaafQIdx6gZZ45cxgswDwYDVR0TAQH/BAUwAwEB/zANBgkq
hkiG9w0BAQsFAAOCAgEAX+/DmcP+iAqOo0WaOOvM7V4Iz8EAXSRdgMgi+xPRH8Dt
gYe1xc0eb3UJkkeOusrQKfEXbC47X905aAGACPNqLs+Mm40h8bctAqKExgFM7noM
8OK/y1I3RjDtbCMHJ5uCanuuqgVpXuuSWOafwY21n2mPi15+wjYJlk9YOVPAXkIl
wHpWwGv+4uuD0ppTHwF2bLFpypeVSsVLQdQ/F6H2K6QFIaHXhMZm2m1wLdD8AuiU
1AagiwOQwnGcSzKSjptO1DjWlJOPffcAzO2zXq3HT4Y3debbiKIY5uhXJfU7u82D
Q45dms99DN6FzFONf92NfHI48PAmHXFD8xoKOYejcsV/Fe0coccCbbj/wlReVabt
PE0skr0z12hPkQ6+BQri2nxKqbQPCyLKQNJ4p1ku2v73TX0zd2fU+P3mV0UoFovF
/8vOqc6J+MyrDSzvqdunEPL8pG6ziGnhC2fT2e41LYKWQqkBjFIQnEeTcr0pVdiG
R4dGu19QV3PBoX2IbLexndiYGCJuBsKpjIu5C4Z5BibXXZdngPwpWdaoG2DZQZ2s
okmiQzkHzZ3ADR/UVqTDICjr8gEzjZRfgwEt+jIkgEV7i5S9GS9miyzUKPi6pEuL
JGVFbYQdFntS/izqlEV0L+3te0WKQIEX6Sq8wdxg0twpRdzaMepJiLTYi/YxJa8=
-----END CERTIFICATE-----`
	fixtureTLSPrivateKey = `-----BEGIN PRIVATE KEY-----
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
UoA+9Hd41MHTo2Frp1cML2BpdbK/
-----END PRIVATE KEY-----`
)
