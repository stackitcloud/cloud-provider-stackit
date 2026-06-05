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
		return fmt.Errorf("failed to get load balancer: %w", err)
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

func updateNeeded(alb *albsdk.LoadBalancer, albPayload *albsdk.CreateLoadBalancerPayload) bool {
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