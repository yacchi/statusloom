package sources

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func isolateConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", dir)
	return dir
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	isolateConfig(t)
	f, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f.Sources == nil || len(f.Sources) != 0 {
		t.Fatalf("Sources = %#v, want empty non-nil map", f.Sources)
	}
	if f.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", f.SchemaVersion, SchemaVersion)
	}
}

func TestSaveLoadRoundTripFillsID(t *testing.T) {
	isolateConfig(t)
	in := File{Sources: map[string]Definition{
		"mycmd":  {Kind: KindCommand, Argv: []string{"ccprofile", "sessions", "--json"}, TimeoutSeconds: 3},
		"ccfile": {Kind: KindFile, Path: "/tmp/sessions.json"},
	}}
	if err := Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cmd, ok := out.Get("mycmd")
	if !ok {
		t.Fatal("mycmd missing after round trip")
	}
	if cmd.ID != "mycmd" {
		t.Errorf("ID = %q, want mycmd", cmd.ID)
	}
	if !reflect.DeepEqual(cmd.Argv, []string{"ccprofile", "sessions", "--json"}) || cmd.TimeoutSeconds != 3 {
		t.Errorf("mycmd = %#v, want argv and timeout preserved", cmd)
	}
	if got := out.IDs(); !reflect.DeepEqual(got, []string{"ccfile", "mycmd"}) {
		t.Errorf("IDs = %v, want [ccfile mycmd]", got)
	}
}

func TestSaveWritesIndentedSortedFileWithoutIDKey(t *testing.T) {
	dir := isolateConfig(t)
	f := File{Sources: map[string]Definition{
		"b": {Kind: KindFile, Path: "/b"},
		"a": {Kind: KindFile, Path: "/a"},
	}}
	if err := Save(f); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "sources.json"))
	if err != nil {
		t.Fatalf("read sources.json: %v", err)
	}
	s := string(data)
	if !strings.HasPrefix(s, "{\n  \"schemaVersion\": 1,\n  \"sources\": {") || !strings.HasSuffix(s, "}\n") {
		t.Errorf("unexpected layout:\n%s", s)
	}
	if strings.Contains(s, `"id"`) || strings.Contains(s, `"ID"`) {
		t.Errorf("ID must not be serialized:\n%s", s)
	}
	if strings.Index(s, `"a"`) > strings.Index(s, `"b"`) {
		t.Errorf("keys not sorted:\n%s", s)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, "sources.json"))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("mode = %o, want 600", perm)
		}
	}
}

func TestSaveInvalidWritesNothing(t *testing.T) {
	dir := isolateConfig(t)
	f := File{Sources: map[string]Definition{
		"Bad_ID": {Kind: KindFile, Path: "/x"},
	}}
	err := Save(f)
	if err == nil || !strings.Contains(err.Error(), `invalid source id "Bad_ID"`) {
		t.Fatalf("Save error = %v, want invalid id error", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "sources.json")); !os.IsNotExist(statErr) {
		t.Fatalf("sources.json exists after failed Save (stat err %v)", statErr)
	}
}

func TestLoadInvalidJSONIsError(t *testing.T) {
	dir := isolateConfig(t)
	if err := os.WriteFile(filepath.Join(dir, "sources.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load of invalid JSON returned nil error")
	}
}

func TestPutValidatesAndReplaces(t *testing.T) {
	var f File
	if err := f.Put(Definition{ID: "a", Kind: KindFile, Path: "/p"}); err != nil {
		t.Fatalf("Put on zero File: %v", err)
	}
	if err := f.Put(Definition{ID: "a", Kind: KindFile, Path: "/q"}); err != nil {
		t.Fatalf("replacing Put: %v", err)
	}
	if d, _ := f.Get("a"); d.Path != "/q" {
		t.Errorf("Path = %q, want /q after replace", d.Path)
	}

	before := len(f.Sources)
	if err := f.Put(Definition{ID: "Bad_ID", Kind: KindFile, Path: "/x"}); err == nil {
		t.Fatal("Put of invalid definition returned nil error")
	}
	if len(f.Sources) != before {
		t.Errorf("invalid Put changed the map: %d -> %d entries", before, len(f.Sources))
	}
}

func TestGetRemoveUnknown(t *testing.T) {
	f := File{}
	if d, ok := f.Get("nope"); ok || d.ID != "" {
		t.Errorf("Get(nope) = %#v, %v; want zero, false", d, ok)
	}
	if f.Remove("nope") {
		t.Error("Remove(nope) = true, want false")
	}
}

func TestConfigDirMatchesPathDirectory(t *testing.T) {
	dir := isolateConfig(t)
	p, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if filepath.Dir(p) != dir {
		t.Errorf("Path dir = %q, want %q", filepath.Dir(p), dir)
	}
}
