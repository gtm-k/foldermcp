package v3

import "testing"

func TestHasIndexerSupportIsDefined(t *testing.T) {
	// Compile-time check: this file must reference HasIndexerSupport
	// to catch the case where both build files get excluded and the
	// constant silently vanishes.
	_ = HasIndexerSupport
}
