package ingress

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	"google.golang.org/protobuf/testing/protocmp"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
)

const (
	testNetworkID   = "test-network"
	testHost        = "example.com"
	testPath        = "/"
	testNodeName    = "node-0"
	testNodeIP      = "1.1.1.1"
	testServiceName = "test-service"
	testServicePort = 80
	testNodePort    = 30080
)

func ingressPrefixPath(path, serviceName string) networkingv1.HTTPIngressPath {
	return networkingv1.HTTPIngressPath{
		Path:     path,
		PathType: new(networkingv1.PathTypePrefix),
		Backend: networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{
				Name: serviceName,
				Port: networkingv1.ServiceBackendPort{Number: testServicePort},
			},
		},
	}
}

func ingressExactPath(path, serviceName string) networkingv1.HTTPIngressPath {
	return networkingv1.HTTPIngressPath{
		Path:     path,
		PathType: new(networkingv1.PathTypeExact),
		Backend: networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{
				Name: serviceName,
				Port: networkingv1.ServiceBackendPort{Number: testServicePort},
			},
		},
	}
}

func ingressRule(host string, paths ...networkingv1.HTTPIngressPath) networkingv1.IngressRule {
	return networkingv1.IngressRule{
		Host: host,
		IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{Paths: paths},
		},
	}
}

func fixtureIngressWithParams(name, namespace string, annotations map[string]string, rules ...networkingv1.IngressRule) networkingv1.Ingress {
	return networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: new(testIngressClassName),
			Rules:            rules,
		},
	}
}

func fixtureServiceWithParams(name string, port, nodePort int32) *corev1.Service { //nolint:unparam // We might need it later.
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{
				{
					Port:     port,
					NodePort: nodePort,
				},
			},
		},
	}
}

func fixtureNode(mods ...func(*corev1.Node)) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: testNodeName},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: testNodeIP}},
		},
	}
	for _, mod := range mods {
		mod(node)
	}
	return node
}

