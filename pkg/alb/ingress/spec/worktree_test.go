package spec

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	. "github.com/stackitcloud/cloud-provider-stackit/pkg/alb/ingress/testutil/ingress"
	. "github.com/stackitcloud/cloud-provider-stackit/pkg/alb/ingress/testutil/service"
	"github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type testCase struct {
	class       *networkingv1.IngressClass
	ingresses   []networkingv1.Ingress
	secrets     []corev1.Secret
	services    []corev1.Service
	nodes       []corev1.Node
	existingALB *v2api.LoadBalancer

	matcher types.GomegaMatcher
}

var _ = Describe("WorkTreeALB", func() {
	It("should sort rules from most to least-specific even if their priority is inversed", func() {
		tree, errs := BuildTree(&networkingv1.IngressClass{}, []networkingv1.Ingress{
			Ingress(
				"default", "ingress-with-higher-priority",
				WithAnnotation(AnnotationPriority, "5"),
				WithRule("my-host.local",
					WithPath("/prefix/b", new(networkingv1.PathTypePrefix), "my-service", networkingv1.ServiceBackendPort{Number: 1337}),
					WithPath("/exact/b", new(networkingv1.PathTypeExact), "my-service", networkingv1.ServiceBackendPort{Number: 1337}),
					WithPath("/exact/b/b", new(networkingv1.PathTypeExact), "my-service", networkingv1.ServiceBackendPort{Number: 1337}),
				),
			),
			Ingress(
				"default", "ingress-with-lower-priority",
				WithAnnotation(AnnotationPriority, "4"),
				WithRule("my-host.local",
					WithPath("/prefix/a", new(networkingv1.PathTypePrefix), "my-service", networkingv1.ServiceBackendPort{Number: 1337}),
					WithPath("/exact/a", new(networkingv1.PathTypeExact), "my-service", networkingv1.ServiceBackendPort{Number: 1337}),
					WithPath("/exact/a/a", new(networkingv1.PathTypeExact), "my-service", networkingv1.ServiceBackendPort{Number: 1337}),
				),
			),
		}, nil, []corev1.Service{
			Service(corev1.NamespaceDefault, "my-service", WithServiceType(corev1.ServiceTypeNodePort), WithPort("my-port", 1337, 30000, corev1.ProtocolTCP)),
		}, nil, nil)
		Expect(errs).To(HaveLen(0))
		createPayload := tree.ToCreatePayload(nil, "", "")
		Expect(createPayload.Listeners[0].Http.Hosts[0].Host).To(HaveValue(Equal("my-host.local")))
		Expect(createPayload.Listeners[0].Http.Hosts[0].Rules).To(HaveLen(6))
		Expect(createPayload.Listeners[0].Http.Hosts[0].Rules[0].Path.ExactMatch).To(HaveValue(Equal("/exact/a/a")))
		Expect(createPayload.Listeners[0].Http.Hosts[0].Rules[1].Path.ExactMatch).To(HaveValue(Equal("/exact/b/b")))
		Expect(createPayload.Listeners[0].Http.Hosts[0].Rules[2].Path.ExactMatch).To(HaveValue(Equal("/exact/a")))
		Expect(createPayload.Listeners[0].Http.Hosts[0].Rules[3].Path.ExactMatch).To(HaveValue(Equal("/exact/b")))
		Expect(createPayload.Listeners[0].Http.Hosts[0].Rules[4].Path.Prefix).To(HaveValue(Equal("/prefix/a")))
		Expect(createPayload.Listeners[0].Http.Hosts[0].Rules[5].Path.Prefix).To(HaveValue(Equal("/prefix/b")))
	})

	It("should match rules against correct node ports", func() {
		const host = "my-host.local"
		tree, _ := BuildTree(&networkingv1.IngressClass{}, []networkingv1.Ingress{
			Ingress(
				"default", "ingress-to-node-port-5000",
				WithRule(host, WithPath("/5000", new(networkingv1.PathTypeExact), "service-a", networkingv1.ServiceBackendPort{Number: 1337})),
			),
			Ingress(
				"default", "ingress-to-node-port-5001",
				WithRule(host, WithPath("/5001", new(networkingv1.PathTypeExact), "service-a", networkingv1.ServiceBackendPort{Name: "1338"})),
			),
			Ingress(
				"default", "ingress-to-node-port-5002",
				WithRule(host, WithPath("/5002", new(networkingv1.PathTypeExact), "service-a", networkingv1.ServiceBackendPort{Number: 1339})),
			),
			Ingress(
				"default", "ingress-to-node-port-5003",
				WithRule(host, WithPath("/5003", new(networkingv1.PathTypeExact), "service-b", networkingv1.ServiceBackendPort{Number: 1337})),
			),
		}, nil, []corev1.Service{
			Service("default", "service-a",
				WithPort("1337", 1337, 5000, corev1.ProtocolTCP),
				WithPort("1338", 1338, 5001, corev1.ProtocolTCP),
				WithPort("1339", 1339, 5002, corev1.ProtocolTCP),
			),
			Service("default", "service-b",
				WithPort("1337", 1337, 5003, corev1.ProtocolTCP),
			),
		}, nil, nil)
		createPayload := tree.ToCreatePayload(nil, "", "")

		Expect(createPayload.Listeners[0].Http.Hosts[0].Host).To(HaveValue(Equal(host)))

		// The following assertions require that target pool are sorted by target ports.
		Expect(createPayload.Listeners[0].Http.Hosts[0].Rules[0].Path.ExactMatch).To(HaveValue(Equal("/5000")))
		Expect(createPayload.TargetPools[0].Name).To(Equal(createPayload.Listeners[0].Http.Hosts[0].Rules[0].TargetPool))
		Expect(createPayload.TargetPools[0].TargetPort).To(HaveValue(Equal(int32(5000))))

		Expect(createPayload.Listeners[0].Http.Hosts[0].Rules[1].Path.ExactMatch).To(HaveValue(Equal("/5001")))
		Expect(createPayload.TargetPools[1].Name).To(Equal(createPayload.Listeners[0].Http.Hosts[0].Rules[1].TargetPool))
		Expect(createPayload.TargetPools[1].TargetPort).To(HaveValue(Equal(int32(5001))))

		Expect(createPayload.Listeners[0].Http.Hosts[0].Rules[2].Path.ExactMatch).To(HaveValue(Equal("/5002")))
		Expect(createPayload.TargetPools[2].Name).To(Equal(createPayload.Listeners[0].Http.Hosts[0].Rules[2].TargetPool))
		Expect(createPayload.TargetPools[2].TargetPort).To(HaveValue(Equal(int32(5002))))

		Expect(createPayload.Listeners[0].Http.Hosts[0].Rules[3].Path.ExactMatch).To(HaveValue(Equal("/5003")))
		Expect(createPayload.TargetPools[3].Name).To(Equal(createPayload.Listeners[0].Http.Hosts[0].Rules[3].TargetPool))
		Expect(createPayload.TargetPools[3].TargetPort).To(HaveValue(Equal(int32(5003))))
	})

	It("should return an error when the TLS secret doesn't exist", func() {
		_, errs := BuildTree(
			&networkingv1.IngressClass{},
			[]networkingv1.Ingress{
				Ingress(corev1.NamespaceDefault, "ingress-with-tls-secret-reference", WithTLSSecret("doesnt-exist")),
			},
			nil, nil, nil, nil,
		)

		Expect(errs).To(HaveLen(1))
		Expect(errs[0].description).To(Equal("TLS secret doesn't exist"))
	})

	It("should return an error when the TLS secret isn't of type TLS", func() {
		_, errs := BuildTree(
			&networkingv1.IngressClass{},
			[]networkingv1.Ingress{
				Ingress(corev1.NamespaceDefault, "ingress-with-tls-secret-reference", WithTLSSecret("non-tls")),
			},
			[]corev1.Secret{
				{
					ObjectMeta: v1.ObjectMeta{Namespace: corev1.NamespaceDefault, Name: "non-tls"},
					Type:       corev1.SecretTypeDockerConfigJson, // Not TLS
				},
			}, nil, nil, nil,
		)

		Expect(errs).To(HaveLen(1))
		Expect(errs[0].description).To(Equal("TLS secret isn't of type kubernetes.io/tls"))
	})

	It("should return an error when the TLS secret isn't of type TLS", func() {
		_, errs := BuildTree(
			&networkingv1.IngressClass{},
			[]networkingv1.Ingress{
				Ingress(corev1.NamespaceDefault, "ingress-with-tls-secret-reference", WithTLSSecret("non-tls")),
			},
			[]corev1.Secret{
				{
					ObjectMeta: v1.ObjectMeta{Namespace: corev1.NamespaceDefault, Name: "non-tls"},
					Type:       corev1.SecretTypeDockerConfigJson, // Not TLS
				},
			}, nil, nil, nil,
		)

		Expect(errs).To(HaveLen(1))
		Expect(errs[0].description).To(Equal("TLS secret isn't of type kubernetes.io/tls"))
	})

	It("should return an error when TLS secret parsing fails", func() {
		_, errs := BuildTree(
			&networkingv1.IngressClass{},
			[]networkingv1.Ingress{
				Ingress(corev1.NamespaceDefault, "ingress-with-tls-secret-reference", WithTLSSecret("invalid-tls")),
			},
			[]corev1.Secret{
				{
					ObjectMeta: v1.ObjectMeta{Namespace: corev1.NamespaceDefault, Name: "invalid-tls"},
					Type:       corev1.SecretTypeTLS,
					Data: map[string][]byte{
						corev1.TLSCertKey:       []byte("invalid cert"),
						corev1.TLSPrivateKeyKey: []byte(fixtureTLSPrivateKey),
					},
				},
			}, nil, nil, nil,
		)

		Expect(errs).To(HaveLen(1))
		Expect(errs[0].description).To(Equal("invalid certificate: tls: failed to find any PEM data in certificate input"))
	})

	It("should process TLS secret correctly", func() {
		tree, errs := BuildTree(
			&networkingv1.IngressClass{},
			[]networkingv1.Ingress{
				Ingress(corev1.NamespaceDefault, "ingress-with-tls-secret-reference", WithTLSSecret("my-tls")),
			},
			[]corev1.Secret{
				{
					ObjectMeta: v1.ObjectMeta{Namespace: corev1.NamespaceDefault, Name: "my-tls"},
					Type:       corev1.SecretTypeTLS,
					Data: map[string][]byte{
						corev1.TLSCertKey:       []byte(fixtureTLSPublicKey),
						corev1.TLSPrivateKeyKey: []byte(fixtureTLSPrivateKey),
					},
				},
			}, nil, nil, nil,
		)

		Expect(errs).To(HaveLen(0))
		Expect(tree.GetMissingCertificates(nil)).To(ConsistOf(
			WorkTreeCertificate{
				PublicKey:  fixtureTLSPublicKey,
				PrivateKey: fixtureTLSPrivateKey,
			},
		))
	})

	It("should enable websocket if enable on ingress class", func() {
		tree, errs := BuildTree(
			&networkingv1.IngressClass{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationWebSocket: "true",
					},
				},
			},
			[]networkingv1.Ingress{
				Ingress(corev1.NamespaceDefault, "ingress-1", WithRule("my-host.local",
					WithPath("/a", new(networkingv1.PathTypePrefix), "my-service", networkingv1.ServiceBackendPort{Number: 80}),
				)),
				Ingress(corev1.NamespaceDefault, "ingress-1", WithAnnotation(AnnotationWebSocket, "false"), WithRule("my-host.local",
					WithPath("/b", new(networkingv1.PathTypePrefix), "my-service", networkingv1.ServiceBackendPort{Number: 80}),
				)),
			},
			nil,
			[]corev1.Service{
				Service(corev1.NamespaceDefault, "my-service", WithServiceType(corev1.ServiceTypeNodePort), WithPort("my-port", 80, 30000, corev1.ProtocolTCP)),
			}, nil, nil,
		)

		Expect(errs).To(HaveLen(0))
		create := tree.ToCreatePayload(nil, "network-id", "region")
		Expect(create.Listeners).To(HaveLen(1))
		Expect(create.Listeners[0].Http.Hosts).To(HaveLen(1))
		Expect(create.Listeners[0].Http.Hosts[0].Rules).To(HaveLen(2))
		Expect(create.Listeners[0].Http.Hosts[0].Rules[0].Path.Prefix).To(HaveValue(Equal("/a")))
		Expect(create.Listeners[0].Http.Hosts[0].Rules[0].WebSocket).To(HaveValue(BeTrue()))
		Expect(create.Listeners[0].Http.Hosts[0].Rules[1].Path.Prefix).To(HaveValue(Equal("/b")))
		Expect(create.Listeners[0].Http.Hosts[0].Rules[1].WebSocket).To(Or(BeNil(), HaveValue(BeFalse())))
	})

	It("should enable websocket if enable on ingress", func() {
		tree, errs := BuildTree(
			&networkingv1.IngressClass{},
			[]networkingv1.Ingress{
				Ingress(corev1.NamespaceDefault, "ingress-1", WithRule("my-host.local",
					WithPath("/a", new(networkingv1.PathTypePrefix), "my-service", networkingv1.ServiceBackendPort{Number: 80}),
				)),
				Ingress(corev1.NamespaceDefault, "ingress-1", WithAnnotation(AnnotationWebSocket, "true"), WithRule("my-host.local",
					WithPath("/b", new(networkingv1.PathTypePrefix), "my-service", networkingv1.ServiceBackendPort{Number: 80}),
				)),
			},
			nil,
			[]corev1.Service{
				Service(corev1.NamespaceDefault, "my-service", WithServiceType(corev1.ServiceTypeNodePort), WithPort("my-port", 80, 30000, corev1.ProtocolTCP)),
			}, nil, nil,
		)

		Expect(errs).To(HaveLen(0))
		create := tree.ToCreatePayload(nil, "network-id", "region")
		Expect(create.Listeners).To(HaveLen(1))
		Expect(create.Listeners[0].Http.Hosts).To(HaveLen(1))
		Expect(create.Listeners[0].Http.Hosts[0].Rules).To(HaveLen(2))
		Expect(create.Listeners[0].Http.Hosts[0].Rules[0].Path.Prefix).To(HaveValue(Equal("/a")))
		Expect(create.Listeners[0].Http.Hosts[0].Rules[0].WebSocket).To(HaveValue(Or(BeNil(), HaveValue(BeFalse()))))
		Expect(create.Listeners[0].Http.Hosts[0].Rules[1].Path.Prefix).To(HaveValue(Equal("/b")))
		Expect(create.Listeners[0].Http.Hosts[0].Rules[1].WebSocket).To(HaveValue(BeTrue()))
	})

	It("should set WAF on all ports if specified on ingress class", func() {
		tree, errs := BuildTree(
			&networkingv1.IngressClass{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationWAFName: "my-waf",
					},
				},
			},
			[]networkingv1.Ingress{
				Ingress(corev1.NamespaceDefault, "ingress-1", WithRule("my-host.local",
					WithPath("/", new(networkingv1.PathTypePrefix), "my-service", networkingv1.ServiceBackendPort{Number: 80}),
				)),
				Ingress(corev1.NamespaceDefault, "ingress-1", WithAnnotation(AnnotationHTTPPort, "8080"), WithRule("my-host.local",
					WithPath("/", new(networkingv1.PathTypePrefix), "my-service", networkingv1.ServiceBackendPort{Number: 80}),
				)),
			},
			nil,
			[]corev1.Service{
				Service(corev1.NamespaceDefault, "my-service", WithServiceType(corev1.ServiceTypeNodePort), WithPort("my-port", 80, 30000, corev1.ProtocolTCP)),
			}, nil, nil,
		)

		Expect(errs).To(HaveLen(0))
		create := tree.ToCreatePayload(nil, "network-id", "region")
		Expect(create.Listeners).To(HaveLen(2))
		Expect(create.Listeners[0].WafConfigName).To(HaveValue(Equal("my-waf")))
		Expect(create.Listeners[1].WafConfigName).To(HaveValue(Equal("my-waf")))
	})
})

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
