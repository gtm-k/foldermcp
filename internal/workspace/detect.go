package workspace

// detectNetworkFS returns true if the given path resides on a network
// filesystem. UNC paths (\\server\share or //server/share) are always
// treated as network paths. Otherwise, it delegates to the platform-specific
// detection which may use GetDriveType on Windows, /proc/mounts on Linux, etc.
func detectNetworkFS(path string) bool {
	if isUNCPath(path) {
		return true
	}
	return detectNetworkFSPlatform(path)
}
