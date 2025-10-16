/*
Copyright 2022 The Kubernetes Authors.

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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"

	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("Node Controller", func() {
	var (
		nodeMockClient *stackit.MockNodeClient
		instance       *Instances

		projectID string
		serverID  string
	)

	BeforeEach(func() {
		projectID = "my-project"
		serverID = "my-server"

		ctrl := gomock.NewController(GinkgoT())
		nodeMockClient = stackit.NewMockNodeClient(ctrl)

		var err error
		instance, err = NewInstance(nodeMockClient, projectID, "eu-01")
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("InstanceExists", func() {
		It("does not error if instance not found", func() {
			nodeMockClient.EXPECT().ListServers(gomock.Any(), projectID).Return(&[]iaas.Server{}, nil)

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			}

			exist, err := instance.InstanceExists(context.Background(), node)
			Expect(err).NotTo(HaveOccurred())
			Expect(exist).To(BeFalse())
		})

		It("successfully get the instance when provider ID not there", func() {
			nodeMockClient.EXPECT().ListServers(gomock.Any(), projectID).Return(&[]iaas.Server{
				{
					Name: ptr.To("foo"),
				},
			}, nil)

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			}

			exist, err := instance.InstanceExists(context.Background(), node)
			Expect(err).NotTo(HaveOccurred())
			Expect(exist).To(BeTrue())
		})

		It("successfully get the instance when provider ID is there", func() {
			nodeMockClient.EXPECT().GetServer(gomock.Any(), projectID, serverID).Return(&iaas.Server{
				Name: ptr.To("foo"),
			}, nil)

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: corev1.NodeSpec{
					ProviderID: fmt.Sprintf("stackit:///%s", serverID),
				},
			}

			exist, err := instance.InstanceExists(context.Background(), node)
			Expect(err).NotTo(HaveOccurred())
			Expect(exist).To(BeTrue())
		})

		It("error when list server fails", func() {
			nodeMockClient.EXPECT().ListServers(gomock.Any(), projectID).Return(nil, fmt.Errorf("failed due to some reason"))

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			}

			_, err := instance.InstanceExists(context.Background(), node)
			Expect(err).To(HaveOccurred())
		})

		It("does not error when get server instance not found", func() {
			nodeMockClient.EXPECT().GetServer(gomock.Any(), projectID, serverID).Return(nil, stackit.ErrorNotFound)

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: corev1.NodeSpec{
					ProviderID: fmt.Sprintf("stackit:///%s", serverID),
				},
			}

			exist, err := instance.InstanceExists(context.Background(), node)
			Expect(err).NotTo(HaveOccurred())
			Expect(exist).To(BeFalse())
		})
	})

	Describe("InstanceShutdown", func() {
		It("successfully gets the instance status with provider ID", func() {
			nodeMockClient.EXPECT().ListServers(gomock.Any(), projectID).Return(&[]iaas.Server{
				{
					Name:   ptr.To("foo"),
					Status: ptr.To(instanceStopping),
				},
			}, nil)

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			}

			isShutdown, err := instance.InstanceShutdown(context.Background(), node)
			Expect(err).NotTo(HaveOccurred())
			Expect(isShutdown).To(BeTrue())
		})

		It("successfully gets the instance status without provider ID", func() {
			nodeMockClient.EXPECT().GetServer(gomock.Any(), projectID, serverID).Return(&iaas.Server{
				Name:   ptr.To("foo"),
				Status: ptr.To("ACTIVE"),
			}, nil)

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: corev1.NodeSpec{
					ProviderID: fmt.Sprintf("stackit:///%s", serverID),
				},
			}

			isShutdown, err := instance.InstanceShutdown(context.Background(), node)
			Expect(err).NotTo(HaveOccurred())
			Expect(isShutdown).To(BeFalse())
		})

		It("fails if server not found", func() {
			nodeMockClient.EXPECT().ListServers(gomock.Any(), projectID).Return(nil, stackit.ErrorNotFound)

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			}

			isShutdown, err := instance.InstanceShutdown(context.Background(), node)
			Expect(err).To(HaveOccurred())
			Expect(isShutdown).To(BeFalse())
		})
	})

	Describe("InstanceMetadata", func() {
		It("does not error if instance not found", func() {
			nodeMockClient.EXPECT().ListServers(gomock.Any(), projectID).Return(&[]iaas.Server{}, nil)

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			}

			metadata, err := instance.InstanceMetadata(context.Background(), node)
			Expect(err).NotTo(HaveOccurred())
			Expect(metadata).To(BeNil())
		})

		It("successfully get all the metadata values", func() {
			nodeMockClient.EXPECT().ListServers(gomock.Any(), projectID).Return(&[]iaas.Server{
				{
					Name:        ptr.To("foo"),
					Id:          ptr.To(serverID),
					MachineType: ptr.To("flatcar"),
					Nics: &[]iaas.ServerNetwork{
						{
							Ipv4: ptr.To("10.10.100.24"),
						},
					},
				},
			}, nil)

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			}

			metadata, err := instance.InstanceMetadata(context.Background(), node)
			Expect(err).NotTo(HaveOccurred())
			Expect(metadata.ProviderID).To(Equal(fmt.Sprintf("stackit:///%s", serverID)))
			Expect(metadata.InstanceType).To(Equal("flatcar"))
			Expect(metadata.NodeAddresses).To(Equal([]corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "10.10.100.24",
				},
				{
					Type:    corev1.NodeHostName,
					Address: "foo",
				},
			}))
			Expect(metadata.Region).To(Equal("eu-01"))
		})

		It("errors when list server fails", func() {
			nodeMockClient.EXPECT().ListServers(gomock.Any(), projectID).Return(nil, fmt.Errorf("failed due to some reason"))

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			}

			metadata, err := instance.InstanceMetadata(context.Background(), node)
			Expect(err).To(HaveOccurred())
			Expect(metadata).To(BeNil())
		})
	})
})
