package ingress

import (
	"context"
	"errors"
	"fmt"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
	"k8s.io/utils/ptr"
)

func (r *IngressClassReconciler) applyALB(ctx context.Context, alb *albsdk.CreateLoadBalancerPayload) error {
	responseAlb, err := r.ALBClient.GetLoadBalancer(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region, *alb.Name)
	if err != nil {
		if errors.Is(err, stackit.ErrorNotFound) {
			_, err := r.ALBClient.CreateLoadBalancer(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region, alb)
			if err != nil {
				return fmt.Errorf("failed to create load balancer: %w", err)
			}
			return nil
		}
	}

	if !updateNeeded(responseAlb, alb) {
		return nil
	}

	updateAlb := albsdk.UpdateLoadBalancerPayload{
		DisableTargetSecurityGroupAssignment: alb.DisableTargetSecurityGroupAssignment,
		Errors:                               alb.Errors,
		ExternalAddress:                      alb.ExternalAddress,
		Labels:                               alb.Labels,
		Listeners:                            alb.Listeners,
		LoadBalancerSecurityGroup:            alb.LoadBalancerSecurityGroup,
		Name:                                 alb.Name,
		Networks:                             alb.Networks,
		Options:                              alb.Options,
		PlanId:                               alb.PlanId,
		PrivateAddress:                       alb.PrivateAddress,
		Region:                               alb.Region,
		Status:                               alb.Status,
		TargetPools:                          alb.TargetPools,
		TargetSecurityGroup:                  alb.TargetSecurityGroup,
		AdditionalProperties:                 alb.AdditionalProperties,
		Version:                              responseAlb.Version,
	}

	_, err = r.ALBClient.UpdateLoadBalancer(ctx, r.ALBConfig.Global.ProjectID, r.ALBConfig.Global.Region, *updateAlb.Name, &updateAlb)
	if err != nil {
		return fmt.Errorf("failed to update load balancer: %w", err)
	}

	return nil
}

// detectChange checks if there is any difference between the current and desired ALB configuration.
func updateNeeded(alb *albsdk.LoadBalancer, albPayload *albsdk.CreateLoadBalancerPayload) bool { //nolint:gocyclo,funlen // We check a lot of fields. Not much complexity.
	if len(alb.Listeners) != len(albPayload.Listeners) {
		return true
	}

	for i := range alb.Listeners {
		listener := (alb.Listeners)[i]
		payloadListener := (albPayload.Listeners)[i]

		if ptr.Deref(listener.Protocol, "") != ptr.Deref(payloadListener.Protocol, "") ||
			ptr.Deref(listener.Port, 0) != ptr.Deref(payloadListener.Port, 0) {
			return true
		}

		// WAF config check
		if ptr.Deref(listener.WafConfigName, "") != ptr.Deref(payloadListener.WafConfigName, "") {
			return true
		}

		// HTTP rules comparison (via Hosts)
		if listener.Http != nil && payloadListener.Http != nil {
			albHosts := listener.Http.Hosts
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
		} else if listener.Http != nil || payloadListener.Http != nil {
			// One is nil, one isn't
			return true
		}

		// HTTPS certificate comparison
		if listener.Https != nil && payloadListener.Https != nil {
			a := listener.Https.CertificateConfig
			b := payloadListener.Https.CertificateConfig
			if len(a.CertificateIds) != len(b.CertificateIds) {
				return true
			}
		} else if listener.Https != nil || payloadListener.Https != nil {
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
