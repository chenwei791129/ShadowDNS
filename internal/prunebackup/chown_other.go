//go:build !unix

package prunebackup

import "os"

// preserveOwner is a no-op on non-Unix platforms, where the file's uid/gid are
// not exposed through *syscall.Stat_t. It reports attempted=false so the caller
// knows no ownership change was issued.
func preserveOwner(dst string, origInfo os.FileInfo) (attempted bool, err error) {
	return false, nil
}
