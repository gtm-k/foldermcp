package router

// AnnotateCompleteness adds a user-visible warning when completeness=partial,
// ensuring the LLM always surfaces degraded results to the user per spec §9.4.
//
// InspectResult and BrowseResult do not carry completeness metadata because
// they are single-source hard-error RPCs (spec §9.5): they either succeed
// fully or fail entirely, so partial completeness is not meaningful.
func AnnotateCompleteness(r *SearchResult) {
	if r.Completeness == "partial" && len(r.Warnings) == 0 {
		r.Warnings = []string{"completeness=partial; some sources failed. See failed_sources and sources[]."}
	}
}
