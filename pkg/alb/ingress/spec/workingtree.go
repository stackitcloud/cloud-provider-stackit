package spec

import (
	"cmp"
	"crypto/sha256"
	cryptotls "crypto/tls"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
	certsdk "github.com/stackitcloud/stackit-sdk-go/services/certificates/v2api"
)

type CertificateFingerprint string

// WorkingTreeALB
//
// The zero value is invalid. Use BuildTree to create a working tree.
//
// Look at the methods how a working tree can be used.
type WorkingTreeALB struct {
	ingressClass *networkingv1.IngressClass
	planId       string

	listeners map[int16]*workingTreeListener
	// We can already create the real type because there is nothing to merge or track.
	targetPools map[ingressPathReference]*albsdk.TargetPool
	// We maintain certificates on ALB-level although we
	certificates map[CertificateFingerprint]WorkingTreeCertificate

	existingALB *albsdk.LoadBalancer
}

type workingTreeListener struct {
	hosts map[string]*workingTreeHost
}

type pathWithType struct {
	pathType networkingv1.PathType
	path     string
}

type workingTreeHost struct {
	paths map[pathWithType]*workingTreePath
}

type ingressPathReference struct {
	namespace string
	name      string
	uid       string
	ruleIndex int
	pathIndex int
}

func (i ingressPathReference) toTargetPoolName() string {
	return fmt.Sprintf("%s-%d-%d", i.uid, i.ruleIndex, i.pathIndex)
}

type workingTreePath struct {
	ingressPathReference ingressPathReference
	websocket            bool
}

type WorkingTreeCertificate struct {
	PublicKey  string
	PrivateKey string
}

