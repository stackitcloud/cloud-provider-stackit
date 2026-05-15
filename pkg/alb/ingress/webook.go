package ingress

import (
	"fmt"
	"context"
	"net"
	"net/http"
	"strconv"

	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type IngressValidator struct {
    Client  client.Client
    Decoder admission.Decoder
}

func (v *IngressValidator) InjectDecoder(d admission.Decoder) error {
	v.Decoder = d
	return nil
}

func (v *IngressValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	ingress := &networkingv1.Ingress{}
	if err := v.Decoder.Decode(req, ingress); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if ingress.Spec.IngressClassName == nil {
		return admission.Allowed("No ingress class specified; ignoring.")
	}

	ingressClass := &networkingv1.IngressClass{}
	if err := v.Client.Get(ctx, client.ObjectKey{Name: *ingress.Spec.IngressClassName}, ingressClass); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	if ingressClass.Spec.Controller != controllerName {
		return admission.Allowed("Ingress managed by a different controller; allowing.")
	}

	// 1. Network Mode Check.
	mode, exists := ingress.Annotations[AnnotationNetworkMode]
	if !exists {
		return admission.Denied("The annotation '" + AnnotationNetworkMode + "' is mandatory for STACKIT ALB Ingresses.")
	}
	if mode != "NodePort" {
		return admission.Denied(fmt.Sprintf("The annotation '%s' currently only supports the value 'NodePort'.", AnnotationNetworkMode))
	}

	// 2. Validate IP Addresses.
	if val, ok := ingress.Annotations[AnnotationExternalIP]; ok {
		if net.ParseIP(val) == nil {
			return admission.Denied(fmt.Sprintf("Annotation '%s' must be a valid IP address.", AnnotationExternalIP))
		}
	}

	// 3. Validate Booleans.
	boolAnnotations := []string{
		AnnotationInternal,
		AnnotationTargetPoolTLSEnabled,
		AnnotationTargetPoolTLSSkipCertificateValidation,
		AnnotationHTTPSOnly,
		AnnotationWebSocket,
	}
	for _, ann := range boolAnnotations {
		if val, ok := ingress.Annotations[ann]; ok {
			if _, err := strconv.ParseBool(val); err != nil {
				return admission.Denied(fmt.Sprintf("Annotation '%s' must be a valid boolean (true or false).", ann))
			}
		}
	}

	// 4. Validate Ports (Must be between 1 and 65535).
	portAnnotations := []string{AnnotationHTTPPort, AnnotationHTTPSPort}
	for _, ann := range portAnnotations {
		if val, ok := ingress.Annotations[ann]; ok {
			port, err := strconv.Atoi(val)
			if err != nil || port < 1 || port > 65535 {
				return admission.Denied(fmt.Sprintf("Annotation '%s' must be a valid port number between 1 and 65535.", ann))
			}
		}
	}

	// 5. Validate TTL and Priority (Must be valid integers. TTL must be non-negative).
	intAnnotations := []string{AnnotationCookiePersistenceTTLSeconds, AnnotationPriority}
	for _, ann := range intAnnotations {
		if val, ok := ingress.Annotations[ann]; ok {
			num, err := strconv.Atoi(val)
			if err != nil {
				return admission.Denied(fmt.Sprintf("Annotation '%s' must be a valid integer.", ann))
			}
			// Optional: Enforce TTL to be non-negative
			if ann == AnnotationCookiePersistenceTTLSeconds && num < 0 {
				return admission.Denied(fmt.Sprintf("Annotation '%s' must be greater than or equal to 0.", ann))
			}
		}
	}

	return admission.Allowed("Validation passed.")
}