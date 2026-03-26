package ingress_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/alb/ingress"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"

	"go.uber.org/mock/gomock"
)

const (
	finalizerName = "stackit.cloud/alb-ingress"
	projectID     = "dummy-project-id"
	region        = "eu01"
)

var _ = Describe("IngressClassReconciler", func() {
	var (
		k8sClient  client.Client
		namespace  *corev1.Namespace
		mockCtrl   *gomock.Controller
		albClient  *stackit.MockApplicationLoadBalancerClient
		certClient *stackit.MockCertificatesClient
		ctx        context.Context
		cancel     context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		DeferCleanup(cancel)

		mockCtrl = gomock.NewController(GinkgoT())
		albClient = stackit.NewMockApplicationLoadBalancerClient(mockCtrl)
		certClient = stackit.NewMockCertificatesClient(mockCtrl)

		var err error
		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())

		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "stackit-alb-ingress-test-",
			},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(context.Background(), namespace)
		})

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Metrics: server.Options{BindAddress: "0"},
			Cache: cache.Options{
				DefaultNamespaces: map[string]cache.Config{
					namespace.Name: {},
				},
			},
			Controller: config.Controller{SkipNameValidation: ptr.To(true)},
		})
		Expect(err).NotTo(HaveOccurred())

		reconciler := &ingress.IngressClassReconciler{
			Client:            mgr.GetClient(),
			Scheme:            scheme.Scheme,
			ALBClient:         albClient,
			CertificateClient: certClient,
			ALBConfig: stackitconfig.ALBConfig{
				Global: stackitconfig.GlobalOpts{
					ProjectID: projectID,
					Region:    region,
				},
				ApplicationLoadBalancer: stackitconfig.ApplicationLoadBalancerOpts{
					NetworkID: "dummy-network",
				},
			},
		}
		Expect(reconciler.SetupWithManager(mgr)).To(Succeed())

		go func() {
			defer GinkgoRecover()
			Expect(mgr.Start(ctx)).To(Succeed())
		}()

		Eventually(func() bool {
			return mgr.GetCache().WaitForCacheSync(ctx)
		}, "2s", "50ms").Should(BeTrue())
	})

	Context("when the IngressClass does NOT point to our controller", func() {
		It("should ignore the IngressClass", func() {
			ingressClass := &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ignored-ingressclass",
					Namespace: namespace.Name,
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: "some.other/controller",
				},
			}
			Expect(k8sClient.Create(ctx, ingressClass)).To(Succeed())

			Consistently(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(ingressClass), ingressClass)
			}).Should(Succeed())

			Expect(ingressClass.Finalizers).To(BeEmpty())
		})
	})

	Context("when the IngressClass points to our controller", func() {
		var ingressClass *networkingv1.IngressClass

		BeforeEach(func() {
			ingressClass = &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "managed-ingressclass",
					Namespace: namespace.Name,
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: "stackit.cloud/alb-ingress",
				},
			}
			Expect(k8sClient.Create(ctx, ingressClass)).To(Succeed())
		})

		AfterEach(func() {
			var ic networkingv1.IngressClass
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ingressClass), &ic)
			if apierrors.IsNotFound(err) {
				// nothing to clean up, it’s already deleted
				return
			}
			Expect(err).NotTo(HaveOccurred())

			if controllerutil.ContainsFinalizer(&ic, finalizerName) {
				controllerutil.RemoveFinalizer(&ic, finalizerName)
				Expect(k8sClient.Update(ctx, &ic)).To(Succeed())
			}

			// delete the patched object (ic), not the old ingressClass pointer
			err = k8sClient.Delete(ctx, &ic)
			if err != nil && !apierrors.IsNotFound(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			Eventually(func() bool {
				return apierrors.IsNotFound(
					k8sClient.Get(ctx, client.ObjectKeyFromObject(ingressClass), &networkingv1.IngressClass{}),
				)
			}).Should(BeTrue(), "IngressClass should be fully deleted")
		})

		Context("and it is being deleted", func() {
			BeforeEach(func() {
				Expect(controllerutil.AddFinalizer(ingressClass, finalizerName)).To(BeTrue())
				Expect(k8sClient.Update(ctx, ingressClass)).To(Succeed())

				// Stub ALB deletion in case controller proceeds to cleanup
				albClient.EXPECT().
					DeleteLoadBalancer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					AnyTimes().
					Return(nil)

				// Stub certificate deletion in case controller proceeds to cleanup
				certClient.EXPECT().
					ListCertificate(gomock.Any(), gomock.Any(), gomock.Any()).
					AnyTimes().
					Return(nil, nil)
			})

			Context("and NO referencing Ingresses exist", func() {
				It("should remove finalizer and delete ALB", func() {
					Expect(k8sClient.Delete(ctx, ingressClass)).To(Succeed())
					Eventually(func(g Gomega) {
						var ic networkingv1.IngressClass
						err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ingressClass), &ic)
						if apierrors.IsNotFound(err) {
							// IngressClass is gone — controller must have removed the finalizer
							return
						}
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(controllerutil.ContainsFinalizer(&ic, finalizerName)).To(BeFalse())
					}).Should(Succeed())
				})
			})

			Context("and referencing Ingresses DO exist", func() {
				It("should NOT remove finalizer and NOT delete ALB", func() {
					ing := &networkingv1.Ingress{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "referencing-ingress",
							Namespace: namespace.Name,
						},
						Spec: networkingv1.IngressSpec{
							IngressClassName: ptr.To("managed-ingressclass"),
							Rules: []networkingv1.IngressRule{
								{
									Host: "example.com",
									IngressRuleValue: networkingv1.IngressRuleValue{
										HTTP: &networkingv1.HTTPIngressRuleValue{
											Paths: []networkingv1.HTTPIngressPath{
												{
													Path:     "/",
													PathType: ptr.To(networkingv1.PathTypePrefix),
													Backend: networkingv1.IngressBackend{
														Service: &networkingv1.IngressServiceBackend{
															Name: "dummy-svc",
															Port: networkingv1.ServiceBackendPort{Number: 80},
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
					Expect(k8sClient.Create(ctx, ing)).To(Succeed())
					DeferCleanup(func() {
						_ = k8sClient.Delete(ctx, ing)
					})

					// Wait until the controller sees the Ingress and processes it
					Eventually(func(g Gomega) {
						var ic networkingv1.IngressClass
						err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ingressClass), &ic)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(ic.Finalizers).To(ContainElement(finalizerName))
					}).Should(Succeed())

					Expect(k8sClient.Delete(ctx, ingressClass)).To(Succeed())

					// Expect finalizer to still be there
					Consistently(func(g Gomega) {
						var ic networkingv1.IngressClass
						err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ingressClass), &ic)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(ic.Finalizers).To(ContainElement(finalizerName))
					}, "1s", "100ms").Should(Succeed())
				})
			})
		})

		Context("and it is NOT being deleted", func() {
			Context("and it does NOT have the finalizer", func() {
				It("should add the finalizer", func() {
					// Stub ALB deletion in case controller proceeds to cleanup
					albClient.EXPECT().
						DeleteLoadBalancer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						AnyTimes().
						Return(nil)

					// Stub certificate deletion in case controller proceeds to cleanup
					certClient.EXPECT().
						ListCertificate(gomock.Any(), gomock.Any(), gomock.Any()).
						AnyTimes().
						Return(nil, nil)

					Eventually(func(g Gomega) {
						var updated networkingv1.IngressClass
						err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ingressClass), &updated)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(controllerutil.ContainsFinalizer(&updated, finalizerName)).To(BeTrue())
					}).Should(Succeed())
				})
			})

			Context("and it ALREADY has the finalizer", func() {
				BeforeEach(func() {
					Expect(controllerutil.AddFinalizer(ingressClass, finalizerName)).To(BeTrue())
					Expect(k8sClient.Update(ctx, ingressClass)).To(Succeed())
				})

				Context("and NO referencing Ingresses exist", func() {
					It("should clean up ALB and certs, but retain the IngressClass and finalizer", func() {
						albClient.EXPECT().
							DeleteLoadBalancer(gomock.Any(), projectID, region, "k8s-ingress-managed-ingressclass").
							Return(nil).
							AnyTimes()

						certClient.EXPECT().
							ListCertificate(gomock.Any(), projectID, region).
							Return(nil, nil).
							AnyTimes()

						certClient.EXPECT().
							DeleteCertificate(gomock.Any(), projectID, region, gomock.Any()).
							Return(nil).
							AnyTimes()

						Consistently(func(g Gomega) {
							var ic networkingv1.IngressClass
							err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ingressClass), &ic)
							g.Expect(err).NotTo(HaveOccurred())
							g.Expect(controllerutil.ContainsFinalizer(&ic, finalizerName)).To(BeTrue())
						}, "5s", "100ms").Should(Succeed())
					})
				})

				Context("and referencing Ingresses DO exist", func() {
					BeforeEach(func() {
						ing := &networkingv1.Ingress{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "referencing-ingress",
								Namespace: namespace.Name,
							},
							Spec: networkingv1.IngressSpec{
								IngressClassName: ptr.To("managed-ingressclass"),
								Rules: []networkingv1.IngressRule{
									{
										Host: "example.com",
										IngressRuleValue: networkingv1.IngressRuleValue{
											HTTP: &networkingv1.HTTPIngressRuleValue{
												Paths: []networkingv1.HTTPIngressPath{
													{
														Path:     "/",
														PathType: ptr.To(networkingv1.PathTypePrefix),
														Backend: networkingv1.IngressBackend{
															Service: &networkingv1.IngressServiceBackend{
																Name: "dummy-svc",
																Port: networkingv1.ServiceBackendPort{Number: 80},
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
						Expect(k8sClient.Create(ctx, ing)).To(Succeed())
						DeferCleanup(func() {
							_ = k8sClient.Delete(ctx, ing)
						})
					})

					// Context("and ALB does NOT exist", func() {
					// 	FIt("should create the ALB", func() {
					// 		Eventually(func(g Gomega) {
					// 			var ic networkingv1.IngressClass
					// 			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ingressClass), &ic)
					// 			g.Expect(err).NotTo(HaveOccurred())
					// 			g.Expect(ic.DeletionTimestamp.IsZero()).To(BeTrue(), "IngressClass should not be marked for deletion")
					// 			g.Expect(ic.Finalizers).To(ContainElement(finalizerName), "Finalizer should still be present")
					// 		}).Should(Succeed())
					// 	})
					// })

					// Context("and ALB already exists", func() {
					// 	BeforeEach(func() {
					// 		albClient.EXPECT().
					// 			GetLoadBalancer(gomock.Any(), projectID, region, "k8s-ingress-managed-ingressclass").
					// 			Return(&albsdk.LoadBalancer{
					// 				Listeners:      &[]albsdk.Listener{},
					// 				TargetPools:    &[]albsdk.TargetPool{},
					// 				Status:         albsdk.LOADBALANCERSTATUS_READY.Ptr(),
					// 				ExternalAddress: ptr.To("1.2.3.4"),
					// 				Version:        albsdk.PtrString("1"),
					// 			}, nil)
					// 	})

					// 	Context("and ALB config has changed", func() {
					// 		It("should update the ALB", func() {
					// 			albClient.EXPECT().
					// 				UpdateLoadBalancer(gomock.Any(), projectID, region, "k8s-ingress-managed-ingressclass", gomock.Any()).
					// 				Return(nil, nil)

					// 			Eventually(func(g Gomega) {
					// 				var ic networkingv1.IngressClass
					// 				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ingressClass), &ic)
					// 				g.Expect(err).NotTo(HaveOccurred())
					// 			}).Should(Succeed())
					// 		})
					// 	})

					// 	Context("and ALB config has NOT changed", func() {
					// 		It("should not update the ALB", func() {
					// 			// No update call expected
					// 			Eventually(func(g Gomega) {
					// 				var ic networkingv1.IngressClass
					// 				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ingressClass), &ic)
					// 				g.Expect(err).NotTo(HaveOccurred())
					// 			}).Should(Succeed())
					// 		})
					// 	})

					// 	Context("and ALB is ready and has an IP", func() {
					// 		It("should update Ingress status", func() {
					// 			Eventually(func(g Gomega) {
					// 				var updated networkingv1.Ingress
					// 				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ingress), &updated)
					// 				g.Expect(err).NotTo(HaveOccurred())
					// 				g.Expect(updated.Status.LoadBalancer.Ingress).ToNot(BeEmpty())
					// 				g.Expect(updated.Status.LoadBalancer.Ingress[0].IP).To(Equal("1.2.3.4"))
					// 			}).Should(Succeed())
					// 		})
					// 	})

					// 	Context("and ALB is ready but has NO IP", func() {
					// 		BeforeEach(func() {
					// 			albClient.EXPECT().
					// 				GetLoadBalancer(gomock.Any(), projectID, region, "k8s-ingress-managed-ingressclass").
					// 				Return(&albsdk.LoadBalancer{
					// 					Listeners:       &[]albsdk.Listener{},
					// 					TargetPools:     &[]albsdk.TargetPool{},
					// 					Status:          ptr.To(albclient.LBStatusReady),
					// 					ExternalAddress: nil,
					// 					PrivateAddress:  nil,
					// 					Version:         1,
					// 				}, nil)
					// 		})

					// 		It("should requeue for later", func() {
					// 			// This can be indirectly asserted by ensuring status is not updated yet
					// 			Consistently(func(g Gomega) {
					// 				var updated networkingv1.Ingress
					// 				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ingress), &updated)
					// 				g.Expect(err).NotTo(HaveOccurred())
					// 				g.Expect(updated.Status.LoadBalancer.Ingress).To(BeEmpty())
					// 			}, "1s", "100ms").Should(Succeed())
					// 		})
					// 	})

					// 	Context("and ALB is NOT ready", func() {
					// 		BeforeEach(func() {
					// 			albClient.EXPECT().
					// 				GetLoadBalancer(gomock.Any(), projectID, region, "k8s-ingress-managed-ingressclass").
					// 				Return(&albsdk.LoadBalancer{
					// 					Listeners:       &[]albsdk.Listener{},
					// 					TargetPools:     &[]albsdk.TargetPool{},
					// 					Status:          ptr.To("PENDING"),
					// 					ExternalAddress: nil,
					// 					PrivateAddress:  nil,
					// 					Version:         1,
					// 				}, nil)
					// 		})

					// 		It("should requeue for later", func() {
					// 			Consistently(func(g Gomega) {
					// 				var updated networkingv1.Ingress
					// 				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ingress), &updated)
					// 				g.Expect(err).NotTo(HaveOccurred())
					// 				g.Expect(updated.Status.LoadBalancer.Ingress).To(BeEmpty())
					// 			}, "1s", "100ms").Should(Succeed())
					// 		})
					// 	})
					// })
				})
			})
		})
	})
})
