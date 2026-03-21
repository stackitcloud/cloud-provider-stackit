package ingress

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
	certsdk "github.com/stackitcloud/stackit-sdk-go/services/certificates/v2api"
)

const (
	// externalIPAnnotation references an OpenStack floating IP that should be used by the application load balancer.
	// If set it will be used instead of an ephemeral IP. The IP must be created by the customer. When the service is deleted,
	// the floating IP will not be deleted. The IP is ignored if the alb.stackit.cloud/internal-alb is set.
	// If the annotation is set after the creation it must match the ephemeral IP.
	// This will promote the ephemeral IP to a static IP.
	externalIPAnnotation = "alb.stackit.cloud/external-address"
	// If true, the application load balancer is not exposed via a floating IP.
	internalIPAnnotation = "alb.stackit.cloud/internal-alb"
	// If true, the application load balancer enables TLS bridging.
	// It uses the trusted CAs from the operating system for validation.
	tlsBridgingTrustedCaAnnotation = "alb.stackit.cloud/tls-bridging-trusted-ca"
	// If set, the application load balancer enables TLS bridging with a custom CA provided as value.
	tlsBridgingCustomCaAnnotation = "alb.stackit.cloud/tls-bridging-custom-ca"
	// If true, the application load balancer enables TLS bridging but skips validation.
	tlsBridgingSkipValidationAnnotation = "alb.stackit.cloud/tls-bridging-no-validation"
	// priorityAnnotation is used to set the priority of the Ingress.
	priorityAnnotation = "alb.stackit.cloud/priority"
)

const (
	// minPriority and maxPriority are the minimum and maximum values for the priority annotation.
	minPriority = 1
	maxPriority = 25
	// defaultPriority is the default priority for Ingress resources that do not have a priority annotation.
	defaultPriority = 0
)

type ruleMetadata struct {
	path             string
	host             string
	priority         int
	pathLength       int
	pathTypeVal      int
	ingressName      string
	ingressNamespace string
	ruleOrder        int
	targetPool       string
}

