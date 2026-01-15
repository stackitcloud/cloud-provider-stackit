package ccm

import (
	"bytes"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetConfig", func() {
	It("should parse valid YAML configuration", func() {
		validYAML := `
global:
  projectId: "test-project"
  region: "eu01"
metadata:
  searchOrder: "metadataService,configDrive"
loadBalancer:
  api: "https://load-balancer.api.eu01.stackit.cloud"
  networkId: "test-network"
`

		config, err := GetConfig(strings.NewReader(validYAML))
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Global.ProjectID).To(Equal("test-project"))
		Expect(config.Global.Region).To(Equal("eu01"))
		Expect(config.Metadata.SearchOrder).To(Equal("metadataService,configDrive"))
		Expect(config.LoadBalancer.API).To(Equal("https://load-balancer.api.eu01.stackit.cloud"))
		Expect(config.LoadBalancer.NetworkID).To(Equal("test-network"))
	})

	It("should handle missing optional fields", func() {
		minimalYAML := `
global:
  projectId: "my-project"
  region: "eu01"
loadBalancer:
  networkId: "my-network"
`

		config, err := GetConfig(strings.NewReader(minimalYAML))
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Global.ProjectID).To(Equal("my-project"))
		Expect(config.Global.Region).To(Equal("eu01"))
		Expect(config.LoadBalancer.NetworkID).To(Equal("my-network"))
		Expect(config.LoadBalancer.API).To(BeEmpty())
	})

	It("should handle configuration with extra labels", func() {
		yamlWithLabels := `
global:
  projectId: "test-project"
  region: "eu01"
loadBalancer:
  networkId: "test-network"
  extraLabels:
    environment: "production"
    team: "platform"
`

		config, err := GetConfig(strings.NewReader(yamlWithLabels))
		Expect(err).NotTo(HaveOccurred())
		Expect(config.LoadBalancer.ExtraLabels).To(HaveKeyWithValue("environment", "production"))
		Expect(config.LoadBalancer.ExtraLabels).To(HaveKeyWithValue("team", "platform"))
	})

	It("should return error for malformed YAML", func() {
		invalidYAML := `
global:
  projectId: "test-project"
  region: "eu01"
  invalid yaml syntax here [[[
`

		_, err := GetConfig(strings.NewReader(invalidYAML))
		Expect(err).To(HaveOccurred())
	})

	It("should handle empty YAML gracefully", func() {
		emptyYAML := ``

		config, err := GetConfig(strings.NewReader(emptyYAML))
		Expect(err).NotTo(HaveOccurred())
		Expect(config).To(Equal(Config{}))
	})

	It("should return error for invalid YAML structure", func() {
		invalidStructure := `
this is not valid yaml structure
- random: content
`

		_, err := GetConfig(strings.NewReader(invalidStructure))
		Expect(err).To(HaveOccurred())
	})

	It("should handle YAML with comments", func() {
		yamlWithComments := `
# This is a comment
global:
  projectId: "test-project" # inline comment
  region: "eu01"
# Another comment
loadBalancer:
  networkId: "test-network"
`

		config, err := GetConfig(strings.NewReader(yamlWithComments))
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Global.ProjectID).To(Equal("test-project"))
		Expect(config.LoadBalancer.NetworkID).To(Equal("test-network"))
	})

	It("should handle YAML with different string quote styles", func() {
		mixedQuotesYAML := `
global:
  projectId: "test-project"
  region: 'eu01'
loadBalancer:
  networkId: test-network
`

		config, err := GetConfig(strings.NewReader(mixedQuotesYAML))
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Global.ProjectID).To(Equal("test-project"))
		Expect(config.Global.Region).To(Equal("eu01"))
		Expect(config.LoadBalancer.NetworkID).To(Equal("test-network"))
	})

	It("should handle empty reader gracefully", func() {
		emptyReader := bytes.NewReader([]byte{})

		config, err := GetConfig(emptyReader)
		Expect(err).NotTo(HaveOccurred())
		Expect(config).To(Equal(Config{}))
	})
})
