package ingress

import (
	networkingv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Ingress constructs an ingress for testing purposes.
func Ingress(namespace, name string, opts ...IngressOption) networkingv1.Ingress {
	i := networkingv1.Ingress{
		ObjectMeta: v1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Annotations: map[string]string{},
		},
	}
	for _, o := range opts {
		o.applyToIngress(&i)
	}
	return i
}

type IngressOption interface {
	applyToIngress(ingress *networkingv1.Ingress)
}

type ingressOptionFunc func(ingress *networkingv1.Ingress)

func (f ingressOptionFunc) applyToIngress(ingress *networkingv1.Ingress) {
	f(ingress)
}

func WithUID(uid string) IngressOption {
	return ingressOptionFunc(func(ingress *networkingv1.Ingress) {
		ingress.UID = types.UID(uid)
	})
}

func WithIngressClass(ingressClass string) IngressOption {
	return ingressOptionFunc(func(ingress *networkingv1.Ingress) {
		ingress.Spec.IngressClassName = new(ingressClass)
	})
}

func WithAnnotation(key, value string) IngressOption {
	return ingressOptionFunc(func(ingress *networkingv1.Ingress) {
		ingress.Annotations[key] = value
	})
}

func WithTLSSecret(secretName string) ingressOptionFunc {
	return ingressOptionFunc(func(ingress *networkingv1.Ingress) {
		ingress.Spec.TLS = append(ingress.Spec.TLS, networkingv1.IngressTLS{
			SecretName: secretName,
		})
	})
}

func WithRule(host string, opts ...RuleOptions) IngressOption {
	return ingressOptionFunc(func(ingress *networkingv1.Ingress) {
		rule := networkingv1.IngressRule{
			Host: host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{},
			},
		}
		for _, o := range opts {
			o.applyToRule(&rule)
		}
		ingress.Spec.Rules = append(ingress.Spec.Rules, rule)
	})
}

type RuleOptions interface {
	applyToRule(rule *networkingv1.IngressRule)
}

type ruleOptionsFunc func(rule *networkingv1.IngressRule)

func (f ruleOptionsFunc) applyToRule(rule *networkingv1.IngressRule) {
	f(rule)
}

func WithPath(path string, _type *networkingv1.PathType, serviceName string, serviceBackendPort networkingv1.ServiceBackendPort) RuleOptions {
	return ruleOptionsFunc(func(rule *networkingv1.IngressRule) {
		if rule.HTTP.Paths == nil {
			rule.HTTP.Paths = []networkingv1.HTTPIngressPath{}
		}
		rule.HTTP.Paths = append(rule.HTTP.Paths, networkingv1.HTTPIngressPath{
			PathType: _type,
			Path:     path,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: serviceName,
					Port: serviceBackendPort,
				},
			},
		})
	})
}
