package relay

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Stage all files before committing removal. Before the commit marker exists,
// recovery rolls every rename back; afterwards it finishes physical cleanup.
// Paths are relative to the media library. Only explicit library deletion owns originals.
func validDeletionPath(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) == 2 && parts[0] == "presentations" {
		for _, suffix := range []string{"-scene.pcm", "-music.pcm"} {
			if strings.HasSuffix(parts[1], suffix) && validRevisionID(strings.TrimSuffix(parts[1], suffix)) {
				return true
			}
		}
		return false
	}
	if len(parts) == 2 && parts[0] == "originals" {
		return validClipName(parts[1])
	}
	if len(parts) == 2 && parts[0] == "prepared" {
		id := strings.TrimSuffix(parts[1], ".flv")
		return strings.HasSuffix(parts[1], ".flv") && len(id) == 65 && id[32] == '-' && validDesignID(id[:32]) && validDesignID(id[33:])
	}
	if len(parts) == 2 && parts[0] == "revisions" {
		return validRevisionID(strings.TrimSuffix(parts[1], filepath.Ext(parts[1]))) && (filepath.Ext(parts[1]) == ".json" || filepath.Ext(parts[1]) == ".flv")
	}
	if len(parts) == 3 && parts[0] == "generator" && parts[1] == "captures" && strings.HasSuffix(parts[2], ".flv") {
		return validDesignID(strings.TrimSuffix(parts[2], ".flv"))
	}
	if len(parts) != 3 || parts[0] != "generator" || filepath.Ext(parts[2]) != ".json" {
		return false
	}
	id := strings.TrimSuffix(parts[2], ".json")
	switch parts[1] {
	case "designs", "jobs", "templates", "playlists":
		return validDesignID(id)
	case "themes":
		var revision int
		if len(id) < 34 || id[32] != '-' || !validDesignID(id[:32]) {
			return false
		}
		_, err := fmt.Sscanf(id[33:], "%d", &revision)
		return err == nil && revision > 0 && fmt.Sprint(revision) == id[33:]
	}
	return false
}

func recoverMediaDeletions(root string) error {
	dir := filepath.Join(root, ".deletions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "delete-") {
			return errors.New("invalid media deletion storage")
		}
		if err := recoverMediaDeletion(root, filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func recoverMediaDeletion(root, dir string) error {
	var paths []string
	if err := readMediaJSON(filepath.Join(dir, "paths.json"), &paths); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		// No files move before the manifest has been written.
		return os.RemoveAll(dir)
	}
	for _, path := range paths {
		if !validDeletionPath(path) {
			return errors.New("invalid media deletion path")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "committed")); err == nil {
		return purgeMediaDeletion(dir, paths)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for i, path := range paths {
		staged := filepath.Join(dir, fmt.Sprint(i))
		if _, err := os.Lstat(staged); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		destination := filepath.Join(root, path)
		if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
			return errors.New("cannot restore interrupted media deletion")
		}
		if err := os.Rename(staged, destination); err != nil {
			return err
		}
	}
	return os.RemoveAll(dir)
}

var errMediaDeletionRollback = errors.New("cannot restore media deletion")

// committed distinguishes an acknowledged catalog change from a staging failure.
func deleteMediaFiles(root string, paths []string) (committed bool, err error) {
	return deleteReviewedMediaFiles(root, paths, nil)
}

// Review staged files before committing so external replacements can be restored.
func deleteReviewedMediaFiles(root string, paths []string, review func(path, staged string) error) (committed bool, err error) {
	for _, path := range paths {
		if !validDeletionPath(path) {
			return false, errors.New("invalid media deletion path")
		}
	}
	dir, err := os.MkdirTemp(filepath.Join(root, ".deletions"), "delete-")
	if err != nil {
		return false, err
	}
	if err := writeState(filepath.Join(dir, "paths.json"), paths); err != nil {
		return false, errors.Join(err, os.RemoveAll(dir))
	}
	rollback := func(cause error) (bool, error) {
		if err := recoverMediaDeletion(root, dir); err != nil {
			return false, errors.Join(cause, errMediaDeletionRollback, err)
		}
		return false, cause
	}
	for i, path := range paths {
		source := filepath.Join(root, path)
		parent, err := os.Stat(filepath.Dir(source))
		if err != nil {
			return rollback(err)
		}
		if !parent.IsDir() {
			return rollback(errors.New("media storage directory unavailable"))
		}
		info, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return rollback(err)
		}
		if !info.Mode().IsRegular() {
			return rollback(errors.New("media deletion requires regular files"))
		}
		err = os.Rename(source, filepath.Join(dir, fmt.Sprint(i)))
		// Missing output is already removed; its metadata can still be cleaned up.
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return rollback(err)
		}
	}
	if review != nil {
		for i, path := range paths {
			if err := review(path, filepath.Join(dir, fmt.Sprint(i))); err != nil {
				return rollback(err)
			}
		}
	}
	if err := writeState(filepath.Join(dir, "committed"), true); err != nil {
		return rollback(err)
	}
	return true, purgeMediaDeletion(dir, paths)
}

// Keep the commit marker until every payload is gone. Removing the entire
// directory at once could remove that marker before a crash, making recovery
// mistake the remaining files for an uncommitted transaction and restore them.
func purgeMediaDeletion(dir string, paths []string) error {
	for i := range paths {
		if err := os.Remove(filepath.Join(dir, fmt.Sprint(i))); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.RemoveAll(dir)
}
