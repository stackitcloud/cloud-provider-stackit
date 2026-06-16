package ingress

import (
	"context"
	"errors"
	"fmt"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/alb/ingress/spec"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/labels"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
	certsdk "github.com/stackitcloud/stackit-sdk-go/services/certificates/v2api"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *IngressClassReconciler) applyALB(ctx context.Context, ingressClass *networkingv1.IngressClass) error {
	ingresses, err := r.getIngressesForIngressClass(ctx, ingressClass)
	if err != nil {
		return fmt.Errorf("failed to get ingresses for class: %w", err)
	}

	secrets, err := r.getTLSSecretsFromIngresses(ctx, ingresses)
	if err != nil {
		return fmt.Errorf("failed to get secrets for ingresses: %w", err)
	}

	services, err := r.getServicesForIngresses(ctx, ingresses)
	if err != nil {
		return fmt.Errorf("failed to get services for ingresses: %w", err)
	}

	nodes := corev1.NodeList{}
	if err := r.Client.List(ctx, &nodes); err != nil {
		return fmt.Errorf("failed to get nodes: %w", err)
	}

	existingALB, err := r.ALBClient.GetLoadBalancer(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region, "my-alb") // TODO: Set real name
	if err != nil && !errors.Is(err, stackit.ErrorNotFound) {
		return fmt.Errorf("failed to get load balancer: %w", err)
	}
	if errors.Is(err, stackit.ErrorNotFound) {
		existingALB = nil
	}

	tree, _ := spec.BuildTree( // TODO: deal with errors
		ingressClass,
		ingresses,
		secrets,
		services,
		nodes.Items,
		existingALB,
	)

	// Create certificates that are needed and get an ID mapping
	// TODO: Deal with paging.
	projectCertificates, err := r.CertificateClient.ListCertificate(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region)
	if err != nil {
		return fmt.Errorf("failed to list certificates: %w", err)
	}
	ingressClassCertificates := []certsdk.GetCertificateResponse{}
	for _, cert := range projectCertificates.Items {
		if cert.Labels != nil && (*cert.Labels)[labels.LabelIngressClassUID] == string(ingressClass.UID) {
			// TODO: Check for nil-ness
			ingressClassCertificates = append(ingressClassCertificates, cert)
		}
	}
	// Optional: Delete any that are already no longer used and will not be used with the update.

	missingCertificates := tree.GetMissingCertificates(ingressClassCertificates)
	for fingerprint, c := range missingCertificates {
		createCertificatePayload := &certsdk.CreateCertificatePayload{
			Name:       new(string(fingerprint)), // TODO: Add some identifying prefix and shorten it to 63 characters
			ProjectId:  &r.ALBConfig.Global.ProjectID,
			PrivateKey: new(string(c.PrivateKey)),
			PublicKey:  new(string(c.PublicKey)),
			Labels: &map[string]string{
				labels.LabelIngressClassUID: string(ingressClass.UID),
			},
		}
		response, err := r.CertificateClient.CreateCertificate(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region, createCertificatePayload)
		if err != nil {
			return fmt.Errorf("failed to create certificate: %w", err)
		}
		// TODO: Check for nil-ness
		ctrl.LoggerFrom(ctx).Info("Created certificate", "id", response.Id, "fingerprint", fingerprint)
		ingressClassCertificates = append(ingressClassCertificates, *response)
	}

	certIDMap := map[spec.CertificateFingerprint]string{}
	for _, cert := range ingressClassCertificates {
		certIDMap[spec.CertificateFingerprint(*cert.Data.FingerprintSha256)] = *cert.Id
	}

	if existingALB == nil {
		alb := tree.ToCreatePayload(certIDMap, r.ALBConfig.ApplicationLoadBalancer.NetworkID, r.ALBConfig.Global.Region)
		_, err := r.ALBClient.CreateLoadBalancer(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region, alb)
		if err != nil {
			return fmt.Errorf("failed to create load balancer: %w", err)
		}
		ctrl.LoggerFrom(ctx).Info("Created application load balancer", "name", alb.Name)
		return nil
	}

	alb := tree.ToUpdatePayload(certIDMap, r.ALBConfig.ApplicationLoadBalancer.NetworkID, r.ALBConfig.Global.Region)
	if !updateNeeded(existingALB, alb) {
		return nil
	}

	_, err = r.ALBClient.UpdateLoadBalancer(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region, *alb.Name, alb)
	if err != nil {
		return fmt.Errorf("failed to update load balancer: %w", err)
	}
	ctrl.LoggerFrom(ctx).Info("Updated application load balancer", "name", alb.Name)

	// TODO:
	// Clean up orphaned certificates now that the ALB is successfully detached from them
	// if cleanupErr := r.cleanupUnusedCertificates(ctx, ingressClass, activeCertIDs); cleanupErr != nil {
	// 	log.Error(cleanupErr, "failed to cleanup unused certificates")
	// }

	return nil
}