// BuildTree creates a new working tree.
//
// It tries to fit as much ingresses into the working tree as possible, bound by the limits of the application load balancer.
//
// Every ingress rule translates into 1 or 2 rules in the ALB.
//
// If existingALB is nil it is assumed that no load balancer exists yet.
//
// It must return all sorts of errors.
//
// The arguments must only contain data related to the ingress class.
//
// This function might change the of ingresses in the provided slice.
func BuildTree(
	ingressClass *networkingv1.IngressClass,
	ingresses []networkingv1.Ingress,
	secrets []corev1.Secret,
	services []corev1.Service,
	nodes []corev1.Node,
	existingALB *albsdk.LoadBalancer,
) (*WorkingTreeALB, []errorEvent) {
	errors := []errorEvent{}

	servicesMap := map[types.NamespacedName]corev1.Service{}
	for _, s := range services {
		servicesMap[client.ObjectKeyFromObject(&s)] = s
	}
	secretsMap := map[types.NamespacedName]corev1.Secret{}
	for _, s := range secrets {
		secretsMap[client.ObjectKeyFromObject(&s)] = s
	}

	targets := getTargetsOfNodes(nodes)

	tree := &WorkingTreeALB{
		ingressClass: ingressClass,
		planId:       GetAnnotation(AnnotationPlanID, "", ingressClass),

		listeners: map[int16]*workingTreeListener{
			80: new(workingTreeListener{
				hosts: map[string]*workingTreeHost{},
			}),
			443: new(workingTreeListener{
				hosts: map[string]*workingTreeHost{},
			}),
		},
		targetPools:  map[ingressPathReference]*albsdk.TargetPool{},
		existingALB:  existingALB,
		certificates: map[CertificateFingerprint]WorkingTreeCertificate{},
	}

	// TODO: Explain sorting
	slices.SortFunc(ingresses, func(a, b networkingv1.Ingress) int {
		if diff := GetAnnotation(AnnotationPriority, 0, &a) - GetAnnotation(AnnotationPriority, 0, &b); diff != 0 {
			return diff
		}
		if diff := a.CreationTimestamp.Compare(b.CreationTimestamp.Time); diff != 0 {
			return diff
		}
		return cmp.Compare(fmt.Sprintf("%s/%s", a.Namespace, a.Name),
			fmt.Sprintf("%s/%s", b.Namespace, b.Name))
	})
	for _, ingress := range ingresses {
		for tlsIndex, tls := range ingress.Spec.TLS {
			// TODO: document that the host field is completely ignored
			secret, exists := secretsMap[types.NamespacedName{Namespace: ingress.Namespace, Name: tls.SecretName}]
			if !exists {
				errors = append(errors, errorEvent{
					ingress:     &ingress,
					fieldPath:   field.NewPath("spec", "tls").Index(tlsIndex).Child("secretName"),
					description: "TLS secret doesn't exist",
				})
				continue
			}
			if secret.Type != corev1.SecretTypeTLS {
				errors = append(errors, errorEvent{
					ingress:     &ingress,
					fieldPath:   field.NewPath("spec", "tls").Index(tlsIndex).Child("secretName"),
					description: "TLS secret isn't of type kubernetes.io/tls",
				})
				continue
			}

			fingerprint, err := ValidateTLSCertAndFingerprint(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
			if err != nil {
				errors = append(errors, errorEvent{
					ingress:     &ingress,
					fieldPath:   field.NewPath("spec", "tls").Index(tlsIndex).Child("secretName"),
					description: fmt.Sprintf("invalid certificate: %s", err.Error()),
				})
				continue
			}

			tree.certificates[CertificateFingerprint(fingerprint)] = WorkingTreeCertificate{
				PublicKey:  string(secret.Data[corev1.TLSCertKey]),
				PrivateKey: string(secret.Data[corev1.TLSPrivateKeyKey]),
			}
		}
		for ruleIndex, rule := range ingress.Spec.Rules {
			// TODO: support rules that don't have a path
			for pathIndex, path := range rule.HTTP.Paths {
				_pathWithType := pathWithType{pathType: ptr.Deref(path.PathType, networkingv1.PathTypeExact), path: path.Path}
				ingressPathReference := ingressPathReference{namespace: ingress.Namespace, name: ingress.Name, uid: string(ingress.UID), ruleIndex: ruleIndex, pathIndex: pathIndex}

				// TODO: What is the default port?
				host, exists := tree.listeners[80].hosts[rule.Host]
				if !exists {
					host = &workingTreeHost{
						paths: map[pathWithType]*workingTreePath{},
					}
					tree.listeners[80].hosts[rule.Host] = host
				}
				// TODO: Define a semantic for ImplementationSpecific path. According to spec it MUST be supported.
				albPath, exists := host.paths[_pathWithType]
				if exists && albPath.ingressPathReference == ingressPathReference {
					errors = append(errors, errorEvent{

						fieldPath:   field.NewPath("spec", "rules", strconv.Itoa(ruleIndex), "path", strconv.Itoa(pathIndex)),
						description: "Path already exists",
					})
					continue
				}
				if !exists {
					albPath = &workingTreePath{
						ingressPathReference: ingressPathReference,
					}
					// TODO: check limits
				}
				albPath.websocket = GetAnnotation(AnnotationWebSocket, false, ingressClass, &ingress)

				targetPool, exists := tree.targetPools[ingressPathReference]
				if !exists {
					targetPool = &albsdk.TargetPool{}
					// TODO: check limits. If the limit is breached here, we shouldn't have added the paths either. So adding the path to the tree must be delayed until all checks are completed.
				}

				// TODO: Support other backends than services.

				service, exists := servicesMap[types.NamespacedName{Namespace: ingress.Namespace, Name: path.Backend.Service.Name}]
				if !exists {
					errors = append(errors, errorEvent{
						ingress:     &ingress,
						fieldPath:   field.NewPath("spec", "rules").Index(ruleIndex).Child("paths").Index(pathIndex).Child("backend", "service", "name"),
						description: "Service doesn't exist",
					})
					continue
				}
				if service.Spec.Type != corev1.ServiceTypeNodePort {
					errors = append(errors, errorEvent{
						ingress:     &ingress,
						fieldPath:   field.NewPath("spec", "rules").Index(ruleIndex).Child("paths").Index(pathIndex).Child("backend", "service", "name"),
						description: "Service is not of type NodePort",
					})
					continue
				}
				nodePort := int32(0)
				for _, port := range service.Spec.Ports {
					if port.Port == path.Backend.Service.Port.Number ||
						port.Name == path.Backend.Service.Port.Name {
						if port.NodePort == 0 {
							errors = append(errors, errorEvent{
								ingress:     &ingress,
								fieldPath:   field.NewPath("spec", "rules").Index(ruleIndex).Child("paths").Index(pathIndex).Child("backend", "service", "name"),
								description: "Service port doesn't have a node port",
							})
							continue
						}
						nodePort = port.NodePort
					}
				}

				targetPool.Name = new(ingressPathReference.toTargetPoolName())
				targetPool.TargetPort = new(nodePort)
				targetPool.Targets = targets
				// TODO: Use TCP health checks for eTP=Cluster
				if service.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyLocal {
					targetPool.ActiveHealthCheck = &albsdk.ActiveHealthCheck{
						AltPort: &service.Spec.HealthCheckNodePort,
						HttpHealthChecks: &albsdk.HttpHealthChecks{
							Path: new("/healthz"),
						},
						// TODO: Optimize interval etc.
					}
				}
				// TODO: Recommend the use of eTP=Local.

				// We are committing to adding this here.
				tree.listeners[80].hosts[rule.Host].paths[_pathWithType] = albPath
				tree.targetPools[ingressPathReference] = targetPool
			}
		}
	}

	return tree, errors
}

func ValidateTLSCertAndFingerprint(publicKey, privateKey []byte) (string, error) {
	cert, err := cryptotls.X509KeyPair(publicKey, privateKey)
	if err != nil {
		return "", err
	}
	sha256Hash := sha256.Sum256(cert.Leaf.Raw)
	return hex.EncodeToString(sha256Hash[:]), nil
}

func getTargetsOfNodes(nodes []corev1.Node) []albsdk.Target {
	targets := []albsdk.Target{}
	for _, node := range nodes {
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
	return targets
}

// GetMissingCertificates returns all certificates that are required by t except those that it finds in existingCert.
// It can be used to create all remaining certificates required to create the ALB.
//
// This function uses the SHA256 fingerprint from the response to match existing certificates.
func (t WorkingTreeALB) GetMissingCertificates(existingCerts []certsdk.GetCertificateResponse) map[CertificateFingerprint]WorkingTreeCertificate {
	missingCerts := map[CertificateFingerprint]WorkingTreeCertificate{}
	existingCertsMap := map[CertificateFingerprint]any{}
	for _, cert := range existingCerts {
		if cert.Data == nil || cert.Data.FingerprintSha256 == nil {
			continue
		}
		existingCertsMap[CertificateFingerprint(*cert.Data.FingerprintSha256)] = nil
	}

	for fingerprint, cert := range t.certificates {
		if _, exists := existingCertsMap[fingerprint]; exists {
			continue
		}
		missingCerts[fingerprint] = cert
	}
	return missingCerts
}

// ToCreatePayload
// Doesn't include certificates that are missing in certificateIDMap.
func (t WorkingTreeALB) ToCreatePayload(
	certificateIDMap map[CertificateFingerprint]string,
	networkID string,
	region string,
) *albsdk.CreateLoadBalancerPayload {
	listeners := []albsdk.Listener{}
	for port, listener := range t.listeners {
		hosts := []albsdk.HostConfig{}
		for hostname, host := range listener.hosts {
			rules := []albsdk.Rule{}
			for path, pathDetails := range host.paths {
				rule := albsdk.Rule{
					TargetPool: new(pathDetails.ingressPathReference.toTargetPoolName()),
					WebSocket:  &pathDetails.websocket,
				}

				switch path.pathType {
				case networkingv1.PathTypeExact:
					rule.Path = new(albsdk.Path{
						ExactMatch: new(path.path),
					})
				default:
					rule.Path = new(albsdk.Path{
						Prefix: new(path.path),
					})
				}

				rules = append(rules, rule)
			}

			hosts = append(hosts, albsdk.HostConfig{
				Host:  &hostname,
				Rules: rules,
			})
		}

		var https *albsdk.ProtocolOptionsHTTPS
		if port == 443 {
			https = &albsdk.ProtocolOptionsHTTPS{
				CertificateConfig: &albsdk.CertificateConfig{
					CertificateIds: []string{},
				},
			}
			for fingerprint := range t.certificates {
				if id, exists := certificateIDMap[fingerprint]; exists {
					https.CertificateConfig.CertificateIds = append(https.CertificateConfig.CertificateIds, id)
				}
			}
		}

		listeners = append(listeners, albsdk.Listener{
			Port: new(int32(port)),
			Http: &albsdk.ProtocolOptionsHTTP{
				Hosts: hosts,
			},
			Https: https,
		})
	}

	targetPools := []albsdk.TargetPool{}
	for _, targetPool := range t.targetPools {
		targetPools = append(targetPools, *targetPool)
	}
	slices.SortFunc(targetPools, func(a, b albsdk.TargetPool) int {
		return cmp.Compare(*a.TargetPort, *b.TargetPort)
	})

	return &albsdk.CreateLoadBalancerPayload{
		DisableTargetSecurityGroupAssignment: new(true), // TODO: Make this configurable via flag
		Name:                                 new(fmt.Sprintf("k8s-ingress-%s", t.ingressClass.UID)),
		Labels: &map[string]string{
			"ingress-class-uid": string(t.ingressClass.UID),
		},
		// TODO: Support static IP and promotion but not demotion
		Listeners: listeners,
		Networks: []albsdk.Network{
			{
				NetworkId: new(networkID),
				Role:      new("ROLE_LISTENERS_AND_TARGETS"),
			},
		},
		Options: &albsdk.LoadBalancerOptions{
			EphemeralAddress: new(true),
			// TODO:
		},
		PlanId:      &t.planId,
		Region:      new(region),
		TargetPools: targetPools,
	}
}

// ToUpdatePayload creates the payload to update a load balancer from the working tree.
// It requires that existingALB was not nil when BuildTree was called.
// certificateIDMap must contain all certificates that exist in the API for this ALB.
// However, not all secrets must exist.
//
// The output is deterministic.
func (t WorkingTreeALB) ToUpdatePayload(
	certificateIDMap map[CertificateFingerprint]string,
	networkID string,
	region string,
) *albsdk.UpdateLoadBalancerPayload {
	create := t.ToCreatePayload(certificateIDMap, networkID, region)
	update := new(albsdk.UpdateLoadBalancerPayload(*create))
	// TODO: Take observability log config from existing LB.
	update.Version = t.existingALB.Version
	return update
}