func fixtureIngress(mods ...func(networkingv1.Ingress)) networkingv1.Ingress {
	ingress := networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: testIngressName, Annotations: nil},
		Spec: networkingv1.IngressSpec{
			IngressClassName: new(testIngressClassName),
			Rules: []networkingv1.IngressRule{
				{
					Host: testHost,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     testPath,
									PathType: new(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: testServiceName,
											Port: networkingv1.ServiceBackendPort{Number: testServicePort},
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
	for _, mod := range mods {
		mod(ingress)
	}
	return ingress
}

func fixtureIngressClass(mods ...func(*networkingv1.IngressClass)) networkingv1.IngressClass {
	ingressClass := networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: testIngressClassName, UID: testIngressClassUID, Finalizers: []string{finalizerName}},
		Spec:       networkingv1.IngressClassSpec{Controller: controllerName},
	}
	for _, mod := range mods {
		mod(&ingressClass)
	}
	return ingressClass
}

func fixtureAlbPayload(mods ...func(*albsdk.CreateLoadBalancerPayload)) *albsdk.CreateLoadBalancerPayload {
	payload := &albsdk.CreateLoadBalancerPayload{
		Name:                                 new(testIngressClassUID),
		DisableTargetSecurityGroupAssignment: new(true),
		Labels: new(map[string]string{
			"alb-ingress-controller-ingress-class-uid": "11111111-2222-3333-4444-555555555555",
		}),
		Listeners: []albsdk.Listener{
			{
				Name:     new("80-http"),
				Port:     new(int32(80)),
				Protocol: new("PROTOCOL_HTTP"),
				Http: &albsdk.ProtocolOptionsHTTP{
					Hosts: []albsdk.HostConfig{
						{
							Host: new(testHost),
							Rules: []albsdk.Rule{
								{
									Path: &albsdk.Path{
										Prefix: new(testPath),
									},
									TargetPool: new("port-30080"),
									WebSocket:  new(false),
								},
							},
						},
					},
				},
			},
		},
		Networks: []albsdk.Network{{NetworkId: new(testNetworkID), Role: new("ROLE_LISTENERS_AND_TARGETS")}},
		Options:  &albsdk.LoadBalancerOptions{EphemeralAddress: new(true)},
		TargetPools: []albsdk.TargetPool{
			{Name: new("port-30080"), TargetPort: new(int32(30080)), Targets: []albsdk.Target{{DisplayName: new(testNodeName), Ip: new(testNodeIP)}}},
		},
	}
	for _, mod := range mods {
		mod(payload)
	}
	return payload
}

//nolint:funlen // Just many test cases.
func Test_albSpecFromIngress(t *testing.T) {
	r := &IngressClassReconciler{
		ALBConfig: config.ALBConfig{
			ApplicationLoadBalancer: config.ApplicationLoadBalancerOpts{
				NetworkID: testNetworkID,
			},
		},
	}
	nodes := []*corev1.Node{fixtureNode()}

	tests := []struct {
		name          string
		ingresses     []networkingv1.Ingress
		ingressClass  networkingv1.IngressClass
		services      []*corev1.Service
		want          *albsdk.CreateLoadBalancerPayload
		wantErr       bool
		wantErrorList []error
	}{
		{
			name:          "valid ingress with HTTP listener",
			ingresses:     []networkingv1.Ingress{fixtureIngress()},
			ingressClass:  fixtureIngressClass(),
			services:      []*corev1.Service{fixtureServiceWithParams(testServiceName, testServicePort, testNodePort)},
			want:          fixtureAlbPayload(),
			wantErr:       false,
			wantErrorList: []error{},
		},
		{
			name:      "valid ingress with HTTP listener with external ip address",
			ingresses: []networkingv1.Ingress{fixtureIngress()},
			ingressClass: fixtureIngressClass(
				func(ic *networkingv1.IngressClass) {
					ic.Annotations = map[string]string{AnnotationExternalIP: "2.2.2.2"}
				},
			),
			services: []*corev1.Service{fixtureServiceWithParams(testServiceName, testServicePort, testNodePort)},
			want: fixtureAlbPayload(func(payload *albsdk.CreateLoadBalancerPayload) {
				payload.ExternalAddress = new("2.2.2.2")
				payload.Options = &albsdk.LoadBalancerOptions{EphemeralAddress: nil}
			}),
			wantErrorList: []error{},
		},
		{
			name:      "valid ingress with HTTP listener with internal ip address",
			ingresses: []networkingv1.Ingress{fixtureIngress()},
			ingressClass: fixtureIngressClass(
				func(ic *networkingv1.IngressClass) {
					ic.Annotations = map[string]string{AnnotationInternal: "true"}
				},
			),
			services: []*corev1.Service{fixtureServiceWithParams(testServiceName, testServicePort, testNodePort)},
			want: fixtureAlbPayload(func(payload *albsdk.CreateLoadBalancerPayload) {
				payload.Options = &albsdk.LoadBalancerOptions{PrivateNetworkOnly: new(true), EphemeralAddress: new(false)}
			}),
			wantErrorList: []error{},
		},
		{
			name:         "same path prefix in same ingress",
			ingressClass: fixtureIngressClass(),
			ingresses: []networkingv1.Ingress{fixtureIngress(
				func(ing networkingv1.Ingress) {
					ing.Spec.Rules[0].HTTP.Paths[0].Path = "/x"

					// second rule with the same path prefix as the first rule
					ing.Spec.Rules[0].HTTP.Paths = append(ing.Spec.Rules[0].HTTP.Paths, networkingv1.HTTPIngressPath{
						Path:     "/x",
						PathType: new(networkingv1.PathTypePrefix),
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: "svc1",
								Port: networkingv1.ServiceBackendPort{Number: testServicePort},
							},
						},
					})
				},
			)},

			services: []*corev1.Service{
				fixtureServiceWithParams(testServiceName, testServicePort, testNodePort),
				fixtureServiceWithParams("svc1", testServicePort, 30003),
			},
			want: fixtureAlbPayload(func(p *albsdk.CreateLoadBalancerPayload) {
				p.Listeners[0].Http.Hosts[0].Rules = []albsdk.Rule{
					{Path: &albsdk.Path{Prefix: new("/x")}, TargetPool: new("port-30080"), WebSocket: new(false)},
				}
				p.TargetPools = []albsdk.TargetPool{
					{Name: new("port-30080"), TargetPort: new(int32(30080)), Targets: []albsdk.Target{{DisplayName: new(testNodeName), Ip: new(testNodeIP)}}},
				}
			}),
			wantErrorList: []error{
				&errorEvent{description: `path "/x" already exists within same ingress`, typ: "error"},
			},
		},
		/*
			// TODO: Fix this test case once ordering and sorting rules are adjusted.
			{
				name:         "path type ordering (Exact before Prefix)",
				ingressClass: fixtureIngressClass(),
				ingresses: []networkingv1.Ingress{
					fixtureIngressWithParams("ingress", "ns", nil,
						ingressRule("host.com",
							ingressExactPath("/same", "svc-exact"),
							ingressPrefixPath("/same", "svc-prefix"),
						),
					),
				},
				services: []*corev1.Service{
					fixtureServiceWithParams("svc-exact", testServicePort, 30100),
					fixtureServiceWithParams("svc-prefix", testServicePort, 30101),
				},
				want: fixtureAlbPayload(func(p *albsdk.CreateLoadBalancerPayload) {
					p.Listeners[0].Http.Hosts[0].Host = new("host.com")
					p.Listeners[0].Http.Hosts[0].Rules = []albsdk.Rule{
						{Path: &albsdk.Path{ExactMatch: new("/same")}, TargetPool: new("pool-30100")},
						{Path: &albsdk.Path{Prefix: new("/same")}, TargetPool: new("pool-30101")},
					}
					p.TargetPools = []albsdk.TargetPool{
						{Name: new("pool-30100"), TargetPort: new(int32(30100)), Targets: []albsdk.Target{{DisplayName: new(testNodeName), Ip: new(testNodeIP)}}},
						{Name: new("pool-30101"), TargetPort: new(int32(30101)), Targets: []albsdk.Target{{DisplayName: new(testNodeName), Ip: new(testNodeIP)}}},
					}
				}),
			},

		*/
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := []client.Object{}
			for _, svc := range tt.services {
				obj = append(obj, svc)
			}

			for _, node := range nodes {
				obj = append(obj, node)
			}

			r.Client = fake.NewClientBuilder().
				WithObjects(obj...).
				Build()

			got, errList, err := r.getAlbSpecForIngresses(context.TODO(), &tt.ingressClass, tt.ingresses)
			if (err != nil) != tt.wantErr {
				t.Errorf("got error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("got %v, want %v, diff=%s", got, tt.want, diff)
			}

			if len(errList) > 0 {
				if diff := cmp.Diff(tt.wantErrorList, errList, cmp.AllowUnexported(errorEvent{})); diff != "" {
					t.Errorf("errList mismatch (-want +got):\n%s", diff)
				}
			}

		})
	}
}
