/*
Copyright 2024 The Kubernetes Authors.

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

package ccm

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/labels"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"

	corev1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

const (
	RegionalProviderIDEnv = "OS_CCM_REGIONAL"
	// TODO: update the state with a more definitive one from the IaaS.
	instanceStopping = "STOPPING"
)

// If makeInstanceID is changed, the regexp should be changed too.
var providerIDRegexp = regexp.MustCompile(`^` + ProviderName + `://([^/]+)$`)

// TODO: remove old provider after migration
var oldProviderIDRegexp = regexp.MustCompile(`^` + oldProviderName + `://([^/]*)/([^/]+)$`)

// Instances encapsulates an implementation of Instances for OpenStack.
type Instances struct {
	regionProviderID bool
	iaasClient       stackit.NodeClient
	projectID        string
	region           string
}

func NewInstance(client stackit.NodeClient, projectID, region string) (*Instances, error) {
	return &Instances{
		iaasClient:       client,
		projectID:        projectID,
		region:           region,
		regionProviderID: false,
	}, nil
}

// InstanceExists indicates whether a given node exists according to the cloud provider
func (i *Instances) InstanceExists(ctx context.Context, node *corev1.Node) (bool, error) {
	_, err := i.getInstance(ctx, node)
	if errors.Is(err, cloudprovider.InstanceNotFound) {
		klog.V(6).Infof("instance not found for node: %s", node.Name)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get instance: %w", err)
	}

	return true, nil
}

// InstanceShutdown returns true if the instance is shutdown according to the cloud provider.
func (i *Instances) InstanceShutdown(ctx context.Context, node *corev1.Node) (bool, error) {
	server, err := i.getInstance(ctx, node)
	if err != nil {
		return false, fmt.Errorf("failed to get instance: %w", err)
	}

	// SHUTOFF is the only state where we can detach volumes immediately
	if *server.Status == instanceStopping {
		return true, nil
	}

	return false, nil
}

// InstanceMetadata returns the instance's metadata.
func (i *Instances) InstanceMetadata(ctx context.Context, node *corev1.Node) (*cloudprovider.InstanceMetadata, error) {
	server, err := i.getInstance(ctx, node)
	if errors.Is(err, cloudprovider.InstanceNotFound) {
		klog.V(6).Infof("instance not found for node: %s", node.Name)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	var addresses []corev1.NodeAddress
	if len(server.GetNics()) == 0 {
		return nil, fmt.Errorf("server has no network interfaces")
	}
	for _, nic := range server.GetNics() {
		if ipv4, ok := nic.GetIpv4Ok(); ok {
			addToNodeAddresses(&addresses,
				corev1.NodeAddress{
					Address: ipv4,
					Type:    corev1.NodeInternalIP,
				})
		}

		// TODO: where to find IPv6SupportDisabled
		if ipv6, ok := nic.GetIpv6Ok(); ok {
			addToNodeAddresses(&addresses,
				corev1.NodeAddress{
					Address: ipv6,
					Type:    corev1.NodeInternalIP,
				})
		}

		if publicIP, ok := nic.GetPublicIpOk(); ok {
			addToNodeAddresses(&addresses,
				corev1.NodeAddress{
					Address: publicIP,
					Type:    corev1.NodeExternalIP,
				})
		}
	}

	addToNodeAddresses(&addresses,
		corev1.NodeAddress{
			Type:    corev1.NodeHostName,
			Address: server.GetName(),
		})

	availabilityZone := labels.Sanitize(server.GetAvailabilityZone())

	return &cloudprovider.InstanceMetadata{
		ProviderID:    i.makeInstanceID(server),
		InstanceType:  server.GetMachineType(),
		NodeAddresses: addresses,
		Zone:          availabilityZone,
		Region:        i.region,
	}, nil
}

func (i *Instances) makeInstanceID(server *iaas.Server) string {
	return fmt.Sprintf("%s://%s", ProviderName, server.GetId())
}

// addToNodeAddresses appends the NodeAddresses to the passed-by-pointer slice,
// only if they do not already exist
func addToNodeAddresses(addresses *[]corev1.NodeAddress, addAddresses ...corev1.NodeAddress) {
	for _, add := range addAddresses {
		exists := false
		for _, existing := range *addresses {
			if existing.Address == add.Address && existing.Type == add.Type {
				exists = true
				break
			}
		}
		if !exists {
			*addresses = append(*addresses, add)
		}
	}
}

// instanceIDFromProviderID splits a provider's id and return instanceID.
// A providerID is build out of '${ProviderName}:///${instance-id}' which contains ':///'.
// or '${ProviderName}://${region}/${instance-id}' which contains '://'.
// See cloudprovider.GetInstanceProviderID and Instances.InstanceID.
func instanceIDFromProviderID(providerID string) (instanceID, region string, err error) {
	// https://github.com/kubernetes/kubernetes/issues/85731
	if providerID != "" && !strings.Contains(providerID, "://") {
		providerID = ProviderName + "://" + providerID
	}

	switch {
	case strings.HasPrefix(providerID, "openstack://"):
		matches := oldProviderIDRegexp.FindStringSubmatch(providerID)
		if len(matches) != 3 {
			return "", "", fmt.Errorf("ProviderID \"%s\" didn't match expected format \"%s://region/InstanceID\"", oldProviderName, providerID)
		}
		return matches[2], matches[1], nil
	case strings.HasPrefix(providerID, "stackit://"):
		matches := providerIDRegexp.FindStringSubmatch(providerID)
		if len(matches) != 2 {
			return "", "", fmt.Errorf("ProviderID \"%s\" didn't match expected format \"%s://InstanceID\"", ProviderName, providerID)
		}
		// The new stackit:// doesn't use the old regional providerID anymore and strictly follows the spec
		return matches[1], "", nil
	default:
		return "", "", fmt.Errorf("unknown ProviderName")
	}
}

func getServerByName(ctx context.Context, client stackit.NodeClient, name, projectID, region string) (*iaas.Server, error) {
	servers, err := client.ListServers(ctx, projectID, region)
	if err != nil {
		return nil, fmt.Errorf("failed to list servers: %w", err)
	}
	serverList := *servers

	if len(serverList) == 0 {
		return nil, cloudprovider.InstanceNotFound
	}

	// TODO: Implement field selector for ListServers so we don't have to do the following
	for i := range serverList {
		server := serverList[i]
		if serverName, ok := server.GetNameOk(); ok && serverName == name {
			return &server, nil
		}
	}

	return nil, cloudprovider.InstanceNotFound
}

func (i *Instances) getInstance(ctx context.Context, node *corev1.Node) (*iaas.Server, error) {
	if node.Spec.ProviderID == "" {
		return getServerByName(ctx, i.iaasClient, node.Name, i.projectID, i.region)
	}

	instanceID, instanceRegion, err := instanceIDFromProviderID(node.Spec.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance ID from Provider ID: %w", err)
	}

	if instanceRegion != "" && instanceRegion != i.region {
		return nil, fmt.Errorf("ProviderID \"%s\" didn't match supported region \"%s\"", node.Spec.ProviderID, i.region)
	}

	server, err := i.iaasClient.GetServer(ctx, i.projectID, i.region, instanceID)
	if stackit.IsNotFound(err) {
		return nil, cloudprovider.InstanceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get server: %w", err)
	}

	return server, nil
}
