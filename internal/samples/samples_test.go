package samples

import (
	"testing"
	"time"
)

// TestNames verifies Names returns every recognized sample name, sorted, for
// UI enumeration.
func TestNames(t *testing.T) {
	got := Names()
	want := []string{EarlySession, Full, SubagentCompleted, SubagentRunning}
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("Names() not sorted: %v", got)
		}
	}
}

// TestDefaultForSection verifies the subagent section falls back to the
// subagent-running sample and every other section (including "main" and "")
// falls back to the full sample.
func TestDefaultForSection(t *testing.T) {
	if got := DefaultForSection("subagent"); got != SubagentRunning {
		t.Errorf("DefaultForSection(subagent) = %q, want %q", got, SubagentRunning)
	}
	for _, section := range []string{"main", "", "unknown"} {
		if got := DefaultForSection(section); got != Full {
			t.Errorf("DefaultForSection(%q) = %q, want %q", section, got, Full)
		}
	}
}

// TestSnapshot_KnownNames verifies every name Names() lists resolves with
// ok=true and carries the tool/session data previews depend on.
func TestSnapshot_KnownNames(t *testing.T) {
	now := time.Now()
	for _, name := range Names() {
		snap, ok := Snapshot(name, now)
		if !ok {
			t.Errorf("Snapshot(%q) ok = false, want true", name)
			continue
		}
		if snap.Tool.ID == "" {
			t.Errorf("Snapshot(%q).Tool.ID is empty", name)
		}
	}
}

// TestSnapshot_UnknownName verifies an unrecognized name reports ok=false
// rather than a zero-value "success".
func TestSnapshot_UnknownName(t *testing.T) {
	if _, ok := Snapshot("does-not-exist", time.Now()); ok {
		t.Error("Snapshot(unknown) ok = true, want false")
	}
}

// TestSnapshot_FullSampleHasSubagent verifies fullSample embeds a Subagent
// task, so the merged claude-code catalog's task-* fields render a real
// preview against it instead of falling back (samples.go's fullSample doc
// comment / webconfig's handleDSLFields).
func TestSnapshot_FullSampleHasSubagent(t *testing.T) {
	snap, ok := Snapshot(Full, time.Now())
	if !ok {
		t.Fatal("Snapshot(Full) ok = false")
	}
	if snap.Subagent == nil {
		t.Error("Snapshot(Full).Subagent is nil, want a sample task")
	}
}

// TestSubagentTasks_KnownNames verifies both subagent sample names resolve
// to a 3-task list whose statuses match "running"/"completed" respectively.
func TestSubagentTasks_KnownNames(t *testing.T) {
	now := time.Now()

	running, ok := SubagentTasks(SubagentRunning, now)
	if !ok || len(running) != 3 {
		t.Fatalf("SubagentTasks(SubagentRunning) = %v, %v, want 3 tasks, ok=true", running, ok)
	}
	for _, task := range running {
		if task.Status != "running" {
			t.Errorf("SubagentTasks(SubagentRunning) task %q status = %q, want running", task.ID, task.Status)
		}
	}

	completed, ok := SubagentTasks(SubagentCompleted, now)
	if !ok || len(completed) != 3 {
		t.Fatalf("SubagentTasks(SubagentCompleted) = %v, %v, want 3 tasks, ok=true", completed, ok)
	}
	for _, task := range completed {
		if task.Status != "completed" {
			t.Errorf("SubagentTasks(SubagentCompleted) task %q status = %q, want completed", task.ID, task.Status)
		}
	}
}

// TestSubagentTasks_NonSubagentName verifies a main-section sample name (or
// an unknown one) is rejected: SubagentTasks only resolves the two subagent
// sample names.
func TestSubagentTasks_NonSubagentName(t *testing.T) {
	for _, name := range []string{Full, EarlySession, "does-not-exist"} {
		if _, ok := SubagentTasks(name, time.Now()); ok {
			t.Errorf("SubagentTasks(%q) ok = true, want false", name)
		}
	}
}
