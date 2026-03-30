package ingress

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

type errorEvents struct {
	ingressRef  corev1.ObjectReference
	description string
	typ         string
}

func (r *IngressClassReconciler) SendEvents(class *networkingv1.IngressClass, events []errorEvents) {
	for _, event := range events {
		if event.ingressRef.Name == "" {
			continue
		}
		r.Recorder.Eventf(class, corev1.EventTypeWarning, "ALB", "Error in %s %s in Namespace %s: %s", event.ingressRef.Kind, event.ingressRef.Name, event.ingressRef.Namespace, event.description)
		r.Recorder.Event(&event.ingressRef, corev1.EventTypeWarning, "ALB", event.description)
	}
}
