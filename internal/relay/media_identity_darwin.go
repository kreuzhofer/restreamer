package relay

import (
	"fmt"
	"os"
	"syscall"
)

// Change time also changes when a file's modification time is restored. It
// avoids rereading gigabytes of unchanged originals on every discovery tick.
func sourceFileIdentity(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d:%d:%d:%d:%d", stat.Dev, stat.Ino, info.Size(), info.ModTime().UnixNano(), stat.Ctimespec.Sec, stat.Ctimespec.Nsec)
}
