package stackit

import (
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/lbapi"
	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	"github.com/stackitcloud/stackit-sdk-go/services/loadbalancer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("lbSpecFromService", func() {
	const (
		externalAddress = "123.124.88.99"
	)

	var (
		http    corev1.ServicePort
		httpAlt corev1.ServicePort
		https   corev1.ServicePort
		dns     corev1.ServicePort
	)
	BeforeEach(func() {
		http = corev1.ServicePort{
			Name:     "http",
			Port:     80,
			Protocol: corev1.ProtocolTCP,
		}
		httpAlt = corev1.ServicePort{
			Name:     "http-alt",
			Port:     8080,
			Protocol: corev1.ProtocolTCP,
		}
		https = corev1.ServicePort{
			Name:     "https",
			Port:     443,
			Protocol: corev1.ProtocolTCP,
		}
		dns = corev1.ServicePort{
			Name:     "dns",
			Port:     53,
			Protocol: corev1.ProtocolUDP,
		}
	})

	Context("internal load balancer", func() {
		It("should return internal load balancer spec", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb": "true",
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec).To(PointTo(MatchFields(IgnoreExtras, Fields{
				"Options": PointTo(MatchFields(IgnoreExtras, Fields{
					"PrivateNetworkOnly": PointTo(BeTrue()),
				})),
			})))
		})

		It("should return internal load balancer spec with yawol annotation", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"yawol.stackit.cloud/internalLB": "true",
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec).To(PointTo(MatchFields(IgnoreExtras, Fields{
				"Options": PointTo(MatchFields(IgnoreExtras, Fields{
					"PrivateNetworkOnly": PointTo(BeTrue()),
				})),
			})))
		})

		It("should error if value for internal network doesn't parse as bool", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb": "maybe",
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(MatchError(ContainSubstring("invalid bool")))
		})

		It("should error if values for internal network are incompatible", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":   "true",
						"yawol.stackit.cloud/internalLB": "false",
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(MatchError(ContainSubstring("incompatible values")))
		})

		It("should not set floating IP on internal load balancers", func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":      "true",
						"lb.stackit.cloud/external-address": externalAddress,
					},
				},
			}
			spec, err := lbSpecFromService(svc, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.Options.PrivateNetworkOnly).To(PointTo(BeTrue()))
			Expect(spec.ExternalAddress).To(BeNil())
		})
	})

	Context("external IP", func() {
		It("should take external IP from annotation", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address": externalAddress,
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.ExternalAddress).To(PointTo(Equal(externalAddress)))
		})

		It("should take external IP from yawol annotation", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"yawol.stackit.cloud/existingFloatingIP": externalAddress,
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.ExternalAddress).To(PointTo(Equal(externalAddress)))
		})

		It("should error on incompatible values for external IP", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address":      "123.124.88.99",
						"yawol.stackit.cloud/existingFloatingIP": "55.66.77.88",
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(MatchError(ContainSubstring("incompatible values")))
		})

		It("should error if external IP is not a valid IP", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address": "I'm not an IP",
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(HaveOccurred())
		})

		It("should error if external IP is an IPv6 address", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address": "2001:db8::",
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Metric metricsRemoteWrite", func() {
		It("should set metrics in load balancer spec", func() {
			pushURL := "test-endpoint"
			spec, err := lbSpecFromService(&corev1.Service{}, []*corev1.Node{}, "my-network", &loadbalancer.LoadbalancerOptionObservability{
				Metrics: &loadbalancer.LoadbalancerOptionMetrics{
					CredentialsRef: ptr.To(sampleCredentialsRef),
					PushUrl:        &pushURL,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(spec).To(PointTo(MatchFields(IgnoreExtras, Fields{
				"Options": PointTo(MatchFields(IgnoreExtras, Fields{
					"Observability": PointTo(MatchFields(IgnoreExtras, Fields{
						"Metrics": PointTo(MatchFields(IgnoreExtras, Fields{
							"CredentialsRef": PointTo(Equal(sampleCredentialsRef)),
							"PushUrl":        PointTo(Equal(pushURL)),
						})),
					})),
				})),
			})))
		})
	})

	Context("TCP proxy protocol ", func() {
		It("should set all TCP ports to TCP protocol protocol", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":        "true",
						"lb.stackit.cloud/tcp-proxy-protocol": "true",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{http, dns, httpAlt},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec).To(PointTo(MatchFields(IgnoreExtras, Fields{
				"Listeners": PointTo(ConsistOf(
					MatchFields(IgnoreExtras, Fields{
						"DisplayName": PointTo(Equal("http")),
						"Protocol":    PointTo(Equal(lbapi.ProtocolTCPProxy)),
					}),
					MatchFields(IgnoreExtras, Fields{
						"DisplayName": PointTo(Equal("http-alt")),
						"Protocol":    PointTo(Equal(lbapi.ProtocolTCPProxy)),
					}),
					MatchFields(IgnoreExtras, Fields{
						"DisplayName": PointTo(Equal("dns")),
						"Protocol":    PointTo(Equal(lbapi.ProtocolUDP)),
					}),
				)),
			})))
			Expect(spec).To(haveConsistentTargetPool())
		})

		It("should only set TCP proxy protocol if covered by filter", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":                     "true",
						"lb.stackit.cloud/tcp-proxy-protocol":              "true",
						"lb.stackit.cloud/tcp-proxy-protocol-ports-filter": "8080,80",
						"yawol.stackit.cloud/tcpProxyProtocol":             "true",
						"yawol.stackit.cloud/tcpProxyProtocolPortsFilter":  "8080,80",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{http, httpAlt, https},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec).To(PointTo(MatchFields(IgnoreExtras, Fields{
				"Listeners": PointTo(ConsistOf(
					MatchFields(IgnoreExtras, Fields{
						"DisplayName": PointTo(Equal("http")),
						"Protocol":    PointTo(Equal(lbapi.ProtocolTCPProxy)),
					}),
					MatchFields(IgnoreExtras, Fields{
						"DisplayName": PointTo(Equal("http-alt")),
						"Protocol":    PointTo(Equal(lbapi.ProtocolTCPProxy)),
					}),
					MatchFields(IgnoreExtras, Fields{
						"DisplayName": PointTo(Equal("https")),
						"Protocol":    PointTo(Equal(lbapi.ProtocolTCP)),
					}),
				)),
			})))
			Expect(spec).To(haveConsistentTargetPool())
		})

		It("should not set TCP proxy protocol on empty filter", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":                     "true",
						"lb.stackit.cloud/tcp-proxy-protocol":              "true",
						"lb.stackit.cloud/tcp-proxy-protocol-ports-filter": "",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{http},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec).To(PointTo(MatchFields(IgnoreExtras, Fields{
				"Listeners": PointTo(ConsistOf(
					MatchFields(IgnoreExtras, Fields{
						"DisplayName": PointTo(Equal("http")),
						"Protocol":    PointTo(Equal(lbapi.ProtocolTCP)),
					}),
				)),
			})))
			Expect(spec).To(haveConsistentTargetPool())
		})

		It("should error on incompatible values for TCP proxy", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":         "true",
						"lb.stackit.cloud/tcp-proxy-protocol":  "true",
						"yawol.stackit.cloud/tcpProxyProtocol": "false",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{http},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(MatchError(ContainSubstring("incompatible values")))
		})

		It("should error on out range port", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":                     "true",
						"lb.stackit.cloud/tcp-proxy-protocol":              "true",
						"lb.stackit.cloud/tcp-proxy-protocol-ports-filter": "66000",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{http},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(MatchError(ContainSubstring("invalid port")))
		})

		It("should error on incompatible values for TCP proxy port filter", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":                     "true",
						"lb.stackit.cloud/tcp-proxy-protocol":              "true",
						"lb.stackit.cloud/tcp-proxy-protocol-ports-filter": "80",
						"yawol.stackit.cloud/tcpProxyProtocolPortsFilter":  "8080",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{http},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(MatchError(ContainSubstring("incompatible values")))
		})
	})

	Context("ports", func() {
		It("should create one listener per port", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address": externalAddress,
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{http, dns},
				},
			}, []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.2.3.4"}},
					},
				},
			}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.Listeners).To(PointTo(ConsistOf(
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("http")),
					"Protocol":    PointTo(Equal(lbapi.ProtocolTCP)),
					"Port":        PointTo(BeNumerically("==", 80)),
					"TargetPool":  PointTo(Equal("http")),
				}),
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("dns")),
					"Protocol":    PointTo(Equal(lbapi.ProtocolUDP)),
					"Port":        PointTo(BeNumerically("==", 53)),
					"TargetPool":  PointTo(Equal("dns")),
				}),
			)))
			Expect(spec).To(haveConsistentTargetPool())
		})

		It("should error on invalid port protocol", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address": externalAddress,
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "nope",
							Port:     8080,
							Protocol: corev1.ProtocolSCTP,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(MatchError(ContainSubstring("unsupported protocol")))
		})

		It("should set listener to default if port name is empty", func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb": "true",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "", // No name
							Port:     80,
							Protocol: corev1.ProtocolTCP,
						},
					},
				},
			}
			spec, err := lbSpecFromService(svc, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.Listeners).To(PointTo(ConsistOf(havePortName("port-tcp-80"))))
			Expect(spec).To(haveConsistentTargetPool())
		})
	})

	Context("source IP ranges", func() {
		It("should take source IP ranges from spec with precende over yawol annotation", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address":            externalAddress,
						"yawol.stackit.cloud/loadBalancerSourceRanges": "2.0.0.0/8,3.0.0.0/8",
					},
				},
				Spec: corev1.ServiceSpec{
					LoadBalancerSourceRanges: []string{
						// All IPs belonging a garage in Palo Alto.
						"15.0.0.0/8",
						"16.0.0.0/8",
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec).To(PointTo(MatchFields(IgnoreExtras, Fields{
				"Options": PointTo(MatchFields(IgnoreExtras, Fields{
					"AccessControl": PointTo(MatchFields(IgnoreExtras, Fields{
						"AllowedSourceRanges": PointTo(Equal([]string{"15.0.0.0/8", "16.0.0.0/8"})),
					})),
				})),
			})))
		})

		It("should take source IP ranges from annotation", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address":            externalAddress,
						"yawol.stackit.cloud/loadBalancerSourceRanges": "2.0.0.0/8,3.0.0.0/8",
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec).To(PointTo(MatchFields(IgnoreExtras, Fields{
				"Options": PointTo(MatchFields(IgnoreExtras, Fields{
					"AccessControl": PointTo(MatchFields(IgnoreExtras, Fields{
						"AllowedSourceRanges": PointTo(Equal([]string{"2.0.0.0/8", "3.0.0.0/8"})),
					})),
				})),
			})))
		})
	})

	Context("target pools", func() {
		It("should set targets on all targets pools", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address": externalAddress,
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{http, httpAlt},
				},
			}, []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.2.3.4"}},
					},
				},
			}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.TargetPools).To(PointTo(HaveLen(2)))
			Expect(spec.TargetPools).To(PointTo(HaveEach(
				haveTargets(ContainElements(loadbalancer.Target{
					DisplayName: utils.Ptr("node-1"),
					Ip:          utils.Ptr("10.2.3.4"),
				})))))
		})

		It("node without internal IP", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address": externalAddress,
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{http},
				},
			}, []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.2.3.4"}},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{{Type: corev1.NodeExternalIP, Address: "4.5.6.7"}},
					},
				},
			}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.TargetPools).To(PointTo(ConsistOf(
				haveTargets(ConsistOf( // node-2 is missing
					loadbalancer.Target{
						DisplayName: utils.Ptr("node-1"),
						Ip:          utils.Ptr("10.2.3.4"),
					},
				)),
			)))
			Expect(spec).To(haveConsistentTargetPool())
		})
	})

	DescribeTable("unsupported annotations",
		func(annotation string) {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/external-address": externalAddress,
						annotation:                          "some value",
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(MatchError(ContainSubstring("unsupported annotation")))
		},
		Entry("yawol.stackit.cloud/imageId", "yawol.stackit.cloud/imageId"),
		Entry("yawol.stackit.cloud/flavorId", "yawol.stackit.cloud/flavorId"),
		Entry("yawol.stackit.cloud/defaultNetworkID", "yawol.stackit.cloud/defaultNetworkID"),
		Entry("yawol.stackit.cloud/skipCloudControllerDefaultNetworkID", "yawol.stackit.cloud/skipCloudControllerDefaultNetworkID"),
		Entry("yawol.stackit.cloud/floatingNetworkID", "yawol.stackit.cloud/floatingNetworkID"),
		Entry("yawol.stackit.cloud/availabilityZone", "yawol.stackit.cloud/availabilityZone"),
		Entry("yawol.stackit.cloud/debug", "yawol.stackit.cloud/debug"),
		Entry("yawol.stackit.cloud/debugsshkey", "yawol.stackit.cloud/debugsshkey"),
		Entry("yawol.stackit.cloud/replicas", "yawol.stackit.cloud/replicas"),
		Entry("yawol.stackit.cloud/logForward", "yawol.stackit.cloud/logForward"),
		Entry("yawol.stackit.cloud/logForwardLokiURL", "yawol.stackit.cloud/logForwardLokiURL"),
		Entry("yawol.stackit.cloud/serverGroupPolicy", "yawol.stackit.cloud/serverGroupPolicy"),
		Entry("yawol.stackit.cloud/additionalNetworks", "yawol.stackit.cloud/additionalNetworks"),
	)

	Context("TCP idle timeout", func() {
		It("should set timeout on all TCP and TCProxy listeners", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":                     "true",
						"lb.stackit.cloud/tcp-idle-timeout":                "15m",
						"lb.stackit.cloud/tcp-proxy-protocol":              "true",
						"lb.stackit.cloud/tcp-proxy-protocol-ports-filter": "443",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-tcp-port",
							Protocol: corev1.ProtocolTCP,
							Port:     80,
						},
						{
							Name:     "my-second-tcp-port",
							Protocol: corev1.ProtocolTCP,
							Port:     8080,
						},
						{
							Name:     "my-tcp-proxy-port",
							Protocol: corev1.ProtocolTCP,
							Port:     443,
						},
						{
							Name:     "my-udp-port",
							Protocol: corev1.ProtocolUDP,
							Port:     53,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.Listeners).To(PointTo(ConsistOf(
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-tcp-port")),
					"Tcp": PointTo(MatchFields(IgnoreExtras, Fields{
						"IdleTimeout": PointTo(Equal("900s")),
					})),
				}),
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-second-tcp-port")),
					"Tcp": PointTo(MatchFields(IgnoreExtras, Fields{
						"IdleTimeout": PointTo(Equal("900s")),
					})),
				}),
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-tcp-proxy-port")),
					"Protocol":    PointTo(Equal("PROTOCOL_TCP_PROXY")),
					"Tcp": PointTo(MatchFields(IgnoreExtras, Fields{
						"IdleTimeout": PointTo(Equal("900s")),
					})),
				}),
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-udp-port")),
					"Tcp":         BeNil(),
				}),
			)))
		})

		It("should set timeout to 60 minutes if no annotation is specified", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb": "true",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-tcp-port",
							Protocol: corev1.ProtocolTCP,
							Port:     80,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.Listeners).To(PointTo(ConsistOf(
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-tcp-port")),
					"Tcp": PointTo(MatchFields(IgnoreExtras, Fields{
						"IdleTimeout": PointTo(Equal("3600s")),
					})),
				}),
			)))
		})

		It("should set timeout based on yawol annotation", func() { //nolint:dupl // It's not a duplicate.
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":       "true",
						"yawol.stackit.cloud/tcpIdleTimeout": "3m",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-tcp-port",
							Protocol: corev1.ProtocolTCP,
							Port:     80,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.Listeners).To(PointTo(ConsistOf(
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-tcp-port")),
					"Tcp": PointTo(MatchFields(IgnoreExtras, Fields{
						"IdleTimeout": PointTo(Equal("180s")),
					})),
				}),
			)))
		})

		It("should error on non-compatible timeouts", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":       "true",
						"lb.stackit.cloud/tcp-idle-timeout":  "15m",
						"yawol.stackit.cloud/tcpIdleTimeout": "3m",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-tcp-port",
							Protocol: corev1.ProtocolTCP,
							Port:     80,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(HaveOccurred())
		})

		It("should error on invalid TCP idle timeout format", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":      "true",
						"lb.stackit.cloud/tcp-idle-timeout": "15x",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-tcp-port",
							Protocol: corev1.ProtocolTCP,
							Port:     80,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(HaveOccurred())
		})

		It("should use default timeout if yawol annotation is invalid", func() { //nolint:dupl // It's not a duplicate.
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":       "true",
						"yawol.stackit.cloud/tcpIdleTimeout": "3x",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-tcp-port",
							Protocol: corev1.ProtocolTCP,
							Port:     80,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.Listeners).To(PointTo(ConsistOf(
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-tcp-port")),
					"Tcp": PointTo(MatchFields(IgnoreExtras, Fields{
						"IdleTimeout": PointTo(Equal("3600s")),
					})),
				}),
			)))
		})
	})

	Context("UDP idle timeout", func() {
		It("should set timeout on all and only on UDP listeners", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":      "true",
						"lb.stackit.cloud/udp-idle-timeout": "15m",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-udp-port",
							Protocol: corev1.ProtocolUDP,
							Port:     53,
						},
						{
							Name:     "my-second-udp-port",
							Protocol: corev1.ProtocolUDP,
							Port:     1000,
						},
						{
							Name:     "my-tcp-port",
							Protocol: corev1.ProtocolTCP,
							Port:     80,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.Listeners).To(PointTo(ConsistOf(
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-tcp-port")),
					"Udp":         BeNil(),
				}),
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-udp-port")),
					"Udp": PointTo(MatchFields(IgnoreExtras, Fields{
						"IdleTimeout": PointTo(Equal("900s")),
					})),
				}),
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-second-udp-port")),
					"Udp": PointTo(MatchFields(IgnoreExtras, Fields{
						"IdleTimeout": PointTo(Equal("900s")),
					})),
				}),
			)))
		})

		It("should set timeout to 2 minutes if no annotation is specified", func() {
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb": "true",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-udp-port",
							Protocol: corev1.ProtocolUDP,
							Port:     53,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.Listeners).To(PointTo(ConsistOf(
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-udp-port")),
					"Udp": PointTo(MatchFields(IgnoreExtras, Fields{
						"IdleTimeout": PointTo(Equal("120s")),
					})),
				}),
			)))
		})

		It("should set timeout based on yawol annotation", func() { //nolint:dupl // It's not a duplicate.
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":       "true",
						"yawol.stackit.cloud/udpIdleTimeout": "3m",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-udp-port",
							Protocol: corev1.ProtocolUDP,
							Port:     53,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.Listeners).To(PointTo(ConsistOf(
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-udp-port")),
					"Udp": PointTo(MatchFields(IgnoreExtras, Fields{
						"IdleTimeout": PointTo(Equal("180s")),
					})),
				}),
			)))
		})

		It("should error on non-compatible timeouts", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":       "true",
						"lb.stackit.cloud/udp-idle-timeout":  "15m",
						"yawol.stackit.cloud/udpIdleTimeout": "3m",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-udp-port",
							Protocol: corev1.ProtocolUDP,
							Port:     80,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(HaveOccurred())
		})

		It("should error on invalid timeout format", func() {
			_, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":      "true",
						"lb.stackit.cloud/udp-idle-timeout": "15x",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-udp-port",
							Protocol: corev1.ProtocolUDP,
							Port:     80,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).To(HaveOccurred())
		})

		It("should use default timeout if yawol annotation is invalid", func() { //nolint:dupl // It's not a duplicate.
			spec, err := lbSpecFromService(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"lb.stackit.cloud/internal-lb":       "true",
						"yawol.stackit.cloud/udpIdleTimeout": "3x",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "my-udp-port",
							Protocol: corev1.ProtocolUDP,
							Port:     80,
						},
					},
				},
			}, []*corev1.Node{}, "my-network", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec.Listeners).To(PointTo(ConsistOf(
				MatchFields(IgnoreExtras, Fields{
					"DisplayName": PointTo(Equal("my-udp-port")),
					"Udp": PointTo(MatchFields(IgnoreExtras, Fields{
						"IdleTimeout": PointTo(Equal("120s")),
					})),
				}),
			)))
		})
	})

	It("should attach the load balancer to the specified network", func() {
		spec, err := lbSpecFromService(&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"lb.stackit.cloud/internal-lb": "true",
				},
			},
		}, []*corev1.Node{}, "my-network", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.Networks).To(PointTo(ConsistOf(MatchFields(IgnoreExtras, Fields{
			"NetworkId": PointTo(Equal("my-network")),
			"Role":      PointTo(Equal("ROLE_LISTENERS_AND_TARGETS")),
		}))))
	})

	It("should configure a public service without existing IP as ephemeral", func() {
		spec, err := lbSpecFromService(&corev1.Service{}, []*corev1.Node{}, "my-network", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(*spec.Options.EphemeralAddress).To(BeTrue())
	})
})

