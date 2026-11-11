package relay

import (
	"errors"
	"golang.org/x/sys/unix"
)

func brandStorageCapacity(root string, required uint64) error {
	var stat unix.Statfs_t
	if unix.Statfs(root, &stat) != nil {
		return errors.New("Cannot check available package storage")
	}
	if uint64(stat.Bavail)*uint64(stat.Bsize) < required+(64<<20) {
		return errors.New("Insufficient free space for package preparation; free disk space and retry")
	}
	return nil
}
