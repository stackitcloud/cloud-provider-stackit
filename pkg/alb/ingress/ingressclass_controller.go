/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ingress

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
)

const (
	// finalizerName is the name of the finalizer that is added to the IngressClass
	finalizerName = "stackit.cloud/alb-ingress"
	// controllerName is the name of the ALB controller that the IngressClass should point to for reconciliation
	controllerName = "stackit.cloud/alb-ingress"
)

// IngressClassReconciler reconciles a IngressClass object
type IngressClassReconciler struct { //nolint:revive // Naming this ClassReconciler would be confusing.
	Client            client.Client
	ALBClient         stackit.ApplicationLoadBalancerClient
	CertificateClient stackit.CertificatesClient
	Scheme            *runtime.Scheme
	ProjectID         string
	NetworkID         string
	Region            string
}

// +kubebuilder:rbac:groups=networking.k8s.io.stackit.cloud,resources=ingressclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io.stackit.cloud,resources=ingressclasses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io.stackit.cloud,resources=ingressclasses/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the IngressClass object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.4/pkg/reconcile
func (r *IngressClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ingressClass := &networkingv1.IngressClass{}
	err := r.Client.Get(ctx, req.NamespacedName, ingressClass)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the IngressClass points to the ALB controller
	if ingressClass.Spec.Controller != controllerName {
		// If this IngressClass doesn't point to the ALB controller, ignore this IngressClass
		return ctrl.Result{}, nil
	}

	albIngressList, err := r.getAlbIngressList(ctx, ingressClass)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get the list of Ingresses %s: %w", ingressClass.Name, err)
	}

	if !ingressClass.DeletionTimestamp.IsZero() {
		err := r.handleIngressClassDeletion(ctx, albIngressList, ingressClass)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to handle IngressClass deletion: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer to the IngressClass if not already added
	if controllerutil.AddFinalizer(ingressClass, finalizerName) {
		err := r.Client.Update(ctx, ingressClass)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer to IngressClass: %w", err)
		}
		return ctrl.Result{}, nil
	}

	if len(albIngressList) < 1 {
		err := r.handleIngressClassWithoutIngresses(ctx, albIngressList, ingressClass)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to reconcile %s IngressClass with no Ingresses: %w", getAlbName(ingressClass), err)
		}
		return ctrl.Result{}, nil
	}
	_, err = r.handleIngressClassWithIngresses(ctx, albIngressList, ingressClass)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile %s IngressClass with Ingresses: %w", getAlbName(ingressClass), err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *IngressClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Uncomment the following line adding a pointer to an instance of the controlled resource as an argument
		For(&networkingv1.IngressClass{}).
		Watches(&corev1.Node{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []ctrl.Request {
			// TODO: Add predicates - watch only for specific changes on nodes
			ingressClassList := &networkingv1.IngressClassList{}
			err := r.Client.List(ctx, ingressClassList)
			if err != nil {
				panic(err)
			}
			requestList := []ctrl.Request{}
			for i := range ingressClassList.Items {
				ingressClass := ingressClassList.Items[i]
				requestList = append(requestList, ctrl.Request{
					NamespacedName: client.ObjectKeyFromObject(&ingressClass),
				})
			}
			return requestList
		})).
		Watches(&networkingv1.Ingress{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []ctrl.Request {
			ingress, ok := o.(*networkingv1.Ingress)
			if !ok || ingress.Spec.IngressClassName == nil {
				return nil
			}

			return []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name: *ingress.Spec.IngressClassName,
					},
				},
			}
		})).
		Named("ingressclass").
		Complete(r)
}

// handleIngressClassWithIngresses handles the state of IngressClass when at least one Ingress resource is referencing it.
// It ensures that the ALB is created when it is the first ever Ingress
// referencing the specified IngressClass, and performs updates otherwise.
func (r *IngressClassReconciler) handleIngressClassWithIngresses(
	ctx context.Context,
	ingresses []*networkingv1.Ingress,
	ingressClass *networkingv1.IngressClass,
) (ctrl.Result, error) {
	// Get all nodes and services
	nodes := &corev1.NodeList{}
	err := r.Client.List(ctx, nodes)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get nodes: %w", err)
	}
	serviceList := &corev1.ServiceList{}
	err = r.Client.List(ctx, serviceList)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get services: %w", err)
	}
	services := map[string]corev1.Service{}
	for i := range serviceList.Items {
		service := serviceList.Items[i]
		services[service.Name] = service
	}

	// Create ALB payload from Ingresses
	requeueNeeded, albPayload, err := r.albSpecFromIngress(ctx, ingresses, ingressClass, &r.NetworkID, nodes.Items, services)
	if requeueNeeded {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create alb payload: %w", err)
	}

	// Create ALB if it doesn't exist
	alb, err := r.ALBClient.GetLoadBalancer(ctx, r.ProjectID, r.Region, getAlbName(ingressClass))
	if errors.Is(err, stackit.ErrorNotFound) {
		_, err := r.ALBClient.CreateLoadBalancer(ctx, r.ProjectID, r.Region, albPayload)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create load balancer: %w", err)
		}
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get load balancer: %w", err)
	}

	// Update ALB if it exists and the configuration has changed
	if detectChange(alb, albPayload) {
		updatePayload := &albsdk.UpdateLoadBalancerPayload{
			Name:            albPayload.Name,
			ExternalAddress: albPayload.ExternalAddress,
			Listeners:       albPayload.Listeners,
			Networks:        albPayload.Networks,
			Options:         albPayload.Options,
			TargetPools:     albPayload.TargetPools,
			Version:         alb.Version,
		}

		if _, err := r.ALBClient.UpdateLoadBalancer(ctx, r.ProjectID, r.Region, getAlbName(ingressClass), updatePayload); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update load balancer: %w", err)
		}
	}

	requeue, err := r.updateStatus(ctx, ingresses, ingressClass)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update ingress status: %w", err)
	}
	return requeue, nil
}

