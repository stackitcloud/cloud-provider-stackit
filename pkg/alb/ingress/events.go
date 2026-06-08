package ingress

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/record"
)

type errorEvent struct {
	ingressRef  corev1.ObjectReference
	description string
	fieldPath   *field.Path
}

func (e *errorEvent) Error() string {
	if e.fieldPath != nil {
		return fmt.Sprintf("%s: %s", e.fieldPath.String(), e.description)
	}
	return e.description
}

func (e *errorEvent) RecordEvent(class *networkingv1.IngressClass, recorder record.EventRecorder) {
	if e.ingressRef.Name == "" {
		return
	}

	recorder.Eventf(class, corev1.EventTypeWarning, "ALB", "Error in %s %s in Namespace %s: %s", e.ingressRef.Kind, e.ingressRef.Name, e.ingressRef.Namespace, e.Error())
	recorder.Event(&e.ingressRef, corev1.EventTypeWarning, "ALB", e.Error())
}
