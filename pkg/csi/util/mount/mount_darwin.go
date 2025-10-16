//go:build darwin

package mount

import "golang.org/x/sys/unix"

func newDeviceStats(statfs *unix.Statfs_t) *DeviceStats {
	return &DeviceStats{
		Block: false,

		AvailableBytes: int64(statfs.Bavail) * int64(statfs.Bsize),
		TotalBytes:     int64(statfs.Blocks) * int64(statfs.Bsize),
		UsedBytes:      (int64(statfs.Blocks) - int64(statfs.Bfree)) * int64(statfs.Bsize),

		AvailableInodes: int64(statfs.Ffree),
		TotalInodes:     int64(statfs.Files),
		UsedInodes:      int64(statfs.Files) - int64(statfs.Ffree),
	}
}
