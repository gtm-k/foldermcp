//go:build cgo

package store

type Tier string

const (
	TierEntry    Tier = "entry"    // ≤2 GB RAM
	TierMid      Tier = "mid"      // 4-8 GB RAM
	TierProsumer Tier = "prosumer" // 16+ GB RAM
)

type PragmaProfile struct {
	MmapSize         int64
	CacheSizePages   int // negative = KB
	JournalSizeLimit int64
}

var pragmaProfiles = map[Tier]PragmaProfile{
	TierEntry:    {MmapSize: 0, CacheSizePages: -8000, JournalSizeLimit: 67108864},
	TierMid:      {MmapSize: 67108864, CacheSizePages: -32000, JournalSizeLimit: 67108864},
	TierProsumer: {MmapSize: 1073741824, CacheSizePages: -131072, JournalSizeLimit: 67108864},
}

// DetectTier returns the auto-selected tier based on host RAM in MB.
// Overridable via indexd.yaml at runtime.
func DetectTier(ramMB int) Tier {
	switch {
	case ramMB <= 2048:
		return TierEntry
	case ramMB <= 8192:
		return TierMid
	default:
		return TierProsumer
	}
}
