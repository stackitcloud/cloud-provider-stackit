package stackit

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"
	"gopkg.in/gcfg.v1"
)

var _ = Describe("Client", func() {
	Describe("Config", func() {
		It("should parse from ini", func() {
			var cfg Config

			Expect(gcfg.FatalOnly(gcfg.ReadInto(&cfg, strings.NewReader(iniConf)))).To(Succeed())
			Expect(cfg.Metadata).To(Equal(metadata.Opts{
				RequestTimeout: metadata.Duration{Duration: 5 * time.Second},
			},
			))
			Expect(cfg.BlockStorage).To(Equal(BlockStorageOpts{
				RescanOnResize: true,
			}))
		})
	})
})

var iniConf = `
[BlockStorage]
rescan-on-resize=true
[Metadata]
request-timeout=5s
`