// haveTargets succeeds if actual is a target pool and the list of targets matches matcher.
func haveTargets(matcher types.GomegaMatcher) types.GomegaMatcher {
	return WithTransform(func(pool loadbalancer.TargetPool) *[]loadbalancer.Target { return pool.Targets }, PointTo(matcher))
}

// havePortName succeeds if actual is a listener whose display name matches name.
func havePortName(name string) types.GomegaMatcher {
	return WithTransform(func(l loadbalancer.Listener) *string { return l.DisplayName }, PointTo(Equal(name)))
}

// haveConsistentTargetPool succeeds if the target pools of each listener exist.
func haveConsistentTargetPool() types.GomegaMatcher {
	return WithTransform(func(l *loadbalancer.CreateLoadBalancerPayload) bool {
		for _, lb := range *l.Listeners {
			contains := slices.ContainsFunc(*l.TargetPools, func(t loadbalancer.TargetPool) bool {
				return ptr.Equal(lb.TargetPool, t.Name)
			})
			if !contains {
				return false
			}
		}
		return true
	}, BeTrue())
}

type compareLBwithSpecTest struct {
	wantFulfilled         bool
	wantImmutabledChanged *resultImmutableChanged
	lb                    *loadbalancer.LoadBalancer
	spec                  *loadbalancer.CreateLoadBalancerPayload
}