// getServicesForIngresses returns all services that are referenced anywhere in any of the ingresses.
// It ignores services that are not found.
// TODO: Support resource backends (that reference services).
func (r *IngressClassReconciler) getServicesForIngresses(ctx context.Context, ingresses []networkingv1.Ingress) ([]corev1.Service, error) {
	// TODO: This and the next function can be generalized with a NamespacedReferenceList function. Possibly with a callback function for the indexes. Should return a map indexed with types.NamespacedName.
	services := []corev1.Service{}
	for _, ingress := range ingresses {
		for ruleIndex, rule := range ingress.Spec.Rules {
			for pathIndex, path := range rule.HTTP.Paths {
				if path.Backend.Service.Name == "" {
					continue
				}
				service := corev1.Service{}
				err := r.Client.Get(ctx, types.NamespacedName{Namespace: ingress.Namespace, Name: path.Backend.Service.Name}, &service)
				if client.IgnoreNotFound(err) != nil {
					return nil, fmt.Errorf(
						"failed to get service %s referenced in ingress %s at rule %d and path %d (zero-indexed): %w",
						types.NamespacedName{Namespace: ingress.Namespace, Name: path.Backend.Service.Name},
						client.ObjectKeyFromObject(&ingress),
						ruleIndex, pathIndex, err,
					)
				}
				if !apierrors.IsNotFound(err) {
					services = append(services, service)
				}
			}
		}
		if ingress.Spec.DefaultBackend == nil || ingress.Spec.DefaultBackend.Service == nil || ingress.Spec.DefaultBackend.Service.Name == "" {
			continue
		}
		service := corev1.Service{}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: ingress.Namespace, Name: ingress.Spec.DefaultBackend.Service.Name}, &service)
		if client.IgnoreNotFound(err) != nil {
			return nil, fmt.Errorf(
				"failed to get service %s referenced in the default backend of ingress %s: %w",
				types.NamespacedName{Namespace: ingress.Namespace, Name: ingress.Spec.DefaultBackend.Service.Name},
				client.ObjectKeyFromObject(&ingress),
				err,
			)
		}
		if !apierrors.IsNotFound(err) {
			services = append(services, service)
		}
	}
	return services, nil
}

func (r *IngressClassReconciler) getTLSSecretsFromIngresses(ctx context.Context, ingresses []networkingv1.Ingress) ([]corev1.Secret, error) {
	secrets := []corev1.Secret{}
	for _, ingress := range ingresses {
		for tlsIndex, tls := range ingress.Spec.TLS {
			secret := corev1.Secret{}
			err := r.Client.Get(ctx, types.NamespacedName{Namespace: ingress.Namespace, Name: tls.SecretName}, &secret)
			if client.IgnoreNotFound(err) != nil {
				return nil, fmt.Errorf(
					"failed to get secret %s referenced in the ingress %s at position %d: %w",
					types.NamespacedName{Namespace: ingress.Namespace, Name: tls.SecretName},
					client.ObjectKeyFromObject(&ingress),
					tlsIndex, err,
				)
			}
			if !apierrors.IsNotFound(err) {
				secrets = append(secrets, secret)
			}
		}
	}
	return secrets, nil
}

