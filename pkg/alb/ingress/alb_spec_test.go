package ingress

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	stackitmocks "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
	certsdk "github.com/stackitcloud/stackit-sdk-go/services/certificates/v2api"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Node Controller", func() {
	var (
		k8sClient  client.Client
		mockCtrl   *gomock.Controller
		certClient *stackitmocks.MockCertificatesClient

		ingressClass networkingv1.IngressClass
		ingress      networkingv1.Ingress
		service      corev1.Service
		node         corev1.Node

		reconciler IngressClassReconciler

		albSpec albsdk.CreateLoadBalancerPayload
	)

	BeforeEach(func() {
		networkID := "my-network"

		ingressClass = networkingv1.IngressClass{
			ObjectMeta: metav1.ObjectMeta{Name: "test-ingress-class", UID: "test-ingress-class-uid"},
			Spec:       networkingv1.IngressClassSpec{Controller: controllerName},
		}

		service = corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "test-service"},
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

		node = corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.10.10.10"}},
			},
		}

		ingress = testIngress(&ingressClass, &service)

		k8sClient = fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			Build()

		Expect(k8sClient.Create(context.Background(), &ingressClass)).To(Succeed())
		Expect(k8sClient.Create(context.Background(), &ingress)).To(Succeed())
		Expect(k8sClient.Create(context.Background(), &service)).To(Succeed())
		Expect(k8sClient.Create(context.Background(), &node)).To(Succeed())

		reconciler = IngressClassReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
			ALBConfig: stackitconfig.ALBConfig{
				Global: stackitconfig.GlobalOpts{
					ProjectID: "test-project",
					Region:    "test-region",
				},
				ApplicationLoadBalancer: stackitconfig.ApplicationLoadBalancerOpts{NetworkID: networkID}},
		}

		albSpec = albsdk.CreateLoadBalancerPayload{
			DisableTargetSecurityGroupAssignment: new(true),
			Labels:                               new(map[string]string{LabelIngressClassUID: "test-ingress-class-uid"}),
			Listeners: []albsdk.Listener{
				{
					Http: new(albsdk.ProtocolOptionsHTTP{
						Hosts: []albsdk.HostConfig{
							{
								Host: new("example.com"),
								Rules: []albsdk.Rule{
									{
										Path: new(albsdk.Path{
											Prefix: new("/"),
										}),
										TargetPool: new(fmt.Sprintf("port-%d", service.Spec.Ports[0].NodePort)),
										WebSocket:  new(false),
									},
								},
							},
						},
					}),
					Name:     new("80-http"),
					Port:     new(int32(80)),
					Protocol: new("PROTOCOL_HTTP"),
				},
			},
			Name: new(string(ingressClass.UID)), //todo
			Networks: []albsdk.Network{
				{
					NetworkId: new(reconciler.ALBConfig.ApplicationLoadBalancer.NetworkID),
					Role:      new("ROLE_LISTENERS_AND_TARGETS"),
				},
			},
			Options: new(albsdk.LoadBalancerOptions{
				EphemeralAddress: new(true),
			}),
			// Region:         new(reconciler.ALBConfig.Global.Region), why is there a region in spec? TODO
			TargetPools: []albsdk.TargetPool{
				{
					Name:       new(fmt.Sprintf("port-%d", service.Spec.Ports[0].NodePort)),
					TargetPort: new(service.Spec.Ports[0].NodePort),
					Targets: []albsdk.Target{
						{
							DisplayName: new(node.Name),
							Ip:          new(node.Status.Addresses[0].Address),
						},
					},
				},
			},
		}

	})

	Describe("Generate ALB spec", func() {
		It("should work with basic setup", func() {
			spec, errorEventList, err := reconciler.getAlbSpecForIngressClass(context.Background(), &ingressClass)
			Expect(err).To(Succeed())
			Expect(errorEventList).To(BeEmpty())

			Expect(spec).ToNot(BeNil())
			Expect(*spec).To(BeEquivalentTo(albSpec))
		})

		It("should work with labels", func() {

			reconciler.ALBConfig.ApplicationLoadBalancer.ExtraLabels = map[string]string{"managed-by": "alb-ingressClass"}
			// adding extra labels to albSpec.Labels map
			for k, v := range reconciler.ALBConfig.ApplicationLoadBalancer.ExtraLabels {
				(*albSpec.Labels)[k] = v
			}
			spec, errorEventList, err := reconciler.getAlbSpecForIngressClass(context.Background(), &ingressClass)
			Expect(err).To(Succeed())
			Expect(errorEventList).To(BeEmpty())

			Expect(spec).ToNot(BeNil())
			Expect(*spec).To(BeEquivalentTo(albSpec))
		})

		It("should work with certificates", func() {

			mockCtrl = gomock.NewController(GinkgoT())
			certClient = stackitmocks.NewMockCertificatesClient(mockCtrl)

			// Bind this mock instance to live reconciler reference context
			reconciler.CertificateClient = certClient

			certSecret := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-secret-cert",
					UID:  "dummy-secret-uid-value-1234567",
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": []byte("mock-public-key"),
					"tls.key": []byte("mock-private-key"),
				},
			}
			Expect(k8sClient.Create(context.Background(), &certSecret)).To(Succeed())

			actualStoredSecret := &corev1.Secret{}
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "my-secret-cert"}, actualStoredSecret)
			Expect(err).NotTo(HaveOccurred())

			expectedGeneratedCertName := getCertName(&ingressClass, actualStoredSecret)
			targetCertID := "real-certificate-uuid-abc-123"

			mockResponse := &certsdk.GetCertificateResponse{
				Id:   new(targetCertID),
				Name: new(expectedGeneratedCertName),
			}

			certClient.EXPECT().
				CreateCertificate(
					gomock.Any(),
					"test-project",
					"test-region",
					gomock.Any(), // Intercepts any incoming *certsdk.CreateCertificatePayload matching
				).
				Return(mockResponse, nil).
				Times(1)

			httpsIngress := testHttpsIngress(&ingressClass, &service)
			httpsIngress.Annotations = map[string]string{"alb.stackit.cloud/https-only": "true"}

			Expect(k8sClient.Create(context.Background(), new(httpsIngress))).To(Succeed())

			// expected albSpec should include new https listener
			httpListener := testHttpListener(service.Spec.Ports[0].NodePort)
			httpsListener := testHttpsListener(service.Spec.Ports[0].NodePort, targetCertID)
			albSpec.Listeners = []albsdk.Listener{
				httpsListener,
				httpListener,
			}

			// get the specs and compare
			spec, errorEventList, err := reconciler.getAlbSpecForIngressClass(context.Background(), &ingressClass)
			Expect(err).To(Succeed())
			Expect(errorEventList).To(BeEmpty())

			Expect(spec).ToNot(BeNil())

			// compare
			Expect(*spec).To(BeEquivalentTo(albSpec))

		})

		It("should work with 2 ingresses different path", func() {
			ingress2 := testIngress(&ingressClass, &service)
			ingress2.Name = "ingress2"
			ingress2.Spec.Rules[0].HTTP.Paths[0].Path = "/foobar"

			Expect(k8sClient.Create(context.Background(), &ingress2)).To(Succeed())

			secTargetPool := *albSpec.Listeners[0].Http.Hosts[0].Rules[0].TargetPool
			albSpec.Listeners[0].Http.Hosts[0].Rules = []albsdk.Rule{
				{
					Path:       &albsdk.Path{Prefix: new("/foobar")},
					TargetPool: new(secTargetPool),
					WebSocket:  new(false),
				},
				{
					Path:       &albsdk.Path{Prefix: new("/")},
					TargetPool: new(secTargetPool),
					WebSocket:  new(false),
				},
			}

			spec, errorEventList, err := reconciler.getAlbSpecForIngressClass(context.Background(), &ingressClass)
			Expect(err).To(Succeed())
			Expect(errorEventList).To(BeEmpty())

			Expect(spec).ToNot(BeNil())
			Expect(*spec).To(BeEquivalentTo(albSpec))
		})
	})
})

