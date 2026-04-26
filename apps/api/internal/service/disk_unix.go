//go:build !windows

package service

import (
	"fmt"
	"syscall"
)

func diskUsage() string {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return "n/a"
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free
	if total == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", (float64(used)/float64(total))*100)
}
