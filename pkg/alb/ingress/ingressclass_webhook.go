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
	admissionv1 "k8s.io/api/admission/v1"
)

type IngressClassValidator struct {
	Client  client.Client
	Decoder admission.Decoder
}

func (v *IngressClassValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	switch req.Operation {
	case admissionv1.Create:
		return v.handleCreate(ctx, req)
	case admissionv1.Update:
		return v.handleUpdate(ctx, req)
	default:
		return admission.Allowed("Unhandled operation allowed.")
	}
}

func (v *IngressClassValidator) handleCreate(ctx context.Context, req admission.Request) admission.Response {
	newClass := &networkingv1.IngressClass{}
	if err := v.Decoder.Decode(req, newClass); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if newClass.Spec.Controller != controllerName {
		return admission.Allowed("Not a STACKIT ALB IngressClass.")
	}
	
	if val, ok := newClass.Annotations[AnnotationExternalIP]; ok {
		if net.ParseIP(val) == nil {
			return admission.Denied(fmt.Sprintf("Annotation '%s' must be a valid IP address.", AnnotationExternalIP))
		}
	}
	
	if resp := v.validateBaseAnnotations(newClass); !resp.Allowed {
		return resp
	}

	return admission.Allowed("IngressClass creation valid.")
}

func (v *IngressClassValidator) handleUpdate(ctx context.Context, req admission.Request) admission.Response {
	oldClass := &networkingv1.IngressClass{}
	if err := v.Decoder.DecodeRaw(req.OldObject, oldClass); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	newClass := &networkingv1.IngressClass{}
	if err := v.Decoder.Decode(req, newClass); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if newClass.Spec.Controller != controllerName {
		return admission.Allowed("Not a STACKIT ALB IngressClass.")
	}

	if val, ok := newClass.Annotations[AnnotationExternalIP]; ok {
		if net.ParseIP(val) == nil {
			return admission.Denied(fmt.Sprintf("Annotation '%s' must be a valid IP address.", AnnotationExternalIP))
		}
	}

	if resp := v.validateBaseAnnotations(newClass); !resp.Allowed {
		return resp
	}

	if resp := v.validateIPUpdate(ctx, oldClass, newClass); !resp.Allowed {
		return resp
	}

	return admission.Allowed("IngressClass creation valid.")
}

// validateBaseAnnotations checks simple formatting and allowed values for IngressClass annotations.
func (v *IngressClassValidator) validateBaseAnnotations(ingressClass *networkingv1.IngressClass) admission.Response {
	if val, ok := ingressClass.Annotations[AnnotationExternalIP]; ok {
		if net.ParseIP(val) == nil {
			return admission.Denied(fmt.Sprintf("Annotation '%s' must be a valid IP address.", AnnotationExternalIP))
		}
	}

	// Network Mode Check.
	mode, exists := ingressClass.Annotations[AnnotationNetworkMode]
	if !exists {
		return admission.Denied("The annotation '" + AnnotationNetworkMode + "' is mandatory for STACKIT ALB IngressClasses.")
	}
	if mode != "NodePort" {
		return admission.Denied(fmt.Sprintf("The annotation '%s' currently only supports the value 'NodePort'.", AnnotationNetworkMode))
	}

	// Service Plan Check.
	if val, ok := ingressClass.Annotations[AnnotationPlanID]; ok {
		switch val {
		case "p10", "p50", "p250", "p750":
		default:
			return admission.Denied(fmt.Sprintf("Annotation '%s' has an invalid value '%s'. Allowed values are: p10, p50, p250, p750.", AnnotationPlanID, val))
		}
	}

	// Validate Listener Ports (Must be between 1 and 65535)
	portAnnotations := []string{AnnotationHTTPPort, AnnotationHTTPSPort}
	for _, ann := range portAnnotations {
		if val, ok := ingressClass.Annotations[ann]; ok {
			port, err := strconv.Atoi(val)
			if err != nil || port < 1 || port > 65535 {
				return admission.Denied(fmt.Sprintf("Annotation '%s' must be a valid port number between 1 and 65535.", ann))
			}
		}
	}

	return admission.Allowed("Base annotations valid.")
}

// validateIPUpdate enforces the strict update rules for the external IP annotation.
func (v *IngressClassValidator) validateIPUpdate(ctx context.Context, oldClass, newClass *networkingv1.IngressClass) admission.Response {
	oldIP, oldHadIP := oldClass.Annotations[AnnotationExternalIP]
	newIP, newHasIP := newClass.Annotations[AnnotationExternalIP]

	if oldHadIP && newHasIP && oldIP != newIP {
		errMsg := fmt.Sprintf("Changing an existing static IP address is not allowed. The annotation '%s' cannot be updated from '%s' to '%s'.",
			AnnotationExternalIP, oldIP, newIP)
		return admission.Denied(errMsg)
	}

	if !oldHadIP && newHasIP {
		currentAssignedIP, err := v.getAssignedEphemeralIP(ctx, newClass.Name)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		if currentAssignedIP == "" || currentAssignedIP != newIP {
			errMsg := fmt.Sprintf("The load balancer can only be promoted to a static IP address that matches its current ephemeral IP (currently assigned: '%s', requested: '%s').",
				currentAssignedIP, newIP)
			return admission.Denied(errMsg)
		}
	}

	return admission.Allowed("IngressClass IP update valid.")
}

// getAssignedEphemeralIP scans the cluster for an Ingress using this class and returns its assigned IP.
func (v *IngressClassValidator) getAssignedEphemeralIP(ctx context.Context, className string) (string, error) {
	ingressList := &networkingv1.IngressList{}
	if err := v.Client.List(ctx, ingressList); err != nil {
		return "", err
	}

	for _, ing := range ingressList.Items {
		if ing.Spec.IngressClassName != nil && *ing.Spec.IngressClassName == className {
			if len(ing.Status.LoadBalancer.Ingress) > 0 {
				return ing.Status.LoadBalancer.Ingress[0].IP, nil
			}
		}
	}

	return "", nil
}