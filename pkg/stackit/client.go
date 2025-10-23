/*
Copyright 2017 The Kubernetes Authors.

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

package stackit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	oapiError "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"github.com/stackitcloud/stackit-sdk-go/services/loadbalancer"
	"gopkg.in/gcfg.v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/version"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"
)

// userAgentData is used to add extra information to the STACKIT SDK user-agent
var (
	userAgentData []string
	ErrorNotFound = errors.New("not found")
)

// AddExtraFlags is called by the main package to add component specific command line flags
func AddExtraFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&userAgentData, "user-agent", nil, "Extra data to add to STACKIT SDK user-agent. Use multiple times to add more than one component.")
}

type IaasClient interface {
	CreateVolume(context.Context, *iaas.CreateVolumePayload) (*iaas.Volume, error)
	DeleteVolume(ctx context.Context, volumeID string) error
	AttachVolume(ctx context.Context, instanceID, volumeID string) (string, error)
	ListVolumes(ctx context.Context, limit int, startingToken string) ([]iaas.Volume, string, error)
	WaitDiskAttached(ctx context.Context, instanceID string, volumeID string) error
	DetachVolume(ctx context.Context, instanceID, volumeID string) error
	WaitDiskDetached(ctx context.Context, instanceID string, volumeID string) error
	WaitVolumeTargetStatus(ctx context.Context, volumeID string, tStatus []string) error
	GetVolume(ctx context.Context, volumeID string) (*iaas.Volume, error)
	GetVolumesByName(ctx context.Context, name string) ([]iaas.Volume, error)
	GetVolumeByName(ctx context.Context, name string) (*iaas.Volume, error)
	CreateSnapshot(ctx context.Context, name, volID string, tags map[string]string) (*iaas.Snapshot, error)
	ListSnapshots(ctx context.Context, filters map[string]string) ([]iaas.Snapshot, string, error)
	DeleteSnapshot(ctx context.Context, snapID string) error
	GetSnapshotByID(ctx context.Context, snapshotID string) (*iaas.Snapshot, error)
	WaitSnapshotReady(ctx context.Context, snapshotID string) (*string, error)
	CreateBackup(ctx context.Context, name, volID, snapshotID string, tags map[string]string) (*iaas.Backup, error)
	ListBackups(ctx context.Context, filters map[string]string) ([]iaas.Backup, error)
	DeleteBackup(ctx context.Context, backupID string) error
	GetBackupByID(ctx context.Context, backupID string) (*iaas.Backup, error)
	WaitBackupReady(ctx context.Context, backupID string, snapshotSize int64, backupMaxDurationSecondsPerGB int) (*string, error)
	GetInstanceByID(ctx context.Context, instanceID string) (*iaas.Server, error)
	ExpandVolume(ctx context.Context, volumeID string, status string, size int64) error
	GetBlockStorageOpts() BlockStorageOpts
	WaitVolumeTargetStatusWithCustomBackoff(ctx context.Context, volumeID string, targetStatus []string, backoff *wait.Backoff) error
}

type LoadbalancerClient interface {
	GetLoadBalancer(ctx context.Context, projectID string, name string) (*loadbalancer.LoadBalancer, error)
	// DeleteLoadBalancer returns no error if the load balancer doesn't exist.
	DeleteLoadBalancer(ctx context.Context, projectID string, name string) error
	// CreateLoadBalancer returns ErrorNotFound if the project is not enabled.
	CreateLoadBalancer(ctx context.Context, projectID string, loadbalancer *loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	UpdateLoadBalancer(ctx context.Context, projectID, name string, update *loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	UpdateTargetPool(ctx context.Context, projectID string, name string, targetPoolName string, payload loadbalancer.UpdateTargetPoolPayload) error
	CreateCredentials(ctx context.Context, projectID string, payload loadbalancer.CreateCredentialsPayload) (*loadbalancer.CreateCredentialsResponse, error)
	ListCredentials(ctx context.Context, projectID string) (*loadbalancer.ListCredentialsResponse, error)
	GetCredentials(ctx context.Context, projectID string, credentialRef string) (*loadbalancer.GetCredentialsResponse, error)
	UpdateCredentials(ctx context.Context, projectID, credentialRef string, payload loadbalancer.UpdateCredentialsPayload) error
	DeleteCredentials(ctx context.Context, projectID string, credentialRef string) error
}

// NodeClient is the API client wrapper for the cloud-controller-manager's node-controller
type NodeClient interface {
	GetServer(ctx context.Context, projectID, region, serverID string) (*iaas.Server, error)
	DeleteServer(ctx context.Context, projectID, region, serverID string) error
	CreateServer(ctx context.Context, projectID string, region string, create *iaas.CreateServerPayload) (*iaas.Server, error)
	UpdateServer(ctx context.Context, projectID, region, serverID string, update *iaas.UpdateServerPayload) (*iaas.Server, error)
	ListServers(ctx context.Context, projectID, region string) (*[]iaas.Server, error)
}

type iaasClient struct {
	iaas      iaas.DefaultApi
	projectID string
	region    string
	bsOpts    BlockStorageOpts
}

type lbClient struct {
	client loadbalancer.DefaultApi
	region string
}

type nodeClient struct {
	client *iaas.APIClient
}

//nolint:gocritic // The openstack package currently shadows but will be renamed anyway.
func (os *iaasClient) GetBlockStorageOpts() BlockStorageOpts {
	return os.bsOpts
}

type BlockStorageOpts struct {
	RescanOnResize bool `gcfg:"rescan-on-resize"`
}

type GlobalOpts struct {
	ProjectID  string `gcfg:"project-id"`
	IaasAPIURL string `gcfg:"iaas-api-url"`
}

type Config struct {
	Global       GlobalOpts
	Metadata     metadata.Opts
	BlockStorage BlockStorageOpts
}

func GetConfigFromFile(path string) (Config, error) {
	var cfg Config

	config, err := os.Open(path)
	if err != nil {
		klog.ErrorS(err, "Failed to open config file", "path", path)
		return cfg, err
	}
	defer config.Close()

	err = gcfg.FatalOnly(gcfg.ReadInto(&cfg, config))
	if err != nil {
		klog.ErrorS(err, "Failed to parse config file", "path", path)
		return cfg, err
	}
	return cfg, nil
}

// CreateSTACKITProvider creates STACKIT Instance
func CreateSTACKITProvider(client iaas.DefaultApi, cfg *Config) (IaasClient, error) {
	region := os.Getenv("STACKIT_REGION")
	if region == "" {
		panic("STACKIT_REGION environment variable not set")
	}
	// Init iaasClient
	instance := &iaasClient{
		iaas:      client,
		bsOpts:    cfg.BlockStorage,
		projectID: cfg.Global.ProjectID,
		region:    region,
	}

	return instance, nil
}

func CreateIaaSClient(cfg *Config) (iaas.DefaultApi, error) {
	var userAgent []string
	var opts []sdkconfig.ConfigurationOption
	userAgent = append(userAgent, fmt.Sprintf("%s/%s", "block-storage-csi-driver", version.Version))
	for _, data := range userAgentData {
		// Prepend userAgents
		userAgent = append([]string{data}, userAgent...)
	}
	klog.V(4).Infof("Using user-agent: %s", userAgent)

	if cfg.Global.IaasAPIURL != "" {
		opts = append(opts, sdkconfig.WithEndpoint(cfg.Global.IaasAPIURL))
	}

	opts = append(opts, sdkconfig.WithUserAgent(strings.Join(userAgent, " ")))

	return iaas.NewAPIClient(opts...)
}

func NewLoadbalancerClient(cl loadbalancer.DefaultApi, region string) (LoadbalancerClient, error) {
	return &lbClient{
		client: cl,
		region: region,
	}, nil
}

func NewNodeClient(cl *iaas.APIClient) (NodeClient, error) {
	return &nodeClient{client: cl}, nil
}

func isOpenAPINotFound(err error) bool {
	apiErr := &oapiError.GenericOpenAPIError{}
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusNotFound
}

func IsNotFound(err error) bool {
	return errors.Is(err, ErrorNotFound)
}
