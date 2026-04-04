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

	// Convert f_fstypename to a Go string.
	// Fstypename is [16]byte on modern Go/macOS.
	fstype := byteArrayToString(stat.Fstypename[:])
	fstype = strings.ToLower(fstype)

	for _, netType := range networkFSTypeNames {
		if strings.Contains(fstype, netType) {
			return true
		}
	}
	return false
}

// byteArrayToString converts a null-terminated byte slice to a Go string.
func byteArrayToString(arr []byte) string {
	for i, b := range arr {
		if b == 0 {
			return string(arr[:i])
		}
	}
	return string(arr)
}
