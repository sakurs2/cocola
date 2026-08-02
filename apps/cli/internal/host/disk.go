package host

import "syscall"

const (
	MinimumFreeDiskBytes = uint64(2 * 1024 * 1024 * 1024)
	WarnFreeDiskBytes    = uint64(10 * 1024 * 1024 * 1024)
)

func AvailableDiskBytes(path string) (uint64, error) {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return 0, err
	}
	return stats.Bavail * uint64(stats.Bsize), nil
}
