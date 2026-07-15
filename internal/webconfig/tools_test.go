package webconfig

import (
	"net/http"
	"testing"
	"time"
)

func authedGet(t *testing.T, ts *testServer, path string) *http.Response {
	t.Helper()
	return doRequest(t, "GET", ts.baseURL+path, map[string]string{
		"Authorization": "Bearer " + ts.token,
	}, nil)
}

func TestTools(t *testing.T) {
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/tools")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Tools []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"tools"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tools) != 2 {
		t.Fatalf("len(tools) = %d, want 2", len(body.Tools))
	}
	if body.Tools[0].ID != "claude-code" || body.Tools[0].DisplayName != "Claude Code" {
		t.Errorf("tools[0] = %+v, want {claude-code, Claude Code}", body.Tools[0])
	}
	if body.Tools[1].ID != "claude-code-subagent" || body.Tools[1].DisplayName != "Claude Code Subagent" {
		t.Errorf("tools[1] = %+v, want {claude-code-subagent, Claude Code Subagent}", body.Tools[1])
	}
}
