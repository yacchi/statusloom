package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

const modIngestRequestV1 = `{"v":1,"sessionId":"s1","boot":"b","events":[{"seq":1,"at":1000,"name":"agent.spawn","payload":{"permissionMode":"plan"}}],"queries":{}}`

// runModIngestWith runs runModIngest with args and stdin, returning the exit
// code and the captured stdout and stderr.
func runModIngestWith(t *testing.T, args []string, stdin string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runModIngest(args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestModIngest_ThenRenderCachesFillsMod(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	if code, _, stderr := runModIngestWith(t, nil, modIngestRequestV1); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	snap := &schema.StatusSnapshot{}
	snap.Session.ID = "s1"
	applyRenderCaches(snap, "default", time.Now(), false)
	if snap.Mod == nil {
		t.Fatal("snap.Mod = nil, want the recorded mod snapshot")
	}
	if snap.Mod.PermissionMode != "plan" {
		t.Errorf("snap.Mod.PermissionMode = %q, want plan", snap.Mod.PermissionMode)
	}
}

func TestModIngest_UnsupportedVersionWritesNothing(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", cacheDir)

	code, stdout, stderr := runModIngestWith(t, nil, `{"v":2,"sessionId":"s1","boot":"b","events":[]}`)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (a newer protocol must not make the mod retry)", code)
	}
	if stdout != "" || stderr != "" {
		t.Errorf("stdout = %q, stderr = %q, want both empty", stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "mod")); !os.IsNotExist(err) {
		t.Errorf("mod directory exists after an unsupported version, stat err = %v", err)
	}
}

func TestModIngest_InvalidJSON(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	code, stdout, stderr := runModIngestWith(t, nil, "not json")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.HasPrefix(stderr, "statusloom: ") || strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want one line starting with \"statusloom: \"", stderr)
	}
}

func TestModIngest_Arguments(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	code, stdout, stderr := runModIngestWith(t, []string{"x"}, modIngestRequestV1)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `unexpected argument "x"`) {
		t.Errorf("stderr = %q, want it to name the unexpected argument", stderr)
	}
}

func TestModIngest_StdoutEmpty(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	code, stdout, stderr := runModIngestWith(t, nil, modIngestRequestV1)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (the mod ignores stdout but the contract forbids it)", stdout)
	}
}

func TestRenderCaches_ModRecordReadOnly(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", cacheDir)

	// No record yet: the render path must not create the record or its directory.
	snap := &schema.StatusSnapshot{}
	snap.Session.ID = "s1"
	applyRenderCaches(snap, "default", time.Now(), false)
	if snap.Mod != nil {
		t.Errorf("snap.Mod = %+v, want nil without a record", snap.Mod)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "mod")); !os.IsNotExist(err) {
		t.Errorf("mod directory created by the render path, stat err = %v", err)
	}

	// A record for another session exists, but this snapshot has no session id:
	// the empty id must never load a record.
	if code, _, stderr := runModIngestWith(t, nil, modIngestRequestV1); code != 0 {
		t.Fatalf("ingest exit = %d, stderr = %q", code, stderr)
	}
	empty := &schema.StatusSnapshot{}
	applyRenderCaches(empty, "default", time.Now(), false)
	if empty.Mod != nil {
		t.Errorf("snap.Mod = %+v for an empty session id, want nil", empty.Mod)
	}
}