// albSpecFromIngress generates a complete ALB specification for a given set of Ingress resources that reference the same IngressClass.
// It merges and sorts all routing rules across the ingresses based on host, priority, path specificity, path type, and ingress origin.
// The resulting ALB payload includes targets derived from cluster nodes, target pools per backend service, HTTP(S) listeners,
// and optional TLS certificate bindings. This spec is later used to create or update the actual ALB instance.
func (r *IngressClassReconciler) albSpecFromIngress( //nolint:funlen,gocyclo // We go through a lot of fields. Not much complexity.
	ctx context.Context,
	ingresses []*networkingv1.Ingress,
	ingressClass *networkingv1.IngressClass,
	networkID *string,
	nodes []corev1.Node,
	services map[string]corev1.Service,
) (bool, *albsdk.CreateLoadBalancerPayload, error) {
	targetPools := []albsdk.TargetPool{}
	targetPoolSeen := map[string]bool{}
	allCertificateIDs := []string{}
	ruleMetadataList := []ruleMetadata{}

	alb := &albsdk.CreateLoadBalancerPayload{
		Options: &albsdk.LoadBalancerOptions{},
		Networks: []albsdk.Network{
			{
				NetworkId: networkID,
				Role:      ptr.To("ROLE_LISTENERS_AND_TARGETS"),
			},
		},
	}

	// Create targets for each node in the cluster
	targets := []albsdk.Target{}
	for i := range nodes {
		node := nodes[i]
		for j := range node.Status.Addresses {
			address := node.Status.Addresses[j]
			if address.Type == corev1.NodeInternalIP {
				targets = append(targets, albsdk.Target{
					DisplayName: &node.Name,
					Ip:          &address.Address,
				})
				break
			}
		}
	}

	// For each Ingress, add its rules to the combined rule list
	for _, ingress := range ingresses {
		priority := getIngressPriority(ingress)

		for _, rule := range ingress.Spec.Rules {
			for j, path := range rule.HTTP.Paths {
				nodePort, err := getNodePort(services, path)
				if err != nil {
					return false, nil, err
				}

				targetPoolName := fmt.Sprintf("pool-%d", nodePort)
				if !targetPoolSeen[targetPoolName] {
					addTargetPool(ctx, ingress, targetPoolName, &targetPools, nodePort, targets)
					targetPoolSeen[targetPoolName] = true
				}

				pathTypeVal := 1
				if path.PathType != nil && *path.PathType == networkingv1.PathTypeExact {
					pathTypeVal = 0
				}

				ruleMetadataList = append(ruleMetadataList, ruleMetadata{
					path:             path.Path,
					host:             rule.Host,
					priority:         priority,
					pathLength:       len(path.Path),
					pathTypeVal:      pathTypeVal,
					ingressName:      ingress.Name,
					ingressNamespace: ingress.Namespace,
					ruleOrder:        j,
					targetPool:       targetPoolName,
				})
			}
		}

		// Apend certificates from the current Ingress to the combined certificates
		requeueNeeded, certificateIDs, err := r.loadCerts(ctx, ingressClass, ingress)
		if requeueNeeded {
            return true, nil, nil
        }
        if err != nil {
            return false, nil, fmt.Errorf("failed to load tls certificates: %w", err)
        }
		allCertificateIDs = append(allCertificateIDs, certificateIDs...)
	}

	// Sort all collected rules
	sort.SliceStable(ruleMetadataList, func(i, j int) bool {
		a, b := ruleMetadataList[i], ruleMetadataList[j]
		// 1. Host name (lexicographically)
		if a.host != b.host {
			return a.host < b.host
		}
		// 2. Priority annotation (higher priority wins)
		if a.priority != b.priority {
			return a.priority > b.priority
		}
		// 3. Path specificity (longer paths first)
		if a.pathLength != b.pathLength {
			return a.pathLength > b.pathLength
		}
		// 4. Path type precedence (Exact < Prefix)
		if a.pathTypeVal != b.pathTypeVal {
			return a.pathTypeVal < b.pathTypeVal
		}
		// 5. Ingress name tie-breaker
		if a.ingressName != b.ingressName {
			return a.ingressName < b.ingressName
		}
		// 6. Ingress Namespace tie-breaker
		if a.ingressNamespace != b.ingressNamespace {
			return a.ingressNamespace < b.ingressNamespace
		}
		return a.ruleOrder < b.ruleOrder
	})

	// Group rules by host
	hostToRules := map[string][]albsdk.Rule{}
	for _, meta := range ruleMetadataList {
		rule := albsdk.Rule{
			TargetPool: ptr.To(meta.targetPool),
		}
		if meta.pathTypeVal == 0 { // Exact path
			rule.Path = &albsdk.Path{
				ExactMatch: ptr.To(meta.path),
			}
		} else { // Prefix path
			rule.Path = &albsdk.Path{
				Prefix: ptr.To(meta.path),
			}
		}
		hostToRules[meta.host] = append(hostToRules[meta.host], rule)
	}

	// Build Host configs
	httpHosts := []albsdk.HostConfig{}
	hostnames := make([]string, 0, len(hostToRules))
	for host := range hostToRules {
		hostnames = append(hostnames, host)
	}
	sort.Strings(hostnames)

	for _, host := range hostnames {
		rulesCopy := hostToRules[host]
		httpHosts = append(httpHosts, albsdk.HostConfig{
			Host:  ptr.To(host),
			Rules: rulesCopy,
		})
	}

	// Build Listeners
	// Create a default HTTP rule for the ALB Always create an HTTP listener - neecessary step for acme challenge
	// Add TLS listener if any Ingress has TLS configured
	listeners := []albsdk.Listener{
		{
			Name:     ptr.To("http"),
			Port:     ptr.To(int32(80)),
			Protocol: ptr.To("PROTOCOL_HTTP"),
			Http: &albsdk.ProtocolOptionsHTTP{
				Hosts: httpHosts,
			},
		},
	}
	if len(allCertificateIDs) > 0 {
		listeners = append(listeners, albsdk.Listener{
			Name:     ptr.To("https"),
			Port:     ptr.To(int32(443)),
			Protocol: ptr.To("PROTOCOL_HTTPS"),
			Http: &albsdk.ProtocolOptionsHTTP{
				Hosts: httpHosts,
			},
			Https: &albsdk.ProtocolOptionsHTTPS{
				CertificateConfig: &albsdk.CertificateConfig{
					CertificateIds: allCertificateIDs,
				},
			},
		})
	}

	// Set the IP address of the ALB
	err := setIPAddresses(ingressClass, alb)
	if err != nil {
		return false, nil, fmt.Errorf("failed to set IP address: %w", err)
	}

	alb.Name = ptr.To(getAlbName(ingressClass))
	alb.Listeners = listeners
	alb.TargetPools = targetPools

	return false, alb, nil
}

