package ingress

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

func (r *IngressClassReconciler) deleteAllCertsForClass(ctx context.Context, class *networkingv1.IngressClass) error {
	certificatesList, err := r.CertificateClient.ListCertificate(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region)
	if err != nil {
		return fmt.Errorf("failed to list certificates: %w", err)
	}

	if certificatesList == nil || certificatesList.Items == nil {
		return nil // No certificates to clean up
	}

	for _, cert := range certificatesList.Items {
		if strings.HasPrefix(*cert.Name, shortUUID(string(class.UID))) {
			err := r.CertificateClient.DeleteCertificate(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region, *cert.Id)
			if err != nil {
				return fmt.Errorf("failed to delete orphaned certificate %s: %v", *cert.Name, err)
			}
		}
	}
	return nil
}

// getCertName generates a unique name for the Certificate using the IngressClass UID, Ingress UID,
// and TLS Secret UID, ensuring it fits within the Kubernetes 63-character limit.
func getCertName(ingressClass *networkingv1.IngressClass, tlsSecret *corev1.Secret) string {
	classShortUID := shortUUID(string(ingressClass.UID))
	tlsSecretShortUID := shortUUID(string(tlsSecret.UID))[:25]

	return fmt.Sprintf("%s-%s", classShortUID, tlsSecretShortUID)
}

func shortUUID(s string) string {
	return strings.ReplaceAll(string(s), "-", "")
}
