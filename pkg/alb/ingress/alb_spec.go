package ingress

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/labels"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/reference"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
	certsdk "github.com/stackitcloud/stackit-sdk-go/services/certificates/v2api"
)

func (r *IngressClassReconciler) getAlbSpecForIngressClass(
	ctx context.Context,
	class *networkingv1.IngressClass,
) (payload *albsdk.CreateLoadBalancerPayload, activeCertIDs map[string]string, validationErrors []error, err error) {
	ingresses, err := r.getSortedIngressesForIngressClass(ctx, class)
	if err != nil {
		return nil, nil, nil, err
	}

	return r.getAlbSpecForIngresses(ctx, class, ingresses)
}

func (r *IngressClassReconciler) getAlbSpecForIngresses(
	ctx context.Context,
	class *networkingv1.IngressClass,
	ingresses []networkingv1.Ingress,
) (payload *albsdk.CreateLoadBalancerPayload, activeCertIDs map[string]string, validationErrors []error, err error) {
	errorList := []error{}

	listeners := albListeners{}
	certificates := albCertificates{}
	targetPools := albTargetPools{}

	for i, ingress := range ingresses {
		var listenerMergeError, targetPoolMergeError []error
		ingressListeners, ingressCertificates, ingressTargetPools, ingressErrorList := r.getALBResourcesForIngress(ctx, class, &ingress, i)
		errorList = append(errorList, ingressErrorList...)

		certificates = mergeCertificates(certificates, ingressCertificates)
		targetPools, targetPoolMergeError = mergeTargetPools(targetPools, ingressTargetPools)
		errorList = append(errorList, targetPoolMergeError...)
		listeners, listenerMergeError = mergeListeners(listeners, ingressListeners)
		errorList = append(errorList, listenerMergeError...)
	}

	certNameToId, certificateerrorEvent := r.applyCertificates(ctx, class, certificates)
	errorList = append(errorList, certificateerrorEvent...)

	alb, albSpecErrorList, err := r.getAlbSpecForResources(ctx, class, listeners, targetPools, certNameToId)
	errorList = append(errorList, albSpecErrorList...)
	return alb, certNameToId, errorList, err
}

