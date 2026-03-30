package ingress

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Node Controller", func() {
	var (
		k8sClient client.Client

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
			ObjectMeta: metav1.ObjectMeta{Name: "test-ingress-class"},
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

		It("should work with 2 ingresses different path", func() {
			ingress2 := testIngress(&ingressClass, &service)
			ingress2.Name = "ingress2"
			ingress2.Spec.Rules[0].HTTP.Paths[0].Path = "/foobar"

			Expect(k8sClient.Create(context.Background(), &ingress2)).To(Succeed())

			albSpec.Listeners[0].Http.Hosts[0].Rules = append(
				albSpec.Listeners[0].Http.Hosts[0].Rules,
				albsdk.Rule{
					Path:       new(albsdk.Path{Prefix: new("/foobar")}),
					TargetPool: albSpec.Listeners[0].Http.Hosts[0].Rules[0].TargetPool,
					WebSocket:  new(false),
				})

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
