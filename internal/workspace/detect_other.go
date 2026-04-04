//go:build !windows && !linux && !darwin

package workspace

// detectNetworkFSPlatform is a no-op on unsupported platforms.
// Returns false (assume local filesystem).
func detectNetworkFSPlatform(_ string) bool {
	return false
}
