package client

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gardener/gardener/extensions/pkg/controller"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	oapiError "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

var (
	// Scheme is a scheme with the types relevant for OpenStack actuators.
	Scheme *runtime.Scheme

	decoder runtime.Decoder
)

type objectWithGVK interface {
	runtime.Object
	SetGroupVersionKind(gvk schema.GroupVersionKind)
}

func isOpenAPINotFound(err error) bool {
	apiErr := &oapiError.GenericOpenAPIError{}
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusNotFound
}

// cloudProfileConfigFromCluster decodes the provider specific cloud profile configuration for a cluster
func cloudProfileConfigFromCluster(cluster *controller.Cluster) (*stackitv1alpha1.CloudProfileConfig, error) {
	cloudProfileConfig := &stackitv1alpha1.CloudProfileConfig{}
	setGVK(cloudProfileConfig)

	if cluster == nil || cluster.CloudProfile == nil {
		return cloudProfileConfig, nil
	}

	cloudProfileSpecifier := fmt.Sprintf("cloudProfile '%q'", k8sclient.ObjectKeyFromObject(cluster.CloudProfile))
	if cluster.Shoot != nil && cluster.Shoot.Spec.CloudProfile != nil {
		cloudProfileSpecifier = fmt.Sprintf("%s '%s/%s'", cluster.Shoot.Spec.CloudProfile.Kind, cluster.Shoot.Namespace, cluster.Shoot.Spec.CloudProfile.Name)
	}

	if err := decode(cluster.CloudProfile.Spec.ProviderConfig, cloudProfileConfig); err != nil {
		return nil, fmt.Errorf("could not decode providerConfig of %s: %w", cloudProfileSpecifier, err)
	}
	return cloudProfileConfig, nil
}

// setGVK sets the type meta based on the scheme. We do this to ensure that we always have a valid type meta (apiVersion
// + kind) when returning the object.
func setGVK(obj objectWithGVK) {
	gkv, err := apiutil.GVKForObject(obj, Scheme)
	if err != nil {
		panic(fmt.Errorf("could not get kinds from schema: %w", err))
	}
	obj.SetGroupVersionKind(gkv)
}

func decode(raw *runtime.RawExtension, into objectWithGVK) error {
	return decodeWith(decoder, raw, nil, into)
}

// decodeWith decodes the given raw extension into the given object using the given decoder. After decoding, it ensures
// that the type meta is set correctly.
func decodeWith(dec runtime.Decoder, raw *runtime.RawExtension, defaults *schema.GroupVersionKind, into objectWithGVK) error {
	var data []byte
	if raw != nil {
		data = raw.Raw
	}
	_, gkv, err := dec.Decode(data, defaults, into)
	if err != nil {
		return err
	}
	if gkv != nil {
		into.SetGroupVersionKind(*gkv)
	}
	return nil
}

func filterVolumes(volumes []iaas.Volume, filters map[string]string) []iaas.Volume {
	filteredVolumes := make([]iaas.Volume, 0)

	if filters == nil {
		return volumes
	}

	for i := range volumes {
		volume := &volumes[i]
		if val, ok := filters["Name"]; ok && val != volume.GetName() {
			continue
		}
		filteredVolumes = append(filteredVolumes, *volume)
	}

	return filteredVolumes
}

//nolint:dupl // We don't feel like doing generics to undupe this.
func filterSnapshots(snapshots []iaas.Snapshot, filters map[string]string) []iaas.Snapshot {
	filteredSnapshots := make([]iaas.Snapshot, 0)

	if filters == nil {
		return snapshots
	}

	for _, obj := range snapshots {
		if val, ok := filters["Status"]; ok && val != obj.GetStatus() {
			continue
		}
		if val, ok := filters["VolumeID"]; ok && val != obj.GetVolumeId() {
			continue
		}
		if val, ok := filters["Name"]; ok && val != obj.GetName() {
			continue
		}
		filteredSnapshots = append(filteredSnapshots, obj)
	}

	return filteredSnapshots
}

//nolint:dupl // We don't feel like doing generics to undupe this.
func filterBackups(backups []iaas.Backup, filters map[string]string) []iaas.Backup {
	filteredBackups := make([]iaas.Backup, 0)

	if filters == nil {
		return backups
	}

	for _, obj := range backups {
		if val, ok := filters["Status"]; ok && val != obj.GetStatus() {
			continue
		}
		if val, ok := filters["VolumeID"]; ok && val != obj.GetVolumeId() {
			continue
		}
		if val, ok := filters["Name"]; ok && val != obj.GetName() {
			continue
		}
		filteredBackups = append(filteredBackups, obj)
	}

	return filteredBackups
}

func IsNotFound(err error) bool {
	return errors.Is(err, ErrorNotFound)
}