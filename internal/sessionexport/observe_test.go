package sessionexport

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
)

const (
	observePID       = 4242
	observeStartedAt = int64(1762170000)
	observeConfigDir = "/cfg"
)

var observeT0 = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

// observePayload builds the base payload with an overridable cost and cwd.
func observePayload(cost, cwd string) []byte {
	return []byte(`{"session_id":"s1","transcript_path":"/t/s1.jsonl","cwd":"` + cwd +
		`","model":{"id":"m1"},"cost":{"total_cost_usd":` + cost + `},"note":"a<b>&c"}`)
}

func observeDetailPath(t *testing.T) string {
	t.Helper()
	p, err := DetailPath(observePID, observeStartedAt)
	if err != nil {
		t.Fatalf("DetailPath() error = %v", err)
	}
	return p
}

func observeMarkerPath(t *testing.T) string {
	t.Helper()
	p, err := pendingPath()
	if err != nil {
		t.Fatalf("pendingPath() error = %v", err)
	}
	return p
}

func observeReadDetail(t *testing.T) Detail {
	t.Helper()
	var d Detail
	ok, err := cache.ReadJSON(observeDetailPath(t), &d)
	if err != nil || !ok {
		t.Fatalf("ReadJSON(detail) ok=%v err=%v", ok, err)
	}
	return d
}

func observeExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func observeMustObserve(t *testing.T, raw []byte, now time.Time) {
	t.Helper()
	if err := Observe(raw, observePID, observeStartedAt, observeConfigDir, now); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
}

func observeSetup(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

func TestObserveFreshWritesDetailAndMarker(t *testing.T) {
	observeSetup(t)
	observeMustObserve(t, observePayload("0.1", "/w"), observeT0)

	d := observeReadDetail(t)
	if d.Version != detailVersion {
		t.Errorf("version = %d, want %d", d.Version, detailVersion)
	}
	if !d.WrittenAt.Equal(observeT0) {
		t.Errorf("writtenAt = %v, want %v", d.WrittenAt, observeT0)
	}
	if d.Row.SeenAt != observeT0.Unix() {
		t.Errorf("seenAt = %d, want %d", d.Row.SeenAt, observeT0.Unix())
	}
	if d.Row.ConfigDir != observeConfigDir {
		t.Errorf("configDir = %q, want %q", d.Row.ConfigDir, observeConfigDir)
	}
	if d.Row.DetailPath != observeDetailPath(t) {
		t.Errorf("detailPath = %q, want %q", d.Row.DetailPath, observeDetailPath(t))
	}

	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(observeDetailPath(t)); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("detail mode = %v err=%v, want 0600", fi, err)
		}
		for _, dir := range []string{filepath.Dir(observeDetailPath(t)), filepath.Dir(filepath.Dir(observeDetailPath(t)))} {
			if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
				t.Errorf("dir %s mode = %v err=%v, want 0700", dir, fi, err)
			}
		}
	}

	fi, err := os.Stat(observeMarkerPath(t))
	if err != nil {
		t.Fatalf("marker missing: %v", err)
	}
	if fi.Size() != 0 {
		t.Errorf("marker size = %d, want 0", fi.Size())
	}
	if !MergePending() {
		t.Error("MergePending() = false, want true")
	}

	entries, err := os.ReadDir(filepath.Dir(observeDetailPath(t)))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("temp debris left behind: %s", e.Name())
		}
	}
}

func TestObserveSameBytesDoesNotRewrite(t *testing.T) {
	observeSetup(t)
	observeMustObserve(t, observePayload("0.1", "/w"), observeT0)
	observeMustObserve(t, append(observePayload("0.1", "/w"), '\n'), observeT0.Add(10*time.Second))

	if d := observeReadDetail(t); !d.WrittenAt.Equal(observeT0) {
		t.Errorf("writtenAt = %v, want unchanged %v", d.WrittenAt, observeT0)
	}
}

func TestObserveHTMLEscapedPayloadUnchanged(t *testing.T) {
	observeSetup(t)
	raw := observePayload("0.1", "/w")
	observeMustObserve(t, raw, observeT0)
	observeMustObserve(t, raw, observeT0.Add(10*time.Second))

	if d := observeReadDetail(t); !d.WrittenAt.Equal(observeT0) {
		t.Errorf("writtenAt = %v, want unchanged %v (payload with <>& must compare equal)", d.WrittenAt, observeT0)
	}
}

func TestObservePayloadOnlyChangeIsThrottledUntilTwoSeconds(t *testing.T) {
	observeSetup(t)
	observeMustObserve(t, observePayload("0.1", "/w"), observeT0)

	observeMustObserve(t, observePayload("0.2", "/w"), observeT0.Add(1999*time.Millisecond))
	if d := observeReadDetail(t); !d.WrittenAt.Equal(observeT0) {
		t.Errorf("at 1999ms writtenAt = %v, want unchanged %v", d.WrittenAt, observeT0)
	}

	if err := os.Remove(observeMarkerPath(t)); err != nil {
		t.Fatalf("remove marker: %v", err)
	}
	at2s := observeT0.Add(2 * time.Second)
	observeMustObserve(t, observePayload("0.2", "/w"), at2s)
	d := observeReadDetail(t)
	if !d.WrittenAt.Equal(at2s) {
		t.Errorf("at 2s writtenAt = %v, want %v", d.WrittenAt, at2s)
	}
	if d.Row.SeenAt != observeT0.Unix() {
		t.Errorf("payload-only change moved seenAt to %d, want %d", d.Row.SeenAt, observeT0.Unix())
	}
	if observeExists(observeMarkerPath(t)) {
		t.Error("payload-only rewrite re-created the merge marker")
	}
}