var _ = DescribeTable("compareLBwithSpec",
	func(t *compareLBwithSpecTest) {
		fulfills, immutableChanged := compareLBwithSpec(t.lb, t.spec)
		Expect(immutableChanged).To(Equal(t.wantImmutabledChanged))
		Expect(fulfills).To(Equal(t.wantFulfilled))
	},
	Entry("When LB has Observability set", &compareLBwithSpecTest{
		// The load balancer API uses the same field to report an ephemeral IP and to reference a static IP.
		wantFulfilled: true,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
				Observability: &loadbalancer.LoadbalancerOptionObservability{
					Logs: &loadbalancer.LoadbalancerOptionLogs{
						CredentialsRef: ptr.To("credentials-12345"),
						PushUrl:        ptr.To("https://logs.example.org"),
					},
					Metrics: &loadbalancer.LoadbalancerOptionMetrics{
						CredentialsRef: ptr.To("credentials-12345"),
						PushUrl:        ptr.To("https://metrics.example.org"),
					},
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
				Observability: &loadbalancer.LoadbalancerOptionObservability{
					Logs: &loadbalancer.LoadbalancerOptionLogs{
						CredentialsRef: ptr.To("credentials-12345"),
						PushUrl:        ptr.To("https://logs.example.org"),
					},
					Metrics: &loadbalancer.LoadbalancerOptionMetrics{
						CredentialsRef: ptr.To("credentials-12345"),
						PushUrl:        ptr.To("https://metrics.example.org"),
					},
				},
			},
		},
	}),
	Entry("When LB has different Observability set", &compareLBwithSpecTest{
		// The load balancer API uses the same field to report an ephemeral IP and to reference a static IP.
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
				Observability: &loadbalancer.LoadbalancerOptionObservability{
					Metrics: &loadbalancer.LoadbalancerOptionMetrics{
						CredentialsRef: ptr.To("credentials-12345"),
						PushUrl:        ptr.To("https://metrics.example.org"),
					},
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
		},
	}),
	Entry("When LB has an external address and the specification is ephemeral", &compareLBwithSpecTest{
		// The load balancer API uses the same field to report an ephemeral IP and to reference a static IP.
		wantFulfilled: true,
		lb: &loadbalancer.LoadBalancer{
			ExternalAddress: utils.Ptr("123.124.88.99"),
			Options: &loadbalancer.LoadBalancerOptions{
				EphemeralAddress: utils.Ptr(true),
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			ExternalAddress: nil,
			Options: &loadbalancer.LoadBalancerOptions{
				EphemeralAddress: utils.Ptr(true),
			},
		},
	}),
	Entry("When LB has no external IP but one is specified", &compareLBwithSpecTest{
		wantImmutabledChanged: &resultImmutableChanged{field: ".externalAddress"},
		lb: &loadbalancer.LoadBalancer{
			ExternalAddress: nil,
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			ExternalAddress: utils.Ptr("123.124.88.99"),
		},
	}),
	Entry("When specified and actual IP don't match", &compareLBwithSpecTest{
		// The IP can never be changed. Not even with promotion or demotion.
		wantImmutabledChanged: &resultImmutableChanged{field: ".externalAddress"},
		lb: &loadbalancer.LoadBalancer{
			ExternalAddress: utils.Ptr("123.124.88.01"),
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			ExternalAddress: utils.Ptr("123.124.88.99"),
		},
	}),
	Entry("When IP is to be promoted", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			ExternalAddress: utils.Ptr("123.124.88.99"),
			Options: &loadbalancer.LoadBalancerOptions{
				EphemeralAddress: utils.Ptr(true),
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			ExternalAddress: utils.Ptr("123.124.88.99"),
			Options: &loadbalancer.LoadBalancerOptions{
				EphemeralAddress: utils.Ptr(false),
			},
		},
	}),
	Entry("When IP is to be demoted", &compareLBwithSpecTest{
		wantImmutabledChanged: &resultImmutableChanged{field: ".options.ephemeralAddress"},
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				EphemeralAddress: utils.Ptr(false),
			},
			ExternalAddress: utils.Ptr("123.124.88.99"),
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				EphemeralAddress: utils.Ptr(true),
			},
		},
	}),
	Entry("When number of listeners doesn't match", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{}, {},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{},
			},
		},
	}),
	Entry("When listener name doesn't match", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{DisplayName: utils.Ptr("port-a")},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{DisplayName: utils.Ptr("port-b")},
			},
		},
	}),
	Entry("When port name doesn't match", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{Port: utils.Ptr[int64](80)},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{Port: utils.Ptr[int64](443)},
			},
		},
	}),
	Entry("When protocol name doesn't match", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{Protocol: utils.Ptr(lbapi.ProtocolTCP)},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{Protocol: utils.Ptr(lbapi.ProtocolTCPProxy)},
			},
		},
	}),
	Entry("When TCP idle timeout doesn't match", &compareLBwithSpecTest{ //nolint:dupl // It's not a duplicate.
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{
					Protocol: utils.Ptr(lbapi.ProtocolTCP),
					Tcp: &loadbalancer.OptionsTCP{
						IdleTimeout: utils.Ptr("60s"),
					},
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{
					Protocol: utils.Ptr(lbapi.ProtocolTCP),
					Tcp: &loadbalancer.OptionsTCP{
						IdleTimeout: utils.Ptr("120s"),
					},
				},
			},
		},
	}),
	Entry("When UDP idle timeout doesn't match", &compareLBwithSpecTest{ //nolint:dupl // It's not a duplicate.
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{
					Protocol: utils.Ptr(lbapi.ProtocolUDP),
					Udp: &loadbalancer.OptionsUDP{
						IdleTimeout: utils.Ptr("60s"),
					},
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{
					Protocol: utils.Ptr(lbapi.ProtocolUDP),
					Udp: &loadbalancer.OptionsUDP{
						IdleTimeout: utils.Ptr("120s"),
					},
				},
			},
		},
	}),
	Entry("When TCP proxy timeout doesn't match", &compareLBwithSpecTest{ //nolint:dupl // It's not a duplicate.
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{
					Protocol: utils.Ptr(lbapi.ProtocolTCPProxy),
					Tcp: &loadbalancer.OptionsTCP{
						IdleTimeout: utils.Ptr("60s"),
					},
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{
					Protocol: utils.Ptr(lbapi.ProtocolTCPProxy),
					Tcp: &loadbalancer.OptionsTCP{
						IdleTimeout: utils.Ptr("120s"),
					},
				},
			},
		},
	}),
	Entry("When target pool name doesn't match", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{TargetPool: utils.Ptr("target-pool-a")},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Listeners: &[]loadbalancer.Listener{
				{TargetPool: utils.Ptr("target-pool-b")},
			},
		},
	}),
	Entry("When LB has no networks but one is specified", &compareLBwithSpecTest{
		wantImmutabledChanged: &resultImmutableChanged{field: "len(.networks)"},
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Networks: nil,
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Networks: &[]loadbalancer.Network{
				{},
			},
		},
	}),
	Entry("When network id doesn't match", &compareLBwithSpecTest{
		wantImmutabledChanged: &resultImmutableChanged{field: ".networks[0].networkId"},
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Networks: &[]loadbalancer.Network{
				{
					NetworkId: utils.Ptr("my-network"),
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Networks: &[]loadbalancer.Network{
				{
					NetworkId: utils.Ptr("other-network"),
				},
			},
		},
	}),
	Entry("When network role doesn't match", &compareLBwithSpecTest{
		wantImmutabledChanged: &resultImmutableChanged{field: ".networks[0].role"},
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Networks: &[]loadbalancer.Network{
				{
					Role: utils.Ptr("listeners"),
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			Networks: &[]loadbalancer.Network{
				{
					Role: utils.Ptr("targets"),
				},
			},
		},
	}),
	Entry("When number of target pools don't match", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{}, {},
			},
		},
	}),
	Entry("When target pool name doesn't match", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					Name: utils.Ptr("target-pool-a"),
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					Name: utils.Ptr("target-pool-b"),
				},
			},
		},
	}),
	Entry("When target pool port doesn't match", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					TargetPort: utils.Ptr[int64](80),
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					TargetPort: utils.Ptr[int64](443),
				},
			},
		},
	}),
	Entry("When target order does not match", &compareLBwithSpecTest{
		wantFulfilled: true,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					Targets: &[]loadbalancer.Target{
						{
							DisplayName: utils.Ptr("node-a"),
							Ip:          utils.Ptr("10.0.0.1"),
						},
						{
							DisplayName: utils.Ptr("node-b"),
							Ip:          utils.Ptr("10.0.0.2"),
						},
					},
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					Targets: &[]loadbalancer.Target{
						{
							DisplayName: utils.Ptr("node-b"),
							Ip:          utils.Ptr("10.0.0.2"),
						},
						{
							DisplayName: utils.Ptr("node-a"),
							Ip:          utils.Ptr("10.0.0.1"),
						},
					},
				},
			},
		},
	}),
	Entry("When target node-b is added", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					Targets: &[]loadbalancer.Target{
						{
							DisplayName: utils.Ptr("node-a"),
							Ip:          utils.Ptr("10.0.0.1"),
						},
					},
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					Targets: &[]loadbalancer.Target{
						{
							DisplayName: utils.Ptr("node-b"),
							Ip:          utils.Ptr("10.0.0.2"),
						},
						{
							DisplayName: utils.Ptr("node-a"),
							Ip:          utils.Ptr("10.0.0.1"),
						},
					},
				},
			},
		},
	}),
	Entry("When target IP changes", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					Targets: &[]loadbalancer.Target{
						{
							DisplayName: utils.Ptr("node-a"),
							Ip:          utils.Ptr("10.0.0.1"),
						},
					},
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					Targets: &[]loadbalancer.Target{
						{
							DisplayName: utils.Ptr("node-a"),
							Ip:          utils.Ptr("10.0.0.2"),
						},
					},
				},
			},
		},
	}),
	Entry("When targets in spec are empty and targets in lb is nil", &compareLBwithSpecTest{
		wantFulfilled: true,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					Targets: nil,
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					Targets: &[]loadbalancer.Target{},
				},
			},
		},
	}),
	Entry("When health check interval doesn't match", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					ActiveHealthCheck: &loadbalancer.ActiveHealthCheck{
						Interval: utils.Ptr("2"),
					},
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					ActiveHealthCheck: &loadbalancer.ActiveHealthCheck{
						Interval: utils.Ptr("3"),
					},
				},
			},
		},
	}),
	Entry("When unhealthy threshold is unset but specified", &compareLBwithSpecTest{
		wantFulfilled: false,
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					ActiveHealthCheck: &loadbalancer.ActiveHealthCheck{
						UnhealthyThreshold: nil,
					},
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
			TargetPools: &[]loadbalancer.TargetPool{
				{
					ActiveHealthCheck: &loadbalancer.ActiveHealthCheck{
						UnhealthyThreshold: utils.Ptr[int64](3),
					},
				},
			},
		},
	}),
	Entry("When private network is disabled but specified", &compareLBwithSpecTest{
		wantImmutabledChanged: &resultImmutableChanged{field: ".options.privateNetworkOnly"},
		lb: &loadbalancer.LoadBalancer{
			Options: nil,
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
		},
	}),
	Entry("When private IP is reported back from API", &compareLBwithSpecTest{
		wantFulfilled: true,
		lb: &loadbalancer.LoadBalancer{
			PrivateAddress: utils.Ptr("10.1.1.3"),
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
		},
	}),
	Entry("When source ranges are set but not specified", &compareLBwithSpecTest{
		wantImmutabledChanged: &resultImmutableChanged{field: ".options.accessControl"},
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
				AccessControl: &loadbalancer.LoadbalancerOptionAccessControl{
					AllowedSourceRanges: utils.Ptr([]string{"10.0.0.0/24"}),
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
			},
		},
	}),
	Entry("When source ranges don't match", &compareLBwithSpecTest{
		wantImmutabledChanged: &resultImmutableChanged{field: ".options.accessControl"},
		lb: &loadbalancer.LoadBalancer{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
				AccessControl: &loadbalancer.LoadbalancerOptionAccessControl{
					AllowedSourceRanges: utils.Ptr([]string{"10.5.0.0/24"}),
				},
			},
		},
		spec: &loadbalancer.CreateLoadBalancerPayload{
			Options: &loadbalancer.LoadBalancerOptions{
				PrivateNetworkOnly: utils.Ptr(true),
				AccessControl: &loadbalancer.LoadbalancerOptionAccessControl{
					AllowedSourceRanges: utils.Ptr([]string{"10.0.0.0/24"}),
				},
			},
		},
	}),
)