func (r *IngressClassReconciler) getAlbSpecForResources(
	ctx context.Context,
	class *networkingv1.IngressClass,
	listeners albListeners,
	targetPools albTargetPools,
	certNameToId map[string]string,
) (payload *albsdk.CreateLoadBalancerPayload, validationErrors []error, err error) {
	errorList := []error{}

	alb := &albsdk.CreateLoadBalancerPayload{
		Options: &albsdk.LoadBalancerOptions{},
		Networks: []albsdk.Network{
			{
				NetworkId: new(r.ALBConfig.ApplicationLoadBalancer.NetworkID),
				Role:      new("ROLE_LISTENERS_AND_TARGETS"),
			},
		},
		Name:                                 new(string(class.UID)),
		DisableTargetSecurityGroupAssignment: new(true),
	}

	externalAddress := getAnnotation(AnnotationExternalIP, "", class)
	if externalAddress != "" {
		alb.ExternalAddress = &externalAddress
	} else {
		alb.Options.EphemeralAddress = new(true)
	}

	if getAnnotation(AnnotationInternal, false, class) {
		alb.Options.PrivateNetworkOnly = new(true)
		alb.Options.EphemeralAddress = new(false)
	}

	if plan := getAnnotation(AnnotationPlanID, "", class); plan != "" {
		alb.PlanId = &plan
	}

	// add ownership labels
	ownershipLabels := map[string]string{
		labels.LabelIngressClassUID: string(class.UID),
	}
	alb.Labels = &ownershipLabels

	for port, listener := range listeners {
		albsdkListener := albsdk.Listener{
			Http:                 nil,
			Name:                 new(listener.name),
			Port:                 new(int32(port)),
			Protocol:             new(listener.protocol),
			AdditionalProperties: nil,
		}

		if listener.wafConfigName != "" {
			albsdkListener.WafConfigName = new(listener.wafConfigName)
		}

		albsdkHosts := []albsdk.HostConfig{}
		for host, hostPaths := range listener.hosts {
			albsdkHost := albsdk.HostConfig{
				Host: new(host),
			}
			for path, rule := range hostPaths.path {
				albsdkRule := albsdk.Rule{
					TargetPool: new(rule.targetPoolName),
					WebSocket:  new(rule.websocket),
				}

				if rule.cookiePersistenceName != "" {
					albsdkRule.CookiePersistence = new(albsdk.CookiePersistence{
						Name: new(rule.cookiePersistenceName),
						Ttl:  new(fmt.Sprintf("%ds", rule.cookiePersistenceTtlSeconds)),
					})
				}

				switch rule.pathTyp {
				case networkingv1.PathTypeExact:
					albsdkRule.Path = new(albsdk.Path{
						ExactMatch: new(path),
					})
				default:
					albsdkRule.Path = new(albsdk.Path{
						Prefix: new(path),
					})
				}

				albsdkHost.Rules = append(albsdkHost.Rules, albsdkRule)
			}

			/// menekse

			sort.SliceStable(albsdkHost.Rules, func(i, j int) bool {
				pathI := ""
				if albsdkHost.Rules[i].Path.Prefix != nil {
					pathI = *albsdkHost.Rules[i].Path.Prefix
				} else if albsdkHost.Rules[i].Path.ExactMatch != nil {
					pathI = *albsdkHost.Rules[i].Path.ExactMatch
				}

				pathJ := ""
				if albsdkHost.Rules[j].Path.Prefix != nil {
					pathJ = *albsdkHost.Rules[j].Path.Prefix
				} else if albsdkHost.Rules[j].Path.ExactMatch != nil {
					pathJ = *albsdkHost.Rules[j].Path.ExactMatch
				}

				ruleI := hostPaths.path[pathI]
				ruleJ := hostPaths.path[pathJ]

				// Smaller sequence index means it was processed earlier (higher priority)
				return ruleI.sequenceIndex < ruleJ.sequenceIndex
			})

			////

			albsdkHosts = append(albsdkHosts, albsdkHost)

			albsdkListener.Http = new(albsdk.ProtocolOptionsHTTP{
				Hosts: albsdkHosts,
			})
		}

		if listener.protocol == "PROTOCOL_HTTPS" {
			albsdkListener.Https = new(albsdk.ProtocolOptionsHTTPS{
				CertificateConfig: new(albsdk.CertificateConfig{
					CertificateIds: []string{},
				}),
			})
			for _, certificateID := range listener.certificateIDs {
				certUUID, ok := certNameToId[certificateID]
				if !ok {
					continue
				}
				albsdkListener.Https.CertificateConfig.CertificateIds = append(albsdkListener.Https.CertificateConfig.CertificateIds, certUUID)
			}

		}
		if listener.protocol == "PROTOCOL_HTTPS" && len(albsdkListener.Https.CertificateConfig.CertificateIds) == 0 {
			errorList = append(errorList, &errorEvent{
				ingressRef:  listener.ingressRef,
				description: "Certificate not found for protocol HTTPS",
				typ:         "Error",
			})
			continue
		}
		alb.Listeners = append(alb.Listeners, albsdkListener)
	}

	targets, err := r.getTargetsOfNodes(ctx)
	if err != nil {
		return nil, errorList, err
	}

	for name, targetPool := range targetPools {
		albsdkTargetPool := albsdk.TargetPool{
			Name:              new(name),
			TargetPort:        new(targetPool.port),
			Targets:           targets,
			ActiveHealthCheck: nil, // TODO
		}

		if targetPool.tlsEnabled {
			albsdkTargetPool.TlsConfig = &albsdk.TlsConfig{
				Enabled:                   new(bool),
				SkipCertificateValidation: nil,
				CustomCa:                  nil,
			}
			*albsdkTargetPool.TlsConfig.Enabled = true

			if targetPool.skipCertificateValidation {
				albsdkTargetPool.TlsConfig.SkipCertificateValidation = new(bool)
				*albsdkTargetPool.TlsConfig.SkipCertificateValidation = true
			}

			if targetPool.customCA != "" {
				albsdkTargetPool.TlsConfig.CustomCa = new(string)
				*albsdkTargetPool.TlsConfig.CustomCa = targetPool.customCA
			}
		}

		alb.TargetPools = append(alb.TargetPools, albsdkTargetPool)
	}

	return alb, errorList, nil
}