// laodCerts loads the tls certificates from Ingress to the Certificates API
func (r *IngressClassReconciler) loadCerts(
	ctx context.Context,
	ingressClass *networkingv1.IngressClass,
	ingress *networkingv1.Ingress,
) (bool, []string, error) {
	certificateIDs := []string{}

	for _, tls := range ingress.Spec.TLS {
		if tls.SecretName == "" {
			continue
		}

		secret := &corev1.Secret{}
		if err := r.Client.Get(ctx, types.NamespacedName{Namespace: ingress.Namespace, Name: tls.SecretName}, secret); err != nil {
			return false, nil, fmt.Errorf("failed to get TLS secret: %w", err)
		}

		complete, err := isCertReady(secret)
		if err != nil {
			return false, nil, fmt.Errorf("failed to check if certificate is ready: %w", err)
		}
		if !complete {
			// Requeue: The ACME challenge is still in progress and the certificate is not yet fully issued.
			return true, nil, fmt.Errorf("certificate is not complete: %w", err)
		}

		createCertificatePayload := &certsdk.CreateCertificatePayload{
			Name:       ptr.To(getCertName(ingressClass, ingress, secret)),
			ProjectId:  &r.ProjectID,
			PrivateKey: ptr.To(string(secret.Data["tls.key"])),
			PublicKey:  ptr.To(string(secret.Data["tls.crt"])),
		}
		res, err := r.CertificateClient.CreateCertificate(ctx, r.ProjectID, r.Region, createCertificatePayload)
		if err != nil {
			return false, nil, fmt.Errorf("failed to create certificate: %w", err)
		}

		certificateIDs = append(certificateIDs, *res.Id)
	}
	return false, certificateIDs, nil
}

// cleanupCerts deletes the certificates from the Certificates API that are no longer associated with any Ingress in the IngressClass
func (r *IngressClassReconciler) cleanupCerts(ctx context.Context, ingressClass *networkingv1.IngressClass, ingresses []*networkingv1.Ingress) error {
	// Prepare a map of secret names that are currently being used by the ingresses
	usedSecrets := map[string]bool{}
	for _, ingress := range ingresses {
		for _, tls := range ingress.Spec.TLS {
			if tls.SecretName == "" {
				continue
			}
			// Retrieve the TLS Secret
			tlsSecret := &corev1.Secret{}
			err := r.Client.Get(ctx, types.NamespacedName{Namespace: ingress.Namespace, Name: tls.SecretName}, tlsSecret)
			if err != nil {
				log.Printf("failed to get TLS secret %s: %v", tls.SecretName, err)
				continue
			}
			certName := getCertName(ingressClass, ingress, tlsSecret)
			usedSecrets[certName] = true
		}
	}

	certificatesList, err := r.CertificateClient.ListCertificate(ctx, r.ProjectID, r.Region)
	if err != nil {
		return fmt.Errorf("failed to list certificates: %w", err)
	}

	if certificatesList == nil || certificatesList.Items == nil {
		return nil // No certificates to clean up
	}
	for _, cert := range certificatesList.Items {
		certID := *cert.Id
		certName := *cert.Name

		// The certificatesList contains all certificates in the project, so we need to filter them by the ALB IngressClass UID.
		if !strings.HasPrefix(certName, generateShortUID(ingressClass.UID)) {
			continue
		}

		// If the tls secret is no longer in referenced, delete the certificate
		if _, inUse := usedSecrets[certName]; !inUse {
			err := r.CertificateClient.DeleteCertificate(ctx, r.ProjectID, r.Region, certID)
			if err != nil {
				return fmt.Errorf("failed to delete certificate %s: %v", certName, err)
			}
		}
	}
	return nil
}

// isCertReady checks if the certificate chain is complete (leaf + intermediates).
// This is required during ACME challenges (e.g., cert-manager), where a race condition 
// can occur where the Secret may temporarily contain only the leaf certificate before the 
// full chain is written. Because the STACKIT Application Load Balancer Certificates API
// only validates the cryptographic key match and is immutable (no update call),
// we must wait for the full chain to avoid locking the ALB with an incomplete certificate.
func isCertReady(secret *corev1.Secret) (bool, error) {
	tlsCert := secret.Data["tls.crt"]
	if tlsCert == nil {
		return false, fmt.Errorf("tls.crt not found in secret")
	}

	// Split the certificates in the tls.crt by PEM boundary
	blocks := []*pem.Block{}
	for len(tlsCert) > 0 {
		var block *pem.Block
		block, tlsCert = pem.Decode(tlsCert)
		if block == nil {
			return false, fmt.Errorf("failed to decode certificate")
		}
		blocks = append(blocks, block)
	}

	// Parse the certificates using x509
	certs := []*x509.Certificate{}
	for _, block := range blocks {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return false, fmt.Errorf("failed to parse certificate: %v", err)
		}
		certs = append(certs, cert)
	}

    // A valid, trusted chain must contain at least 2 certificates: 
    // the leaf (domain) and at least one intermediate CA.
	return len(certs) > 1, nil
}

