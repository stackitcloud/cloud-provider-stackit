package ingress

import (
	"fmt"
	"context"
	"net/http"
	"strconv"
	"regexp"

	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	admissionv1 "k8s.io/api/admission/v1"
)

type IngressValidator struct {
    Client  client.Client
    Decoder admission.Decoder
}

// Handle routes the request based on the operation type.
func (v *IngressValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	switch req.Operation {
	case admissionv1.Create:
		return v.handleCreate(ctx, req)
	case admissionv1.Update:
		return v.handleUpdate(ctx, req)
	default:
		return admission.Allowed("Unhandled operation allowed.")
	}
}

func (v *IngressValidator) handleCreate(ctx context.Context, req admission.Request) admission.Response {
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

	return v.validateBaseAnnotations(ctx, ingress)
}

func (v *IngressValidator) handleUpdate(ctx context.Context, req admission.Request) admission.Response {
	newIngress := &networkingv1.Ingress{}
	if err := v.Decoder.Decode(req, newIngress); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	oldIngress := &networkingv1.Ingress{}
	if err := v.Decoder.DecodeRaw(req.OldObject, oldIngress); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if newIngress.Spec.IngressClassName == nil {
		return admission.Allowed("No ingress class specified; ignoring.")
	}

	ingressClass := &networkingv1.IngressClass{}
	if err := v.Client.Get(ctx, client.ObjectKey{Name: *newIngress.Spec.IngressClassName}, ingressClass); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	if ingressClass.Spec.Controller != controllerName {
		return admission.Allowed("Ingress managed by a different controller; allowing.")
	}

	return v.validateBaseAnnotations(ctx, newIngress)
}

// validateBaseAnnotations checks simple formatting, allowed values, and basic constraints for all relevant annotations.
func (v *IngressValidator) validateBaseAnnotations(ctx context.Context, ingress *networkingv1.Ingress) admission.Response {
	// Validate WAF Name using the provided regex constraint
	if val, ok := ingress.Annotations[AnnotationWAFName]; ok {
		wafRegex := `^[0-9a-z](?:(?:[0-9a-z]|-){0,61}[0-9a-z])?$`
		matched, _ := regexp.MatchString(wafRegex, val)
		if !matched {
			return admission.Denied(fmt.Sprintf("Annotation '%s' has an invalid value '%s'. It must match the pattern: %s", AnnotationWAFName, val, wafRegex))
		}
	}

	// Validate Booleans
	boolAnnotations := []string{
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

	// Validate Integers and TTL limits
	intAnnotations := []string{AnnotationCookiePersistenceTTLSeconds, AnnotationPriority}
	for _, ann := range intAnnotations {
		if val, ok := ingress.Annotations[ann]; ok {
			num, err := strconv.Atoi(val)
			if err != nil {
				return admission.Denied(fmt.Sprintf("Annotation '%s' must be a valid integer.", ann))
			}
			if ann == AnnotationCookiePersistenceTTLSeconds && num < 0 {
				return admission.Denied(fmt.Sprintf("Annotation '%s' must be greater than or equal to 0.", ann))
			}
		}
	}

	return admission.Allowed("Validation passed.")
}