func (r *IngressClassReconciler) getALBResourcesForIngress(ctx context.Context, class *networkingv1.IngressClass, ingress *networkingv1.Ingress, sequenceIndex int) (albListeners, albCertificates, albTargetPools, []error) {
	ref := getIngressRefForIngress(r.Scheme, ingress)

	certificates := albCertificates{}
	certificateIDs := []string{}
	httpsHosts := map[string]struct{}{}
	errorList := []error{}

	for _, tls := range ingress.Spec.TLS {
		for _, host := range tls.Hosts {
			httpsHosts[host] = struct{}{}
			name, cert, err := r.getCertificateForSecretName(ctx, class, ingress, tls.SecretName)
			if err != nil {
				errorList = append(errorList, &errorEvent{
					ingressRef:  ref,
					typ:         "error",
					description: err.Error(),
				})
				continue
			}
			certificateIDs = append(certificateIDs, name)
			mergeCertificates(certificates, albCertificates{name: cert})
		}
	}

	hosts := map[string]albListenerHost{}
	targets := albTargetPools{}

	for _, rule := range ingress.Spec.Rules {
		if _, ok := hosts[rule.Host]; !ok {
			hosts[rule.Host] = albListenerHost{
				ingressRef: ref,
				path:       map[string]albListenerRule{},
			}
		}

		for _, path := range rule.HTTP.Paths {
			if _, ok := hosts[rule.Host].path[path.Path]; ok {
				errorList = append(errorList, &errorEvent{
					ingressRef:  ref,
					typ:         "error",
					description: fmt.Sprintf("path %q already exists within same ingress", path.Path),
				})
				continue
			}

			poolName, targetPool, err := r.getTargetPoolForPath(ctx, class, ingress, path)
			if err != nil {
				errorList = append(errorList, &errorEvent{
					ingressRef:  ref,
					typ:         "error",
					description: err.Error(),
				})
				continue
			}
			var tagetMergeErrors []error
			targets, tagetMergeErrors = mergeTargetPools(targets, albTargetPools{poolName: targetPool})
			errorList = append(errorList, tagetMergeErrors...)

			hosts[rule.Host].path[path.Path] = albListenerRule{
				ingressRef:                  ref,
				pathTyp:                     ptr.Deref(path.PathType, networkingv1.PathTypePrefix),
				cookiePersistenceName:       getAnnotation(AnnotationCookiePersistenceName, "", class, ingress),
				cookiePersistenceTtlSeconds: getAnnotation(AnnotationCookiePersistenceTTLSeconds, 0, class, ingress),
				targetPoolName:              poolName,
				websocket:                   getAnnotation(AnnotationWebSocket, false, class, ingress),
				sequenceIndex:               sequenceIndex,
			}
		}

	}

	httpPort := getAnnotation(AnnotationHTTPPort, 80, ingress, class)
	httpsPort := getAnnotation(AnnotationHTTPSPort, 443, ingress, class)

	httpListener := albListener{
		ingressRef:    ref,
		protocol:      "PROTOCOL_HTTP",
		name:          fmt.Sprintf("%d-http", httpPort),
		wafConfigName: getAnnotation(AnnotationWAFName, "", class, ingress),
		hosts:         map[string]albListenerHost{},
	}
	httpsListener := albListener{
		ingressRef:     ref,
		protocol:       "PROTOCOL_HTTPS",
		name:           fmt.Sprintf("%d-https", httpsPort),
		wafConfigName:  getAnnotation(AnnotationWAFName, "", class, ingress),
		certificateIDs: certificateIDs,
		hosts:          map[string]albListenerHost{},
	}

	for host, rules := range hosts {
		if !getAnnotation(AnnotationHTTPSOnly, false, class, ingress) {
			httpListener.hosts[host] = rules
		}
		if _, ok := httpsHosts[host]; ok {
			httpsListener.hosts[host] = rules
		}
	}

	listeners := albListeners{}
	if len(httpListener.hosts) > 0 {
		listeners[httpPort] = httpListener
	}
	if len(httpsListener.hosts) > 0 && len(httpsListener.certificateIDs) > 0 {
		listeners[httpsPort] = httpsListener
	}

	return listeners, certificates, targets, errorList
}

func (r *IngressClassReconciler) getSortedIngressesForIngressClass(ctx context.Context, class *networkingv1.IngressClass) ([]networkingv1.Ingress, error) {
	ingresses, err := r.getIngressesForIngressClass(ctx, class)
	if err != nil {
		return nil, err
	}

	sort.SliceStable(ingresses, func(i, j int) bool {
		prioI := getAnnotation(AnnotationPriority, 0, class, &ingresses[i])
		prioJ := getAnnotation(AnnotationPriority, 0, class, &ingresses[j])

		// Sort by Priority (Highest at the beginning)
		if prioI != prioJ {
			return prioI > prioJ
		}

		// Sort by CreationTimestamp (Oldest first) if prio is the same
		return ingresses[i].CreationTimestamp.Before(&ingresses[j].CreationTimestamp)
	})

	return ingresses, nil
}

