package spec

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type errorEvent struct {
	ingress     client.Object
	description string
	fieldPath   *field.Path
}

func (e *errorEvent) Error() string {
	if e.fieldPath != nil {
		return fmt.Sprintf("%s: %s", e.fieldPath.String(), e.description)
	}
	return e.description
}

// TODO: rethink this function
func (e *errorEvent) RecordEvent(class *networkingv1.IngressClass, recorder record.EventRecorder) {
	if e.ingress.GetName() == "" {
		return
	}

	recorder.Eventf(class, corev1.EventTypeWarning, "ALB", "Error in %s in Namespace %s: %s", e.ingress.GetName(), e.ingress.GetNamespace(), e.Error())
	recorder.Event(e.ingress, corev1.EventTypeWarning, "ALB", e.Error())
}
