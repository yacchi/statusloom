package webconfig

// The sample-snapshot construction this file used to hold (sampleSnapshot,
// defaultSampleForSection, and the subagent sample data) has moved to
// internal/samples (plans/room-site.md 3.4節): it is the single source of
// truth shared by this package's /api/dsl/preview and /api/dsl/fields
// endpoints and cmd/room-wasm's statusloomPreview. See dsl.go for the call
// sites (samples.Snapshot / samples.DefaultForSection / samples.SubagentTasks).

// fptr returns a pointer to v. Kept here (rather than only in
// internal/samples) because fields_test.go and usageprobe_test.go build
// schema.ExtraUsageCache/RateWindow test fixtures with it independently of
// the sample snapshots.
func fptr(v float64) *float64 {
	return &v
}
