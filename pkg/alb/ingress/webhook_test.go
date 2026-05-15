package ingress

import (
	"context"
	"encoding/json"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	admissionv1 "k8s.io/api/admission/v1"
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
		Client: fakeClient,
	}
	_ = validator.InjectDecoder(decoder)

	tests := []struct {
		name           string
		className      *string
		annotations    map[string]string
		expectAllowed  bool
	}{
		{
			name:      "Valid Ingress",
			className: &managedIngressClassName,
			annotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
			},
			expectAllowed: true,
		},
		{
			name:          "No IngressClass - Should Ignore and Allow",
			className:     nil,
			annotations:   map[string]string{},
			expectAllowed: true,
		},
		{
			name:      "Unmanaged IngressClass - Should Ignore and Allow",
			className: &unmanagedIngressClassName,
			annotations: map[string]string{
				// These are completely invalid for STACKIT ALB,
				// but the webhook shouldn't check them because it's unmanaged.
				AnnotationNetworkMode: "LoadBalancer",
				AnnotationHTTPPort:    "potato",
			},
			expectAllowed: true,
		},
		{
			name:      "Missing Network Mode",
			className: &managedIngressClassName,
			annotations: map[string]string{
				AnnotationHTTPPort: "80",
			},
			expectAllowed: false,
		},
		{
			name:      "Invalid Network Mode Value - Must be NodePort",
			className: &managedIngressClassName,
			annotations: map[string]string{
				AnnotationNetworkMode: "LoadBalancer",
			},
			expectAllowed: false,
		},
		{
			name:      "Invalid Boolean",
			className: &managedIngressClassName,
			annotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationInternal:    "not-a-bool",
			},
			expectAllowed: false,
		},
		{
			name:      "Invalid Port Number - Out of Range",
			className: &managedIngressClassName,
			annotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationHTTPPort:    "99999",
			},
			expectAllowed: false,
		},
		{
			name:      "Invalid IP Address",
			className: &managedIngressClassName,
			annotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationExternalIP:  "300.0.0.1",
			},
			expectAllowed: false,
		},
		{
			name:      "Negative TTL",
			className: &managedIngressClassName,
			annotations: map[string]string{
				AnnotationNetworkMode:                 "NodePort",
				AnnotationCookiePersistenceTTLSeconds: "-50",
			},
			expectAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ingress",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: tt.className,
				},
			}

			// Marshal it into JSON to simulate the API server payload
			rawIngress, err := json.Marshal(ingress)
			if err != nil {
				t.Fatalf("Failed to marshal ingress: %v", err)
			}

			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: runtime.RawExtension{Raw: rawIngress},
				},
			}

			// Execute the webhook
			res := validator.Handle(context.TODO(), req)

			if res.Allowed != tt.expectAllowed {
				t.Errorf("Expected Allowed=%v, got Allowed=%v. Result Message: %s", 
					tt.expectAllowed, res.Allowed, res.Result.Message)
			}
		})
	}
}