func addTargetPool(
	_ context.Context,
	ingress *networkingv1.Ingress,
	targetPoolName string,
	targetPools *[]albsdk.TargetPool,
	nodePort int32,
	targets []albsdk.Target,
) {
	tlsConfig := &albsdk.TlsConfig{}
	if val, ok := ingress.Annotations[tlsBridgingTrustedCaAnnotation]; ok && val == "true" {
		tlsConfig.Enabled = ptr.To(true)
	}
	if val, ok := ingress.Annotations[tlsBridgingCustomCaAnnotation]; ok && val != "" {
		tlsConfig.Enabled = ptr.To(true)
		tlsConfig.CustomCa = ptr.To(val)
	}
	if val, ok := ingress.Annotations[tlsBridgingSkipValidationAnnotation]; ok && val == "true" {
		tlsConfig.Enabled = ptr.To(true)
		tlsConfig.SkipCertificateValidation = ptr.To(true)
	}
	if tlsConfig.Enabled == nil {
		tlsConfig = nil
	}
	*targetPools = append(*targetPools, albsdk.TargetPool{
		Name:       ptr.To(targetPoolName),
		TargetPort: ptr.To(nodePort),
		TlsConfig:  tlsConfig,
		Targets:    targets,
	})
}

// setIPAddresses configures the Application Load Balancer IP address
// based on IngressClass annotations: internal, ephemeral, or static public IPs.
func setIPAddresses(ingressClass *networkingv1.IngressClass, alb *albsdk.CreateLoadBalancerPayload) error {
	isInternalIP, found := ingressClass.Annotations[internalIPAnnotation]
	if found && isInternalIP == "true" {
		alb.Options = &albsdk.LoadBalancerOptions{
			PrivateNetworkOnly: ptr.To(true),
		}
		return nil
	}
	externalAddress, found := ingressClass.Annotations[externalIPAnnotation]
	if !found {
		alb.Options = &albsdk.LoadBalancerOptions{
			EphemeralAddress: ptr.To(true),
		}
		return nil
	}
	err := validateIPAddress(externalAddress)
	if err != nil {
		return fmt.Errorf("failed to validate external address: %w", err)
	}
	alb.ExternalAddress = ptr.To(externalAddress)
	return nil
}

func validateIPAddress(ipAddr string) error {
	ip, err := netip.ParseAddr(ipAddr)
	if err != nil {
		return fmt.Errorf("invalid format for external IP: %w", err)
	}
	if ip.Is6() {
		return fmt.Errorf("external IP must be an IPv4 address")
	}
	return nil
}

// getNodePort gets the NodePort of the Service
func getNodePort(services map[string]corev1.Service, path networkingv1.HTTPIngressPath) (int32, error) {
	service, found := services[path.Backend.Service.Name]
	if !found {
		return 0, fmt.Errorf("service not found: %s", path.Backend.Service.Name)
	}

	if path.Backend.Service.Port.Name != "" {
		for _, servicePort := range service.Spec.Ports {
			if servicePort.Name == path.Backend.Service.Port.Name {
				if servicePort.NodePort == 0 {
					return 0, fmt.Errorf("port %q of service %q has no node port", servicePort.Name, path.Backend.Service.Name)
				}
				return servicePort.NodePort, nil
			}
		}
	} else {
		for _, servicePort := range service.Spec.Ports {
			if servicePort.Port == path.Backend.Service.Port.Number {
				if servicePort.NodePort == 0 {
					return 0, fmt.Errorf("port %d of service %q has no node port", servicePort.Port, path.Backend.Service.Name)
				}
				return servicePort.NodePort, nil
			}
		}
	}
	return 0, fmt.Errorf("no matching port found for service %q", path.Backend.Service.Name)
}

// getIngressPriority retrieves the priority of the Ingress from its annotations.
func getIngressPriority(ingress *networkingv1.Ingress) int {
	if val, ok := ingress.Annotations[priorityAnnotation]; ok {
		if priority, err := strconv.Atoi(val); err == nil {
			if priority >= minPriority && priority <= maxPriority {
				return priority
			}
		}
	}
	return defaultPriority
}
