package spec

import (
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// AnnotationExternalIP references a STACKIT public IP that should be used by the application load balancer.
	// If set it will be used instead of an ephemeral IP. The IP must be created by the customer. When the service is deleted,
	// the public IP will not be deleted. The IP is ignored if the alb.stackit.cloud/internal-alb is set.
	// If the annotation is set after the creation it must match the ephemeral IP.
	// This will promote the ephemeral IP to a static IP.
	// Can be set on IngressClass.
	AnnotationExternalIP = "alb.stackit.cloud/external-address"
	// AnnotationInternal If true, the application load balancer is not exposed via a public IP.
	// Can be set on IngressClass.
	AnnotationInternal = "alb.stackit.cloud/internal-alb"
	// AnnotationPlanID sets the plan for the ALB.
	// Can be set on IngressClass.
	AnnotationPlanID = "alb.stackit.cloud/plan-id"

	// AnnotationTargetPoolTLSEnabled If true, the application load balancer enables TLS bridging.
	// It uses the trusted CAs from the operating system for validation.
	// Can be set on IngressClass, Ingress and Service.
	AnnotationTargetPoolTLSEnabled = "alb.stackit.cloud/target-pool-tls-enabled"
	// AnnotationTargetPoolTLSCustomCa If set, the application load balancer enables TLS bridging with a custom CA provided as value. Is this an inlined field? What is its format? Can this be set to an empty string to reset it?
	// Can be set on IngressClass, Ingress and Service
	AnnotationTargetPoolTLSCustomCa = "alb.stackit.cloud/target-pool-tls-custom-ca"
	// AnnotationTargetPoolTLSSkipCertificateValidation If true, the application load balancer enables TLS bridging but skips validation.
	// Can be set on IngressClass, Ingress and Service.
	AnnotationTargetPoolTLSSkipCertificateValidation = "alb.stackit.cloud/target-pool-tls-skip-certificate-validation"

	// AnnotationHTTPPort Specifies the HTTP port.
	// Can be set on IngressClass and Ingress.
	AnnotationHTTPPort = "alb.stackit.cloud/http-port"
	// AnnotationHTTPSPort Specifies the HTTPS port.
	// Can be set on IngressClass and Ingress.
	AnnotationHTTPSPort = "alb.stackit.cloud/https-port"
	// AnnotationHTTPSOnly if true, the ingress will not be reachable via HTTP. Does that mean all ingresses are always available via HTTPS?
	// Can be set on IngressClass and Ingress.
	AnnotationHTTPSOnly = "alb.stackit.cloud/https-only"

	// AnnotationWebSocket TODO
	// Can be set on IngressClass and Ingress.
	AnnotationWebSocket = "alb.stackit.cloud/websocket"

	// AnnotationWAFName TODO
	// Can be set on IngressClass and Ingress.
	AnnotationWAFName = "alb.stackit.cloud/web-application-firewall-name"

	// AnnotationPriority is used to set the priority of the Ingress. Can be only set on ingress objects.
	// Can be set on IngressClass and Ingress.
	AnnotationPriority = "alb.stackit.cloud/priority"
)

// GetAnnotation retrieves an annotation value from objects.
// If multiple objects contain the annotation, the later object in the slice takes precedence.
// If no object contains the annotation then defaultValue is returned.
//
// GetAnnotation parses the value of the annotation and return type T.
// If T is string then the value is returned raw.
// For int and bool Atoi and ParseBool are called respectively.
// If parsing fails or T is any other type, defaultValue is returned.
// Only the latest found value is parsed.
//
// TODO: Return parser errors?!
// TODO: Allow unsetting a value by setting the annotation to an empty string?!
func GetAnnotation[T any](annotation string, defaultValue T, objects ...client.Object) T {
	var rawVal string
	var found bool

	// Iterate through sources (e.g., Ingress, then IngressClass)
	for _, object := range objects {
		if val, exists := object.GetAnnotations()[annotation]; exists {
			rawVal = val
			found = true
			break
		}
	}

	if !found {
		return defaultValue
	}

	var result any
	var err error

	switch any(defaultValue).(type) {
	case string:
		return any(rawVal).(T)
	case int:
		result, err = strconv.Atoi(rawVal)
	case bool:
		result, err = strconv.ParseBool(rawVal)
	default:
		return defaultValue
	}

	if err != nil {
		return defaultValue
	}

	return result.(T)
}
