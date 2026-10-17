package agentconfigsvc

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/marmutapp/superbased-observer/internal/fsatomic"
)

// writeAtomic writes a vendor file atomically with fsync, keeping mode.
// A directory it must create is 0755 (vendor config dirs are readable by
// their tools, which run as the same user).
func writeAtomic(path string, b []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	return fsatomic.WriteFile(path, b, fsatomic.Options{
		TempPattern: ".observer-edit-*.tmp", Fsync: true, DirPerm: 0o755, FilePerm: mode,
	})
}

// pinnedTarget is the symlink pin (the internal/hook F6 rule, same
// semantics): resolve once after the lock, write the resolved file, and
// refuse when the link moved between the read and the write.
type pinnedTarget struct {
	path, target string
	isSymlink    bool
	linkValue    string
}

func pinTarget(path string) pinnedTarget {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return pinnedTarget{path: path, target: path}
	}
	link, err := os.Readlink(path)
	if err != nil {
		return pinnedTarget{path: path, target: path}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved == "" {
		return pinnedTarget{path: path, target: path, isSymlink: true, linkValue: link}
	}
	return pinnedTarget{path: path, target: resolved, isSymlink: true, linkValue: link}
}

func (p pinnedTarget) verifyUnmoved() error {
	fi, err := os.Lstat(p.path)
	if err != nil {
		if p.isSymlink {
			return fmt.Errorf("%s: the symlink disappeared while Observer was preparing the write; refusing", p.path)
		}
		return nil
	}
	isLink := fi.Mode()&os.ModeSymlink != 0
	if isLink != p.isSymlink {
		return fmt.Errorf("%s: changed between read and write (symlink-ness changed); refusing", p.path)
	}
	if !p.isSymlink {
		return nil
	}
	link, err := os.Readlink(p.path)
	if err != nil || link != p.linkValue {
		return fmt.Errorf("%s: the symlink was retargeted while Observer was preparing the write; refusing", p.path)
	}
	return nil
}

// statRegular stats a regular file (nil, nil when it does not exist).
func statRegular(path string) (os.FileInfo, error) {
	fi, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return fi, nil
}
