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
	"fmt"
	"time"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	networkingv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// finalizerName is the name of the finalizer that is added to Ingress and IngressClass
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
	ALBConfig         stackitconfig.ALBConfig
}

func (r *IngressClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
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

	log.Info("Reconciling IngressClass", "Name", ingressClass.Name)

	if !ingressClass.DeletionTimestamp.IsZero() {
		err := r.handleIngressClassDeletion(ctx, ingressClass)
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

	alb, errorList, err := r.getAlbSpecForIngressClass(ctx, ingressClass)
	if err != nil {
		// todo handle error (write event)
		log.Error(err, "failed to get alb spec for IngressClass")
	}

	err = r.applyALB(ctx, alb)
	if err != nil {
		// todo handle error (write event)
		log.Error(err, "failed to update alb")
	}

	for _, errorEvent := range errorList {
		log.Info(errorEvent.description, "typ", errorEvent.typ, "ingressRef", errorEvent.ingressRef)
	}

	requeue, err := r.updateStatus(ctx, ingressClass)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update ingress status: %w", err)
	}

	log.Info("Successfully reconciled IngressClass", "Name", ingressClass.Name)

	return requeue, nil
}

// updateStatus updates the status of the Ingresses with the ALB IP address
func (r *IngressClassReconciler) updateStatus(ctx context.Context, ingressClass *networkingv1.IngressClass) (ctrl.Result, error) {
	alb, err := r.ALBClient.GetLoadBalancer(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region, string(ingressClass.UID))
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
		return ctrl.Result{}, fmt.Errorf("alb is ready but has no IPs %v", alb.Name)
	}

	ingresses, err := r.getIngressesForIngressClass(ctx, ingressClass)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get ingresses: %w", err)
	}

	for _, ingress := range ingresses {
		before := ingress.DeepCopy()

		ingress.Status.LoadBalancer.Ingress = []networkingv1.IngressLoadBalancerIngress{
			{
				IP: albIP,
			},
		}

		if apiequality.Semantic.DeepEqual(before, ingress) {
			continue
		}
		patch := client.MergeFrom(before)
		if err := r.Client.Status().Patch(ctx, &ingress, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to patch ingress status object: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

// handleIngressClassDeletion handles the deletion of IngressClass resource.
// It does not wait until all ingresses are deleted. It just removes the status from the ingresses and removes the Alb.
// If this blocked the IngressClass would be there forever as there is no ownerReference in the ingresses.
func (r *IngressClassReconciler) handleIngressClassDeletion(
	ctx context.Context,
	ingressClass *networkingv1.IngressClass,
) error {
	ingresses, err := r.getIngressesForIngressClass(ctx, ingressClass)
	if err != nil {
		return err
	}

	for _, ingress := range ingresses {
		before := ingress.DeepCopy()

		ingress.Status.LoadBalancer.Ingress = []networkingv1.IngressLoadBalancerIngress{}

		if apiequality.Semantic.DeepEqual(before, ingress) {
			continue
		}
		patch := client.MergeFrom(before)
		if err := r.Client.Status().Patch(ctx, &ingress, patch); err != nil {
			return fmt.Errorf("failed to patch shoot object: %w", err)
		}
	}

	err = r.ALBClient.DeleteLoadBalancer(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region, string(ingressClass.UID))
	if err != nil {
		return fmt.Errorf("failed to delete load balancer: %w", err)
	}

	err = r.deleteAllCertsForClass(ctx, ingressClass)
	if err != nil {
		return err
	}

	if controllerutil.RemoveFinalizer(ingressClass, finalizerName) {
		err = r.Client.Update(ctx, ingressClass)
		if err != nil {
			return fmt.Errorf("failed to remove finalizer from IngressClass: %w", err)
		}
	}

	return nil
}
