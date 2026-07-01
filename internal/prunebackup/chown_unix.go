//go:build unix

package prunebackup

import (
	"os"
	"syscall"
)

// preserveOwner chowns dst to the uid/gid of origInfo when the platform exposes
// them via *syscall.Stat_t. It is best-effort: a nil error means either the
// chown succeeded or ownership information was unavailable; a non-nil error is
// the chown failure, which the caller logs without aborting. The returned
// attempted flag reports whether a chown was issued (so the caller can log
// accurately).
func preserveOwner(dst string, origInfo os.FileInfo) (attempted bool, err error) {
	st, ok := origInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return false, nil
	}
	return true, os.Chown(dst, int(st.Uid), int(st.Gid))
}
