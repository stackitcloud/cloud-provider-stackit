package ingress

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/client-go/tools/record"
)

type errorEvent struct {
	ingressRef  corev1.ObjectReference
	description string
	typ         string
}

func (e *errorEvent) Error() string {
	return e.description
}

func (e *errorEvent) RecordEvent(class *networkingv1.IngressClass, recorder record.EventRecorder) {
	if e.ingressRef.Name == "" {
		return
	}
	recorder.Eventf(class, corev1.EventTypeWarning, "ALB", "Error in %s %s in Namespace %s: %s", e.ingressRef.Kind, e.ingressRef.Name, e.ingressRef.Namespace, e.description)
	recorder.Event(&e.ingressRef, corev1.EventTypeWarning, "ALB", e.description)
}