func (r *IngressClassReconciler) getIngressesForIngressClass(ctx context.Context, class *networkingv1.IngressClass) ([]networkingv1.Ingress, error) {
	ingressList := &networkingv1.IngressList{}
	err := r.Client.List(ctx, ingressList)
	if err != nil {
		return nil, err
	}

	var ingresses []networkingv1.Ingress

	for _, ingress := range ingressList.Items {
		if ptr.Deref(ingress.Spec.IngressClassName, "") == class.Name {
			ingresses = append(ingresses, ingress)
		}
	}
	return ingresses, nil
}

func (r *IngressClassReconciler) getTargetPoolForPath(ctx context.Context, class *networkingv1.IngressClass, ingress *networkingv1.Ingress, path networkingv1.HTTPIngressPath) (string, albTargetPool, error) {
	if path.Backend.Service == nil {
		return "", albTargetPool{}, fmt.Errorf("ingress %q does not have a service backend", ingress.Name)
	}

	svc := &corev1.Service{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      path.Backend.Service.Name,
		Namespace: ingress.Namespace,
	}, svc)
	if err != nil {
		return "", albTargetPool{}, err
	}
	if svc.Spec.Type != corev1.ServiceTypeNodePort {
		return "", albTargetPool{}, errors.New("service type is not NodePort")
	}

	for _, port := range svc.Spec.Ports {
		if port.Port == path.Backend.Service.Port.Number ||
			port.Name == path.Backend.Service.Port.Name {
			return fmt.Sprintf("port-%d", port.NodePort), albTargetPool{
				ingressRef:                getIngressRefForIngress(r.Scheme, ingress),
				port:                      port.NodePort,
				targets:                   nil,
				tlsEnabled:                getAnnotation(AnnotationTargetPoolTLSEnabled, false, ingress, class, svc),
				skipCertificateValidation: getAnnotation(AnnotationTargetPoolTLSSkipCertificateValidation, false, ingress, class, svc),
				customCA:                  getAnnotation(AnnotationTargetPoolTLSCustomCa, "", ingress, class, svc),
			}, nil
		}
	}

	return "", albTargetPool{}, errors.New("no matching port in service found")
}

func (r *IngressClassReconciler) getCertificateForSecretName(ctx context.Context, class *networkingv1.IngressClass, ingress *networkingv1.Ingress, secretName string) (string, albCertificate, error) {
	secret := &corev1.Secret{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      secretName,
		Namespace: ingress.Namespace,
	}, secret)
	if err != nil {
		return "", albCertificate{}, err
	}
	if secret.Type != corev1.SecretTypeTLS {
		return "", albCertificate{}, errors.New("secret type is not kubernetes.io/tls")
	}

	return getCertName(class, secret), albCertificate{
		ingressRefs: []corev1.ObjectReference{getIngressRefForIngress(r.Scheme, ingress)},
		publicKey:   secret.Data[corev1.TLSCertKey],
		privateKey:  secret.Data[corev1.TLSPrivateKeyKey],
	}, nil
}

func (r *IngressClassReconciler) applyCertificates(ctx context.Context, class *networkingv1.IngressClass, certificates albCertificates) (map[string]string, []error) {
	errorList := []error{}
	nameToID := map[string]string{}
	for name, certificate := range certificates {
		createCertificatePayload := &certsdk.CreateCertificatePayload{
			Name:       new(name),
			ProjectId:  &r.ALBConfig.Global.ProjectID,
			PrivateKey: new(string(certificate.privateKey)),
			PublicKey:  new(string(certificate.publicKey)),
			Labels: &map[string]string{
				labels.LabelIngressClassUID: string(class.UID),
			},
		}
		response, err := r.CertificateClient.CreateCertificate(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region, createCertificatePayload)
		if err != nil {
			for _, ref := range certificate.ingressRefs {
				errorList = append(errorList, &errorEvent{
					ingressRef:  ref,
					description: fmt.Sprintf("Error creating certificate for ingress %q: %s", ref, err),
					typ:         "error",
				})
			}
			continue
		}
		nameToID[response.GetName()] = response.GetId()
	}
	return nameToID, errorList
}

func (r *IngressClassReconciler) getTargetsOfNodes(ctx context.Context) ([]albsdk.Target, error) {
	nodes := &corev1.NodeList{}
	err := r.Client.List(ctx, nodes)
	if err != nil {
		return nil, err
	}

	targets := []albsdk.Target{}
	for _, node := range nodes.Items {
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
	return targets, nil
}

func getIngressRefForIngress(scheme *runtime.Scheme, ingress *networkingv1.Ingress) corev1.ObjectReference {
	ref, err := reference.GetReference(scheme, ingress)
	if err != nil {
		return corev1.ObjectReference{}
	}
	return *ref
}
