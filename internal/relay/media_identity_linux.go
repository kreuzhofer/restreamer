package relay

import (
	"fmt"
	"os"
	"syscall"
)

func sourceFileIdentity(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d:%d:%d:%d:%d", stat.Dev, stat.Ino, info.Size(), info.ModTime().UnixNano(), stat.Ctim.Sec, stat.Ctim.Nsec)
}
