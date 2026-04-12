//go:build !cgo

package v3

// HasIndexerSupport reports whether this binary was built with the cgo tag
// and can run foldermcp index-v3. False in laptop-only builds.
const HasIndexerSupport = false
