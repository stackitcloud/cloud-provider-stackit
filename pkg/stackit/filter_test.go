package stackit

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"k8s.io/utils/ptr"
)

var _ = Describe("Filter", func() {
	Describe("filterBackups", func() {
		var (
			backups []iaas.Backup
			filters map[string]string
		)

		BeforeEach(func() {
			backups = []iaas.Backup{
				{Status: ptr.To("available"), VolumeId: ptr.To("vol-1"), Name: ptr.To("backup-1")},
				{Status: ptr.To("error"), VolumeId: ptr.To("vol-2"), Name: ptr.To("backup-2")},
				{Status: ptr.To("available"), VolumeId: ptr.To("vol-1"), Name: ptr.To("backup-3")},
			}
			filters = make(map[string]string)
		})

		It("should return all backups when filters is nil", func() {
			result := filterBackups(backups, nil)
			Expect(result).To(Equal(backups))
		})

		It("should filter by Status", func() {
			filters["Status"] = "available"
			result := filterBackups(backups, filters)
			Expect(result).To(HaveLen(2))
			Expect(*result[0].Name).To(Equal("backup-1"))
			Expect(*result[1].Name).To(Equal("backup-3"))
		})

		It("should filter by VolumeID", func() {
			filters["VolumeID"] = "vol-2"
			result := filterBackups(backups, filters)
			Expect(result).To(HaveLen(1))
			Expect(*result[0].Name).To(Equal("backup-2"))
		})

		It("should filter by Name", func() {
			filters["Name"] = "backup-1"
			result := filterBackups(backups, filters)
			Expect(result).To(HaveLen(1))
			Expect(*result[0].Name).To(Equal("backup-1"))
		})

		It("should filter by multiple criteria", func() {
			filters["Status"] = "available"
			filters["VolumeID"] = "vol-1"
			result := filterBackups(backups, filters)
			Expect(result).To(HaveLen(2))
			Expect(*result[0].Name).To(Equal("backup-1"))
			Expect(*result[1].Name).To(Equal("backup-3"))
		})
	})

	Describe("filterVolumes", func() {
		var (
			volumes []iaas.Volume
			filters map[string]string
		)

		BeforeEach(func() {
			volumes = []iaas.Volume{
				{Name: ptr.To("volume-1")},
				{Name: ptr.To("volume-2")},
				{Name: ptr.To("volume-1")},
			}
			filters = make(map[string]string)
		})

		It("should return all volumes when filters is nil", func() {
			result := filterVolumes(volumes, nil)
			Expect(result).To(Equal(volumes))
		})

		It("should filter by Name", func() {
			filters["Name"] = "volume-1"
			result := filterVolumes(volumes, filters)
			Expect(result).To(HaveLen(2))
			Expect(*result[0].Name).To(Equal("volume-1"))
			Expect(*result[1].Name).To(Equal("volume-1"))
		})
	})

	Describe("filterSnapshots", func() {
		var (
			snapshots []iaas.Snapshot
			filters   map[string]string
		)

		BeforeEach(func() {
			snapshots = []iaas.Snapshot{
				{Status: ptr.To("available"), VolumeId: ptr.To("vol-1"), Name: ptr.To("snapshot-1")},
				{Status: ptr.To("error"), VolumeId: ptr.To("vol-2"), Name: ptr.To("snapshot-2")},
				{Status: ptr.To("available"), VolumeId: ptr.To("vol-1"), Name: ptr.To("snapshot-3")},
			}
			filters = make(map[string]string)
		})

		It("should return all snapshots when filters is nil", func() {
			result := filterSnapshots(snapshots, nil)
			Expect(result).To(Equal(snapshots))
		})

		It("should filter by Status", func() {
			filters["Status"] = "available"
			result := filterSnapshots(snapshots, filters)
			Expect(result).To(HaveLen(2))
			Expect(*result[0].Name).To(Equal("snapshot-1"))
			Expect(*result[1].Name).To(Equal("snapshot-3"))
		})

		It("should filter by VolumeID", func() {
			filters["VolumeID"] = "vol-2"
			result := filterSnapshots(snapshots, filters)
			Expect(result).To(HaveLen(1))
			Expect(*result[0].Name).To(Equal("snapshot-2"))
		})

		It("should filter by Name", func() {
			filters["Name"] = "snapshot-1"
			result := filterSnapshots(snapshots, filters)
			Expect(result).To(HaveLen(1))
			Expect(*result[0].Name).To(Equal("snapshot-1"))
		})

		It("should filter by multiple criteria", func() {
			filters["Status"] = "available"
			filters["VolumeID"] = "vol-1"
			result := filterSnapshots(snapshots, filters)
			Expect(result).To(HaveLen(2))
			Expect(*result[0].Name).To(Equal("snapshot-1"))
			Expect(*result[1].Name).To(Equal("snapshot-3"))
		})
	})
})
