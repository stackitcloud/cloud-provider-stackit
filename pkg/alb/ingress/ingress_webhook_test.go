package ingress

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestIngressValidator_Handle(t *testing.T) {
	s := scheme.Scheme
	_ = networkingv1.AddToScheme(s)

	managedIngressClassName := "stackit-alb"
	managedIngressClass := &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: managedIngressClassName},
		Spec: networkingv1.IngressClassSpec{
			Controller: controllerName,
		},
	}

	unmanagedIngressClassName := "nginx"
	unmanagedIngressClass := &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: unmanagedIngressClassName},
		Spec: networkingv1.IngressClassSpec{
			Controller: "k8s.io/ingress-nginx",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(managedIngressClass, unmanagedIngressClass).Build()
	decoder := admission.NewDecoder(s)

	validator := &IngressValidator{
		Client:  fakeClient,
		Decoder: decoder,
	}

	tests := []struct {
		name          string
		operation     admissionv1.Operation
		className     *string
		annotations   map[string]string
		expectAllowed bool
	}{
		{
			name:      "Valid Ingress (Create)",
			operation: admissionv1.Create,
			className: &managedIngressClassName,
			annotations: map[string]string{
				AnnotationHTTPSOnly: "true",
				AnnotationPriority:  "100",
			},
			expectAllowed: true,
		},
		{
			name:      "Valid Ingress (Update)",
			operation: admissionv1.Update,
			className: &managedIngressClassName,
			annotations: map[string]string{
				AnnotationHTTPSOnly:                   "false",
				AnnotationCookiePersistenceTTLSeconds: "3600",
			},
			expectAllowed: true,
		},
		{
			name:          "No IngressClass - Should Ignore and Allow",
			operation:     admissionv1.Create,
			className:     nil,
			annotations:   map[string]string{},
			expectAllowed: true,
		},
		{
			name:      "Unmanaged IngressClass - Should Ignore and Allow",
			operation: admissionv1.Create,
			className: &unmanagedIngressClassName,
			annotations: map[string]string{
				AnnotationHTTPSOnly: "not-a-bool",
			},
			expectAllowed: true,
		},
		{
			name:      "Denied - Invalid Boolean",
			operation: admissionv1.Create,
			className: &managedIngressClassName,
			annotations: map[string]string{
				AnnotationHTTPSOnly: "not-a-bool",
			},
			expectAllowed: false,
		},
		{
			name:      "Denied - Invalid Integer",
			operation: admissionv1.Create,
			className: &managedIngressClassName,
			annotations: map[string]string{
				AnnotationPriority: "high",
			},
			expectAllowed: false,
		},
		{
			name:      "Denied - Negative TTL",
			operation: admissionv1.Create,
			className: &managedIngressClassName,
			annotations: map[string]string{
				AnnotationCookiePersistenceTTLSeconds: "-50",
			},
			expectAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ingress := &networkingv1.Ingress{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "networking.k8s.io/v1",
					Kind:       "Ingress",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ingress",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: tt.className,
				},
			}
			
			rawIngress, err := json.Marshal(ingress)
			if err != nil {
				t.Fatalf("Failed to marshal ingress: %v", err)
			}

			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: tt.operation,
					Object:    runtime.RawExtension{Raw: rawIngress},
				},
			}

			if tt.operation == admissionv1.Update {
				req.AdmissionRequest.OldObject = runtime.RawExtension{Raw: rawIngress}
			}

			res := validator.Handle(context.TODO(), req)

			if res.Allowed != tt.expectAllowed {
				t.Errorf("Expected Allowed=%v, got Allowed=%v. Result Message: %s",
					tt.expectAllowed, res.Allowed, res.Result.Message)
			}
		})
	}
}