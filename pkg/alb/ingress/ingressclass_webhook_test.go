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

func TestIngressClassValidator_Handle(t *testing.T) {
	s := scheme.Scheme
	_ = networkingv1.AddToScheme(s)

	decoder := admission.NewDecoder(s)
	managedController := controllerName
	unmanagedController := "k8s.io/ingress-nginx"
	testClassName := "test-class"

	tests := []struct {
		name           string
		operation      admissionv1.Operation
		controller     string
		oldAnnotations map[string]string
		newAnnotations map[string]string
		ephemeralIP    string
		expectAllowed  bool
	}{
		{
			name:       "Valid IngressClass Create",
			operation:  admissionv1.Create,
			controller: managedController,
			newAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationPlanID:      "p50",
				AnnotationHTTPPort:    "80",
			},
			expectAllowed: true,
		},
		{
			name:       "Unmanaged Controller - Should Ignore and Allow",
			operation:  admissionv1.Create,
			controller: unmanagedController,
			newAnnotations: map[string]string{
				AnnotationPlanID: "invalid-plan",
			},
			expectAllowed: true,
		},
		{
			name:       "Missing Network Mode (Denied)",
			operation:  admissionv1.Create,
			controller: managedController,
			newAnnotations: map[string]string{
				AnnotationPlanID: "p50",
			},
			expectAllowed: false,
		},
		{
			name:       "Invalid Network Mode Value (Denied)",
			operation:  admissionv1.Create,
			controller: managedController,
			newAnnotations: map[string]string{
				AnnotationNetworkMode: "LoadBalancer",
			},
			expectAllowed: false,
		},
		{
			name:       "Invalid IP Address Format (Denied)",
			operation:  admissionv1.Create,
			controller: managedController,
			newAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationExternalIP:  "not-an-ip-address",
			},
			expectAllowed: false,
		},
		{
			name:       "Invalid Plan ID (Denied)",
			operation:  admissionv1.Create,
			controller: managedController,
			newAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationPlanID:      "p100",
			},
			expectAllowed: false,
		},
		{
			name:       "Invalid Port (Denied)",
			operation:  admissionv1.Create,
			controller: managedController,
			newAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationHTTPPort:    "99999",
			},
			expectAllowed: false,
		},

		{
			name:       "Update - Keep same Static IP (Allowed)",
			operation:  admissionv1.Update,
			controller: managedController,
			oldAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationExternalIP:  "1.2.3.4",
			},
			newAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationExternalIP:  "1.2.3.4",
			},
			expectAllowed: true,
		},
		{
			name:       "Update - Change existing Static IP (Denied)",
			operation:  admissionv1.Update,
			controller: managedController,
			oldAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationExternalIP:  "1.2.3.4",
			},
			newAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationExternalIP:  "5.6.7.8",
			},
			expectAllowed: false,
		},
		{
			name:       "Update - Promote Ephemeral to Static (Matches - Allowed)",
			operation:  admissionv1.Update,
			controller: managedController,
			oldAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
			},
			newAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationExternalIP:  "9.9.9.9",
			},
			ephemeralIP:   "9.9.9.9",
			expectAllowed: true,
		},
		{
			name:       "Update - Promote Ephemeral to Static (Mismatch - Denied)",
			operation:  admissionv1.Update,
			controller: managedController,
			oldAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
			},
			newAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationExternalIP:  "8.8.8.8",
			},
			ephemeralIP:   "9.9.9.9",
			expectAllowed: false,
		},
		{
			name:       "Update - Promote Ephemeral to Static (No IP Assigned Yet - Denied)",
			operation:  admissionv1.Update,
			controller: managedController,
			oldAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
			},
			newAnnotations: map[string]string{
				AnnotationNetworkMode: "NodePort",
				AnnotationExternalIP:  "8.8.8.8",
			},
			ephemeralIP:   "",
			expectAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder().WithScheme(s)

			if tt.ephemeralIP != "" {
				mockIngress := &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dummy-ingress",
						Namespace: "default",
					},
					Spec: networkingv1.IngressSpec{
						IngressClassName: &testClassName,
					},
					Status: networkingv1.IngressStatus{
						LoadBalancer: networkingv1.IngressLoadBalancerStatus{
							Ingress: []networkingv1.IngressLoadBalancerIngress{
								{IP: tt.ephemeralIP},
							},
						},
					},
				}
				clientBuilder.WithObjects(mockIngress)
			}

			validator := &IngressClassValidator{
				Client:  clientBuilder.Build(),
				Decoder: decoder,
			}

			newClass := &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:        testClassName,
					Annotations: tt.newAnnotations,
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: tt.controller,
				},
			}
			rawNew, err := json.Marshal(newClass)
			if err != nil {
				t.Fatalf("Failed to marshal new IngressClass: %v", err)
			}

			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: tt.operation,
					Object:    runtime.RawExtension{Raw: rawNew},
				},
			}

			if tt.operation == admissionv1.Update {
				oldClass := &networkingv1.IngressClass{
					ObjectMeta: metav1.ObjectMeta{
						Name:        testClassName,
						Annotations: tt.oldAnnotations,
					},
					Spec: networkingv1.IngressClassSpec{
						Controller: tt.controller,
					},
				}
				rawOld, err := json.Marshal(oldClass)
				if err != nil {
					t.Fatalf("Failed to marshal old IngressClass: %v", err)
				}
				req.AdmissionRequest.OldObject = runtime.RawExtension{Raw: rawOld}
			}

			res := validator.Handle(context.TODO(), req)

			if res.Allowed != tt.expectAllowed {
				t.Errorf("Expected Allowed=%v, got Allowed=%v. Result Message: %s",
					tt.expectAllowed, res.Allowed, res.Result.Message)
			}
		})
	}
}