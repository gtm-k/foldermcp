package workspace

// detectNetworkFS returns true if the given path resides on a network
// filesystem. It uses platform-specific detection and defaults to false
// (assume local) if detection fails.
//
// The actual implementation is provided by detectNetworkFSPlatform in
// platform-specific files (detect_windows.go, detect_linux.go, etc.).
func detectNetworkFS(path string) bool {
	return detectNetworkFSPlatform(path)
}
