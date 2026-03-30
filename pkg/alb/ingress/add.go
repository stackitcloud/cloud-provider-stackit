package ingress

import (
	"context"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// SetupWithManager sets up the controller with the Manager.
func (r *IngressClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Uncomment the following line adding a pointer to an instance of the controlled resource as an argument
		For(&networkingv1.IngressClass{}, builder.WithPredicates(ingressClassPredicate())).
		Watches(&corev1.Node{}, nodeEventHandler(r.Client), builder.WithPredicates(nodePredicate())).
		Watches(&networkingv1.Ingress{}, ingressEventHandler(r.Client)).
		Named("ingressclass").
		Complete(r)
}

func nodeEventHandler(c client.Client) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []ctrl.Request {
		ingressClassList := &networkingv1.IngressClassList{}
		err := c.List(ctx, ingressClassList)
		if err != nil {
			return nil
		}
		requestList := []ctrl.Request{}
		for i := range ingressClassList.Items {
			if ingressClassList.Items[i].Spec.Controller != controllerName {
				continue
			}
			requestList = append(requestList, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(new(ingressClassList.Items[i])),
			})
		}
		return requestList
	})
}

func ingressEventHandler(c client.Client) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []ctrl.Request {
		ingress, ok := o.(*networkingv1.Ingress)
		if !ok || ingress.Spec.IngressClassName == nil {
			return nil
		}

		ingressClass := &networkingv1.IngressClass{}
		err := c.Get(ctx, client.ObjectKey{Name: *ingress.Spec.IngressClassName}, ingressClass)
		if err != nil {
			return nil
		}

		if ingressClass.Spec.Controller == controllerName {
			return []ctrl.Request{
				{
					NamespacedName: client.ObjectKeyFromObject(ingressClass),
				},
			}
		}

		return nil
	})
}

func nodePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(_ event.CreateEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode, ok := e.ObjectOld.(*corev1.Node)
			if !ok {
				return false
			}
			newNode, ok := e.ObjectNew.(*corev1.Node)
			if !ok {
				return false
			}

			if !reflect.DeepEqual(oldNode.Status.Addresses, newNode.Status.Addresses) {
				return true
			}

			return false
		},
		DeleteFunc: func(_ event.DeleteEvent) bool {
			return true
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return true
		},
	}
}

func ingressClassPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(object client.Object) bool {
		ingressClass, ok := object.(*networkingv1.IngressClass)
		if !ok {
			return false
		}
		return ingressClass.Spec.Controller == controllerName
	})
}