func TestObserveNegativeDeltaWrites(t *testing.T) {
	observeSetup(t)
	observeMustObserve(t, observePayload("0.1", "/w"), observeT0)

	back := observeT0.Add(-time.Second)
	observeMustObserve(t, observePayload("0.2", "/w"), back)
	if d := observeReadDetail(t); !d.WrittenAt.Equal(back) {
		t.Errorf("clock stepped back: writtenAt = %v, want %v", d.WrittenAt, back)
	}
}

func TestObserveSummaryChangeNotThrottled(t *testing.T) {
	observeSetup(t)
	observeMustObserve(t, observePayload("0.1", "/w"), observeT0)

	at := observeT0.Add(100 * time.Millisecond)
	observeMustObserve(t, observePayload("0.1", "/w2"), at)
	d := observeReadDetail(t)
	if !d.WrittenAt.Equal(at) {
		t.Errorf("summary change throttled: writtenAt = %v, want %v", d.WrittenAt, at)
	}
	if d.Row.SeenAt != at.Unix() {
		t.Errorf("seenAt = %d, want %d", d.Row.SeenAt, at.Unix())
	}
	if !observeExists(observeMarkerPath(t)) {
		t.Error("summary change did not create the merge marker")
	}
}

func TestObservePreexistingMarkerKeptOnSummaryChange(t *testing.T) {
	observeSetup(t)
	observeMustObserve(t, observePayload("0.1", "/w"), observeT0)
	observeMustObserve(t, observePayload("0.1", "/w2"), observeT0.Add(time.Minute))
	if !observeExists(observeMarkerPath(t)) {
		t.Fatal("marker missing after summary change")
	}
}

func TestObserveUnusableDetailTreatedAsAbsent(t *testing.T) {
	cases := map[string]string{
		"unparsable": "not json",
		"version2":   `{"version":2,"writtenAt":"2026-11-03T12:00:00Z","row":{},"payload":{}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			observeSetup(t)
			path := observeDetailPath(t)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}

			observeMustObserve(t, observePayload("0.1", "/w"), observeT0)
			d := observeReadDetail(t)
			if d.Version != detailVersion {
				t.Errorf("version = %d, want %d", d.Version, detailVersion)
			}
			if d.Row.SeenAt != observeT0.Unix() {
				t.Errorf("seenAt = %d, want %d (absent prev)", d.Row.SeenAt, observeT0.Unix())
			}
			if !observeExists(observeMarkerPath(t)) {
				t.Error("absent prev did not create the marker")
			}
		})
	}
}

func TestObserveRejectsInvalidPayloads(t *testing.T) {
	for _, raw := range []string{"{", "[]"} {
		t.Run(raw, func(t *testing.T) {
			observeSetup(t)
			if err := Observe([]byte(raw), observePID, observeStartedAt, observeConfigDir, observeT0); err == nil {
				t.Fatal("Observe() error = nil, want non-nil")
			}
			if observeExists(observeDetailPath(t)) {
				t.Error("detail written for invalid payload")
			}
			if observeExists(observeMarkerPath(t)) {
				t.Error("marker created for invalid payload")
			}
		})
	}
}

func TestObserveSessionsDirIsFileFails(t *testing.T) {
	observeSetup(t)
	dir := filepath.Dir(observeDetailPath(t))
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Observe(observePayload("0.1", "/w"), observePID, observeStartedAt, observeConfigDir, observeT0); err == nil {
		t.Fatal("Observe() error = nil, want non-nil")
	}
	if observeExists(observeMarkerPath(t)) {
		t.Error("marker created after failed detail write")
	}
}

func TestObserveNeverTouchesPublicFile(t *testing.T) {
	observeSetup(t)
	pub, err := PublicPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(pub), 0o700); err != nil {
		t.Fatal(err)
	}
	want := []byte("{\"keep\":true}\n")
	if err := os.WriteFile(pub, want, 0o600); err != nil {
		t.Fatal(err)
	}

	observeMustObserve(t, observePayload("0.1", "/w"), observeT0)
	observeMustObserve(t, observePayload("0.1", "/w2"), observeT0.Add(time.Second))

	got, err := os.ReadFile(pub)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("public file changed: %q", got)
	}
}

func TestObserveMergePendingFalseWithoutMarker(t *testing.T) {
	observeSetup(t)
	if MergePending() {
		t.Error("MergePending() = true with no marker, want false")
	}
}

func TestObserveMergePendingFalseWhenBaseDirFails(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	if MergePending() {
		t.Error("MergePending() = true when BaseDir fails, want false")
	}
}
