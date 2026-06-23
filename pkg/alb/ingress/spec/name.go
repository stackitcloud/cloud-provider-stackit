package spec

import (
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
)

// LoadBalancerName returns the desired name for a load balancer.
// The ingress class must have a UID.
func LoadBalancerName(ingressClass *networkingv1.IngressClass) string {
	return fmt.Sprintf("k8s-ingress-%s", ingressClass.UID)
}
