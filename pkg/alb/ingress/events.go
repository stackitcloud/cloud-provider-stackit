package ingress

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

type errorEvent struct {
	ingressRef  corev1.ObjectReference
	description string
	typ         string
}

func (e *errorEvent) Error() string {
	return e.description
}

func (r *IngressClassReconciler) SendEvents(class *networkingv1.IngressClass, validationErrors []error) {
	for _, err := range validationErrors {
		var evtErr *errorEvent 

		if errors.As(err, &evtErr) {
			if evtErr.ingressRef.Name == "" {
				continue
			}
			r.Recorder.Eventf(class, corev1.EventTypeWarning, "ALB", "Error in %s %s in Namespace %s: %s", evtErr.ingressRef.Kind, evtErr.ingressRef.Name, evtErr.ingressRef.Namespace, evtErr.description)
			r.Recorder.Event(&evtErr.ingressRef, corev1.EventTypeWarning, "ALB", evtErr.description)
		}
	}
}