// updateStatus updates the status of the Ingresses with the ALB IP address
func (r *IngressClassReconciler) updateStatus(ctx context.Context, ingresses []*networkingv1.Ingress, ingressClass *networkingv1.IngressClass) (ctrl.Result, error) {
	alb, err := r.ALBClient.GetLoadBalancer(ctx, r.ProjectID, r.Region, getAlbName(ingressClass))
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get load balancer: %w", err)
	}

	if *alb.Status != stackit.LBStatusReady {
		// ALB is not yet ready, requeue
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	var albIP string
	if alb.ExternalAddress != nil && *alb.ExternalAddress != "" {
		albIP = *alb.ExternalAddress
	} else if alb.PrivateAddress != nil && *alb.PrivateAddress != "" {
		albIP = *alb.PrivateAddress
	}

	if albIP == "" {
		// ALB ready, but IP not available yet, requeue
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	for _, ingress := range ingresses {
		// Fetch the latest Ingress object to check its current status
		currentIngress := &networkingv1.Ingress{}
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(ingress), currentIngress); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to get latest ingress %s/%s: %v", ingress.Namespace, ingress.Name, err)
		}

		// Check if the IP in the current Ingress status is different
		shouldUpdate := false
		if len(currentIngress.Status.LoadBalancer.Ingress) == 0 {
			shouldUpdate = true
		} else if currentIngress.Status.LoadBalancer.Ingress[0].IP != albIP {
			shouldUpdate = true
		}

		if shouldUpdate {
			currentIngress.Status.LoadBalancer.Ingress = []networkingv1.IngressLoadBalancerIngress{
				{IP: albIP},
			}
			if err := r.Client.Status().Update(ctx, currentIngress); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update ingress status %s/%s: %v", currentIngress.Namespace, currentIngress.Name, err)
			}
		}
	}

	return ctrl.Result{}, nil
}

// handleIngressClassWithoutIngresses handles the state of the IngressClass that is not referenced by any Ingress
func (r *IngressClassReconciler) handleIngressClassWithoutIngresses(
	ctx context.Context,
	ingresses []*networkingv1.Ingress,
	ingressClass *networkingv1.IngressClass,
) error {
	err := r.ALBClient.DeleteLoadBalancer(ctx, r.ProjectID, r.Region, getAlbName(ingressClass))
	if err != nil {
		return fmt.Errorf("failed to delete load balancer: %w", err)
	}
	err = r.cleanupCerts(ctx, ingressClass, ingresses)
	if err != nil {
		return fmt.Errorf("failed to clean up certificates: %w", err)
	}

	return nil
}

// handleIngressClassDeletion handles the deletion of IngressClass resource.
// It ensures that the ALB is deleted only when no other Ingresses
// are referencing the the same IngressClass.
func (r *IngressClassReconciler) handleIngressClassDeletion(
	ctx context.Context,
	ingresses []*networkingv1.Ingress,
	ingressClass *networkingv1.IngressClass,
) error {
	// Before deleting ALB, ensure no other Ingresses with the same IngressClassName exist
	if len(ingresses) < 1 {
		err := r.ALBClient.DeleteLoadBalancer(ctx, r.ProjectID, r.Region, getAlbName(ingressClass))
		if err != nil {
			return fmt.Errorf("failed to delete load balancer: %w", err)
		}
		// Remove finalizer from the IngressClass
		if controllerutil.RemoveFinalizer(ingressClass, finalizerName) {
			err := r.Client.Update(ctx, ingressClass)
			if err != nil {
				return fmt.Errorf("failed to remove finalizer from IngressClass: %w", err)
			}
		}
	}

	// TODO: Throw en error saying other ingresses are still referencing this ingress class
	return nil
}

