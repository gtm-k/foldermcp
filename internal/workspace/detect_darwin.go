//go:build darwin

package workspace

import (
	"strings"

	"golang.org/x/sys/unix"
)

// networkFSTypeNames are filesystem type names reported by macOS statfs that
// indicate a network filesystem.
var networkFSTypeNames = []string{
	"nfs",
	"smbfs",
	"afpfs",
	"webdav",
	"osxfuse",
	"macfuse",
	"fuse",
}

// detectNetworkFSPlatform uses statfs and checks f_fstypename against known
// network filesystem type names. Returns false if detection fails.
func detectNetworkFSPlatform(path string) bool {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return false
	}

	// Convert f_fstypename (int8 array) to a Go string.
	fstype := int8ArrayToString(stat.Fstypename[:])
	fstype = strings.ToLower(fstype)

	for _, netType := range networkFSTypeNames {
		if strings.Contains(fstype, netType) {
			return true
		}
	}
	return false
}

// int8ArrayToString converts a null-terminated int8 slice to a Go string.
func int8ArrayToString(arr []int8) string {
	buf := make([]byte, 0, len(arr))
	for _, b := range arr {
		if b == 0 {
			break
		}
		buf = append(buf, byte(b))
	}
	return string(buf)
}
