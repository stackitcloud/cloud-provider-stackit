package stackit

import (
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"
)

var _ = Describe("Client", func() {
	Describe("Config", func() {
		Describe("GetConfig", func() {
			It("should parse valid yaml configuration", func() {
				cfg, err := GetConfig(strings.NewReader(validYamlConf))
				Expect(err).NotTo(HaveOccurred())
				Expect(cfg.Metadata).To(Equal(metadata.Opts{
					SearchOrder:    "configDrive,metadataService",
					RequestTimeout: metadata.Duration{Duration: 5 * time.Second},
				}))
				Expect(cfg.BlockStorage).To(Equal(BlockStorageOpts{
					RescanOnResize: true,
				}))
				Expect(cfg.Global.ProjectID).To(Equal("test-project"))
				Expect(cfg.Global.Region).To(Equal("eu01"))
				Expect(cfg.Global.IaasAPI).To(Equal("https://api.example.com"))
				Expect(cfg.Global.NetworkID).To(Equal("test-network"))
				Expect(cfg.Global.ExtraLabels).To(Equal(map[string]string{"env": "test"}))
				Expect(cfg.LoadBalancer.API).To(Equal("https://lb-api.example.com"))
			})

			It("should handle missing optional fields", func() {
				cfg, err := GetConfig(strings.NewReader(minimalYamlConf))
				Expect(err).NotTo(HaveOccurred())
				Expect(cfg.Global.ProjectID).To(Equal("test-project"))
				Expect(cfg.Global.Region).To(Equal("eu01"))
				// Optional fields should be empty/zero values
				Expect(cfg.Global.IaasAPI).To(BeEmpty())
				Expect(cfg.Global.NetworkID).To(BeEmpty())
				Expect(cfg.Global.ExtraLabels).To(BeEmpty())
			})

			It("should return error for invalid yaml", func() {
				_, err := GetConfig(strings.NewReader(invalidYamlConf))
				Expect(err).To(HaveOccurred())
			})

			It("should handle empty yaml gracefully", func() {
				cfg, err := GetConfig(strings.NewReader(emptyYamlConf))
				Expect(err).NotTo(HaveOccurred())
				// Empty YAML should result in zero values
				Expect(cfg).To(Equal(Config{}))
			})
		})

		Describe("GetConfigFromFile", func() {
			var tempFile *os.File
			var tempFilePath string

			BeforeEach(func() {
				var err error
				tempFile, err = os.CreateTemp("", "test-config-*.yaml")
				Expect(err).NotTo(HaveOccurred())
				tempFilePath = tempFile.Name()
			})

			AfterEach(func() {
				if tempFile != nil {
					tempFile.Close()
					os.Remove(tempFilePath)
				}
			})

			It("should read configuration from file", func() {
				_, err := tempFile.WriteString(validYamlConf)
				Expect(err).NotTo(HaveOccurred())
				tempFile.Close()

				cfg, err := GetConfigFromFile(tempFilePath)
				Expect(err).NotTo(HaveOccurred())
				Expect(cfg.Global.ProjectID).To(Equal("test-project"))
			})

			It("should return error for non-existent file", func() {
				_, err := GetConfigFromFile("non-existent-file.yaml")
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("Metadata Duration Parsing", func() {
			DescribeTable("should parse various duration formats",
				func(durationStr string, expected time.Duration) {
					yaml := fmt.Sprintf(`
global:
  projectId: "test-project"
  region: "eu01"
metadata:
  requestTimeout: "%s"`, durationStr)

					cfg, err := GetConfig(strings.NewReader(yaml))
					Expect(err).NotTo(HaveOccurred())
					Expect(cfg.Metadata.RequestTimeout.Duration).To(Equal(expected))
				},
				Entry("seconds", "5s", 5*time.Second),
				Entry("minutes", "1m", 1*time.Minute),
				Entry("hours", "2h", 2*time.Hour),
				Entry("milliseconds", "300ms", 300*time.Millisecond),
				Entry("complex duration", "1h30m15s", 1*time.Hour+30*time.Minute+15*time.Second),
			)

			It("should return error for invalid duration format", func() {
				yaml := `
global:
  projectId: "test-project"
  region: "eu01"
metadata:
  requestTimeout: "invalid-duration"`

				_, err := GetConfig(strings.NewReader(yaml))
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("Metadata Search Order Validation", func() {
			DescribeTable("should validate search order format",
				func(searchOrder string, shouldError bool, errorMsg string) {
					err := metadata.CheckMetadataSearchOrder(searchOrder)
					if shouldError {
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring(errorMsg))
					} else {
						Expect(err).NotTo(HaveOccurred())
					}
				},
				Entry("valid configDrive,metadataService", "configDrive,metadataService", false, ""),
				Entry("valid metadataService,configDrive", "metadataService,configDrive", false, ""),
				Entry("valid single configDrive", "configDrive", false, ""),
				Entry("valid single metadataService", "metadataService", false, ""),
				Entry("empty search order", "", true, "metadata.searchOrder"),
				Entry("too many elements", "configDrive,metadataService,extra", true, "metadata.searchOrder"),
				Entry("invalid element", "invalid", true, "metadata.searchOrder"),
				Entry("mixed valid and invalid", "configDrive,invalid", true, "metadata.searchOrder"),
			)
		})
	})
})

const validYamlConf = `
global:
  projectId: "test-project"
  iaasApi: "https://api.example.com"
  networkId: "test-network"
  extraLabels:
    env: "test"
  region: "eu01"
metadata:
  searchOrder: "configDrive,metadataService"
  requestTimeout: "5s"
blockStorage:
  rescanOnResize: true
loadBalancer:
  api: "https://lb-api.example.com"`

const minimalYamlConf = `
global:
  projectId: "test-project"
  region: "eu01"`

const invalidYamlConf = `
global:
  projectId: "test-project"
  region: "eu01"
  invalid yaml syntax here [[[`

const emptyYamlConf = ``
