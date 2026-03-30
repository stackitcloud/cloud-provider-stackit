package ingress

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

type albListeners map[int]albListener

type albListener struct {
	ingressRef     corev1.ObjectReference
	hosts          map[string]albListenerHost
	protocol       string
	name           string
	wafConfigName  string
	certificateIDs []string
}

type albListenerHost struct {
	ingressRef corev1.ObjectReference
	path       map[string]albListenerRule
}
type albListenerRule struct {
	ingressRef                  corev1.ObjectReference
	cookiePersistenceName       string
	cookiePersistenceTtlSeconds int
	pathTyp                     networkingv1.PathType
	targetPoolName              string
	websocket                   bool
}

type albTargetPools map[string]albTargetPool

type albTargetPool struct {
	ingressRef                corev1.ObjectReference
	port                      int32
	tlsEnabled                bool
	customCA                  string
	skipCertificateValidation bool
	targets                   []albTarget
}

type albTarget struct {
	name string
	ip   string
}

type albCertificates map[string]albCertificate

type albCertificate struct {
	ingressRefs []corev1.ObjectReference
	privateKey  []byte
	publicKey   []byte
}

// mergeTargetPools returns a compiled list of albTargetPools.
// It will not return an error if something is already there as the pools are uniq due to the nodePort.
func mergeTargetPools(dst, src albTargetPools) (albTargetPools, []errorEvents) {
	var mergeErrors []errorEvents

	for name, srcTargetPool := range src {
		dstTargetPool, ok := dst[name]
		if !ok {
			if len(dst) >= 20 {
				mergeErrors = append(mergeErrors, errorEvents{
					ingressRef:  srcTargetPool.ingressRef,
					description: fmt.Sprintf("Target pool for %s could not be created due to maximum already reached", name),
				})
				continue
			}

			dst[name] = srcTargetPool
			continue
		}
		if dstTargetPool.skipCertificateValidation != srcTargetPool.skipCertificateValidation {
			mergeErrors = append(mergeErrors, errorEvents{
				ingressRef:  srcTargetPool.ingressRef,
				description: fmt.Sprintf("%s annotation ignored as it already is configred differently in ingress %v", AnnotationTargetPoolTLSSkipCertificateValidation, dstTargetPool.ingressRef),
				typ:         "Warning",
			})
		}
		if dstTargetPool.tlsEnabled != srcTargetPool.tlsEnabled {
			mergeErrors = append(mergeErrors, errorEvents{
				ingressRef:  srcTargetPool.ingressRef,
				description: fmt.Sprintf("%s annotation ignored as it already is configred differently in ingress %v", AnnotationTargetPoolTLSEnabled, dstTargetPool.ingressRef),
				typ:         "Warning",
			})
		}
		if dstTargetPool.customCA != srcTargetPool.customCA {
			mergeErrors = append(mergeErrors, errorEvents{
				ingressRef:  srcTargetPool.ingressRef,
				description: fmt.Sprintf("%s annotation ignored as it already is configred differently in ingress %v", AnnotationTargetPoolTLSCustomCa, dstTargetPool.ingressRef),
				typ:         "Warning",
			})
		}
	}

	return dst, mergeErrors
}

// mergeCertificates returns a compiled list of albCertificates.
// It will not return an error if something is already there as the pools are uniq due to the nodePort.
func mergeCertificates(dst, src albCertificates) albCertificates {
	for id, srcCertificate := range src {
		dstCertificate, ok := dst[id]
		if !ok {
			dst[id] = srcCertificate
			continue
		}
		dstCertificate.ingressRefs = mergeIngressRefs(dstCertificate.ingressRefs, srcCertificate.ingressRefs)
		dst[id] = dstCertificate
	}
	return dst
}

func mergeListeners(dst, src albListeners) (albListeners, []errorEvents) {
	var mergeErrors []errorEvents

	for port, srcListener := range src {
		dstListener, ok := dst[port]
		if !ok {
			dst[port] = srcListener
			continue
		}
		if dstListener.protocol != srcListener.protocol {
			mergeErrors = append(mergeErrors, errorEvents{
				ingressRef: srcListener.ingressRef,
				typ:        "warning",
				description: fmt.Sprintf(
					"Could not use protocol %s for port %d as this is already configured in %s in namespace %s",
					srcListener.protocol, port, dstListener.ingressRef.Name, dstListener.ingressRef.Namespace,
				),
			})
		}
		if dstListener.wafConfigName != srcListener.wafConfigName {
			mergeErrors = append(mergeErrors, errorEvents{
				ingressRef: srcListener.ingressRef,
				typ:        "warning",
				description: fmt.Sprintf(
					"Could not use wafconfig %s for port %d as this is already configured in %s in namespace %s",
					srcListener.wafConfigName, port, dstListener.ingressRef.Name, dstListener.ingressRef.Namespace,
				)})
		}

		var hostsMergeError []errorEvents
		dstListener.hosts, hostsMergeError = mergeListenerHosts(dstListener.hosts, srcListener.hosts)
		mergeErrors = append(mergeErrors, hostsMergeError...)

		dstListener.certificateIDs = mergeListenerCertificateIDs(dst[port].certificateIDs, srcListener.certificateIDs)
		dst[port] = dstListener
	}
	return dst, mergeErrors
}

func mergeListenerCertificateIDs(ids1, ids2 []string) []string {
	ids := append(ids1, ids2...)
	slices.Sort(ids)
	slices.Compact(ids)
	return ids
}

func mergeIngressRefs(ref1, ref2 []corev1.ObjectReference) []corev1.ObjectReference {
	ref := append(ref1, ref2...)
	slices.Compact(ref)
	return ref
}

func mergeListenerHosts(dst, src map[string]albListenerHost) (map[string]albListenerHost, []errorEvents) {
	var mergeErrors []errorEvents

	for host, srcListenerHost := range src {
		dstListenerHost, ok := dst[host]
		if !ok {
			dst[host] = srcListenerHost
			continue
		}
		var hostMergeError []errorEvents
		dstListenerHost.path, hostMergeError = mergeListenerHostPath(dstListenerHost.path, srcListenerHost.path)
		mergeErrors = append(mergeErrors, hostMergeError...)

		dst[host] = dstListenerHost
	}
	return dst, mergeErrors
}

func mergeListenerHostPath(dst, src map[string]albListenerRule) (map[string]albListenerRule, []errorEvents) {
	var mergeErrors []errorEvents

	for path, srcListenerRule := range src {
		dstListenerRule, ok := dst[path]
		if !ok {
			dst[path] = srcListenerRule
			continue
		}
		mergeErrors = append(mergeErrors, errorEvents{
			ingressRef:  srcListenerRule.ingressRef,
			typ:         "error",
			description: fmt.Sprintf("Could not apply path %q as this is already configured in %s in namespace %s", path, dstListenerRule.ingressRef.Name, dstListenerRule.ingressRef.Namespace),
		})
	}
	return dst, mergeErrors
}