func testIngress(class *networkingv1.IngressClass, service *corev1.Service) networkingv1.Ingress {
	return networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ingress"},
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

func testHttpsIngress(class *networkingv1.IngressClass, service *corev1.Service) networkingv1.Ingress {
	return networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "test-https-ingress"},
		Spec: networkingv1.IngressSpec{
			IngressClassName: new(class.Name),
			TLS: []networkingv1.IngressTLS{
				{
					Hosts:      []string{"secure.example.com"},
					SecretName: "my-secret-cert",
				},
			},
			Rules: []networkingv1.IngressRule{
				{
					Host: "secure.example.com",
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

// Returns a clean, isolated Port 80 HTTP Listener structure payload
func testHttpListener(nodePort int32) albsdk.Listener {
	return albsdk.Listener{
		Name:     new("80-http"),
		Port:     new(int32(80)),
		Protocol: new("PROTOCOL_HTTP"),
		Http: &albsdk.ProtocolOptionsHTTP{
			Hosts: []albsdk.HostConfig{
				{
					Host: new("example.com"),
					Rules: []albsdk.Rule{
						{
							Path:       &albsdk.Path{Prefix: new("/")},
							TargetPool: new(fmt.Sprintf("port-%d", nodePort)),
							WebSocket:  new(false),
						},
					},
				},
			},
		},
	}
}

// Returns a clean, isolated Port 443 HTTPS Listener structure payload containing certificate tracking parameters
func testHttpsListener(nodePort int32, certID string) albsdk.Listener {
	return albsdk.Listener{
		Name:     new("443-https"),
		Port:     new(int32(443)),
		Protocol: new("PROTOCOL_HTTPS"),
		Https: &albsdk.ProtocolOptionsHTTPS{
			CertificateConfig: &albsdk.CertificateConfig{
				CertificateIds: []string{certID},
			},
		},
		Http: &albsdk.ProtocolOptionsHTTP{
			Hosts: []albsdk.HostConfig{
				{
					Host: new("secure.example.com"),
					Rules: []albsdk.Rule{
						{
							Path:       &albsdk.Path{Prefix: new("/")},
							TargetPool: new(fmt.Sprintf("port-%d", nodePort)),
							WebSocket:  new(false),
						},
					},
				},
			},
		},
	}
}