func updateNeeded(alb *albsdk.LoadBalancer, albPayload *albsdk.UpdateLoadBalancerPayload) bool {
	return listenersChanged(alb.Listeners, albPayload.Listeners) || targetPoolsChanged(alb.TargetPools, albPayload.TargetPools)
}

func listenersChanged(current, desired []albsdk.Listener) bool {
	if len(current) != len(desired) {
		return true
	}
	for i := range current {
		c, d := current[i], desired[i]

		if ptr.Deref(c.Protocol, "") != ptr.Deref(d.Protocol, "") ||
			ptr.Deref(c.Port, 0) != ptr.Deref(d.Port, 0) ||
			ptr.Deref(c.WafConfigName, "") != ptr.Deref(d.WafConfigName, "") {
			return true
		}

		if httpOptionsChanged(c.Http, d.Http) || httpsOptionsChanged(c.Https, d.Https) {
			return true
		}
	}
	return false
}

func httpOptionsChanged(c, d *albsdk.ProtocolOptionsHTTP) bool {
	if c == nil && d == nil {
		return false
	}
	if c == nil || d == nil || len(c.Hosts) != len(d.Hosts) {
		return true
	}

	for i := range c.Hosts {
		ch, dh := c.Hosts[i], d.Hosts[i]
		if ptr.Deref(ch.Host, "") != ptr.Deref(dh.Host, "") || len(ch.Rules) != len(dh.Rules) {
			return true
		}

		for j := range ch.Rules {
			cr, dr := ch.Rules[j], dh.Rules[j]
			if pathChanged(cr.Path, dr.Path) {
				return true
			}
			if ptr.Deref(cr.WebSocket, false) != ptr.Deref(dr.WebSocket, false) ||
				ptr.Deref(cr.TargetPool, "") != ptr.Deref(dr.TargetPool, "") {
				return true
			}
		}
	}
	return false
}

func pathChanged(c, d *albsdk.Path) bool {
	if c == nil && d == nil {
		return false
	}
	if c == nil || d == nil {
		return true
	}
	return ptr.Deref(c.Prefix, "") != ptr.Deref(d.Prefix, "") || ptr.Deref(c.ExactMatch, "") != ptr.Deref(d.ExactMatch, "")
}

func httpsOptionsChanged(c, d *albsdk.ProtocolOptionsHTTPS) bool {
	if c == nil && d == nil {
		return false
	}
	if c == nil || d == nil {
		return true
	}
	return len(c.CertificateConfig.CertificateIds) != len(d.CertificateConfig.CertificateIds)
}

func targetPoolsChanged(current, desired []albsdk.TargetPool) bool {
	if len(current) != len(desired) {
		return true
	}
	for i := range current {
		c, d := current[i], desired[i]

		if ptr.Deref(c.Name, "") != ptr.Deref(d.Name, "") ||
			ptr.Deref(c.TargetPort, 0) != ptr.Deref(d.TargetPort, 0) ||
			len(c.Targets) != len(d.Targets) {
			return true
		}

		if (c.TlsConfig == nil) != (d.TlsConfig == nil) {
			return true
		}
		if c.TlsConfig != nil && d.TlsConfig != nil {
			if ptr.Deref(c.TlsConfig.SkipCertificateValidation, false) != ptr.Deref(d.TlsConfig.SkipCertificateValidation, false) ||
				ptr.Deref(c.TlsConfig.CustomCa, "") != ptr.Deref(d.TlsConfig.CustomCa, "") {
				return true
			}
		}
	}
	return false
}