// detectChange checks if there is any difference between the current and desired ALB configuration.
func detectChange(alb *albsdk.LoadBalancer, albPayload *albsdk.CreateLoadBalancerPayload) bool { //nolint:gocyclo,funlen // We check a lot of fields. Not much complexity.
	if len(alb.Listeners) != len(albPayload.Listeners) {
		return true
	}

	for i := range alb.Listeners {
		albListener := (alb.Listeners)[i]
		payloadListener := (albPayload.Listeners)[i]

		if ptr.Deref(albListener.Protocol, "") != ptr.Deref(payloadListener.Protocol, "") ||
			ptr.Deref(albListener.Port, 0) != ptr.Deref(payloadListener.Port, 0) {
			return true
		}

		// WAF config check
		if ptr.Deref(albListener.WafConfigName, "") != ptr.Deref(payloadListener.WafConfigName, "") {
			return true
		}

		// HTTP rules comparison (via Hosts)
		if albListener.Http != nil && payloadListener.Http != nil {
			albHosts := albListener.Http.Hosts
			payloadHosts := payloadListener.Http.Hosts

			if len(albHosts) != len(payloadHosts) {
				return true
			}

			for j := range albHosts {
				albHost := albHosts[j]
				payloadHost := payloadHosts[j]

				if ptr.Deref(albHost.Host, "") != ptr.Deref(payloadHost.Host, "") {
					return true
				}

				if len(albHost.Rules) != len(payloadHost.Rules) {
					return true
				}

				for k := range albHost.Rules {
					albRule := albHost.Rules[k]
					payloadRule := payloadHost.Rules[k]

					if albRule.Path != nil || payloadRule.Path != nil {
						if albRule.Path == nil || payloadRule.Path == nil {
							return true
						}
						if ptr.Deref(albRule.Path.Prefix, "") != ptr.Deref(payloadRule.Path.Prefix, "") {
							return true
						}
						if ptr.Deref(albRule.Path.ExactMatch, "") != ptr.Deref(payloadRule.Path.ExactMatch, "") {
							return true
						}
					}
					if ptr.Deref(albRule.TargetPool, "") != ptr.Deref(payloadRule.TargetPool, "") {
						return true
					}
				}
			}
		} else if albListener.Http != nil || payloadListener.Http != nil {
			// One is nil, one isn't
			return true
		}

		// HTTPS certificate comparison
		if albListener.Https != nil && payloadListener.Https != nil {
			a := albListener.Https.CertificateConfig
			b := payloadListener.Https.CertificateConfig
			if len(a.CertificateIds) != len(b.CertificateIds) {
				return true
			}
		} else if albListener.Https != nil || payloadListener.Https != nil {
			// One is nil, one isn't
			return true
		}
	}

	// TargetPools comparison
	if len(alb.TargetPools) != len(albPayload.TargetPools) {
		return true
	}
	for i := range alb.TargetPools {
		a := alb.TargetPools[i]
		b := albPayload.TargetPools[i]

		if ptr.Deref(a.Name, "") != ptr.Deref(b.Name, "") ||
			ptr.Deref(a.TargetPort, 0) != ptr.Deref(b.TargetPort, 0) {
			return true
		}

		if len(a.Targets) != len(b.Targets) {
			return true
		}

		if (a.TlsConfig == nil) != (b.TlsConfig == nil) {
			return true
		}
		if a.TlsConfig != nil && b.TlsConfig != nil {
			if ptr.Deref(a.TlsConfig.SkipCertificateValidation, false) != ptr.Deref(b.TlsConfig.SkipCertificateValidation, false) ||
				ptr.Deref(a.TlsConfig.CustomCa, "") != ptr.Deref(b.TlsConfig.CustomCa, "") {
				return true
			}
		}
	}

	return false
}

// getAlbIngressList lists all Ingresses that reference specified IngressClass
func (r *IngressClassReconciler) getAlbIngressList(
	ctx context.Context,
	ingressClass *networkingv1.IngressClass,
) ([]*networkingv1.Ingress, error) {
	ingressList := &networkingv1.IngressList{}
	err := r.Client.List(ctx, ingressList)
	if err != nil {
		return nil, fmt.Errorf("failed to list all Ingresses: %w", err)
	}

	ingresses := []*networkingv1.Ingress{}
	for i := range ingressList.Items {
		ingress := ingressList.Items[i]
		if ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName == ingressClass.Name {
			ingresses = append(ingresses, &ingress)
		}
	}

	return ingresses, nil
}

// getAlbName returns the name for the ALB by retrieving the name of the IngressClass
func getAlbName(ingressClass *networkingv1.IngressClass) string {
	return fmt.Sprintf("k8s-ingress-%s", ingressClass.Name)
}

// getCertName generates a unique name for the Certificate using the IngressClass UID, Ingress UID,
// and TLS Secret UID, ensuring it fits within the Kubernetes 63-character limit.
func getCertName(ingressClass *networkingv1.IngressClass, ingress *networkingv1.Ingress, tlsSecret *corev1.Secret) string {
	ingressClassShortUID := generateShortUID(ingressClass.UID)
	ingressShortUID := generateShortUID(ingress.UID)
	tlsSecretShortUID := generateShortUID(tlsSecret.UID)

	return fmt.Sprintf("%s-%s-%s", ingressClassShortUID, ingressShortUID, tlsSecretShortUID)
}

// generateShortUID generates a shortened version of a UID by hashing it.
func generateShortUID(uid types.UID) string {
	hash := md5.Sum([]byte(uid))
	return hex.EncodeToString(hash[:4])
}
