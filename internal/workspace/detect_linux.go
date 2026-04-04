//go:build linux

package workspace

import (
	"golang.org/x/sys/unix"
)

// Known network filesystem magic numbers from statfs(2).
var networkFSTypes = map[int64]bool{
	0x6969:     true, // NFS_SUPER_MAGIC
	0xFF534D42: true, // CIFS_MAGIC_NUMBER (SMB)
	0x65735546: true, // FUSE_SUPER_MAGIC
	0x5346414F: true, // AFS_SUPER_MAGIC
	0x7461636F: true, // OCFS2_SUPER_MAGIC
	0xFE534D42: true, // SMB2_MAGIC_NUMBER
	0x564C:     true, // NCP_SUPER_MAGIC
	0x6B414653: true, // kAFS
}

// detectNetworkFSPlatform uses statfs to check f_type against known network FS
// magic numbers. Returns false if detection fails.
func detectNetworkFSPlatform(path string) bool {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return false
	}
	return networkFSTypes[stat.Type]
}
