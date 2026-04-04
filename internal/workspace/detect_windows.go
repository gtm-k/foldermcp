//go:build windows

package workspace

import (
	"strings"
	"syscall"
	"unsafe"
)

const driveRemote = 4 // DRIVE_REMOTE

var (
	kernel32      = syscall.NewLazyDLL("kernel32.dll")
	getDriveTypeW = kernel32.NewProc("GetDriveTypeW")
)

// detectNetworkFSPlatform checks for UNC paths (\\server\share) and uses
// GetDriveType on the drive letter to detect DRIVE_REMOTE.
func detectNetworkFSPlatform(path string) bool {
	// Check for UNC paths: \\server\share or //server/share
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, `//`) {
		return true
	}

	// Extract drive root (e.g. "C:\")
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	root := strings.ToUpper(path[:2]) + `\`

	rootPtr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return false
	}

	ret, _, _ := getDriveTypeW.Call(uintptr(unsafe.Pointer(rootPtr)))
	return ret == driveRemote
}
