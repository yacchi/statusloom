package webconfig

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

func doRequest(t *testing.T, method, url string, headers map[string]string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, url, err)
	}
	return resp
}

func TestAuth_MissingOrWrongToken(t *testing.T) {
	ts := startTestServer(t, time.Hour)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/tools"},
		{"GET", "/api/sessions"},
		{"GET", "/api/dsl/document?tool=claude-code"},
		{"POST", "/api/dsl/preview"},
		{"POST", "/api/shutdown"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path+"/missing", func(t *testing.T) {
			resp := doRequest(t, ep.method, ts.baseURL+ep.path, nil, nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", resp.StatusCode)
			}
			assertJSONError(t, resp, "unauthorized")
		})

		t.Run(ep.method+" "+ep.path+"/wrong", func(t *testing.T) {
			resp := doRequest(t, ep.method, ts.baseURL+ep.path, map[string]string{
				"Authorization": "Bearer not-the-token",
			}, nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

func TestAuth_StaticNoAuthRequired(t *testing.T) {
	ts := startTestServer(t, time.Hour)

	resp := doRequest(t, "GET", ts.baseURL+"/", nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / status = %d, want 200", resp.StatusCode)
	}
}

func assertJSONError(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if body.Error != want {
		t.Errorf("error = %q, want %q", body.Error, want)
	}
}

func TestHost_Invalid(t *testing.T) {
	ts := startTestServer(t, time.Hour)

	resp := doRequest(t, "GET", ts.baseURL+"/api/tools", map[string]string{
		"Host":          "evil.example.com",
		"Authorization": "Bearer " + ts.token,
	}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestHost_ValidVariants(t *testing.T) {
	ts := startTestServer(t, time.Hour)

	for _, host := range []string{
		fmt.Sprintf("127.0.0.1:%d", ts.port),
		fmt.Sprintf("localhost:%d", ts.port),
	} {
		t.Run(host, func(t *testing.T) {
			resp := doRequest(t, "GET", ts.baseURL+"/api/tools", map[string]string{
				"Host":          host,
				"Authorization": "Bearer " + ts.token,
			}, nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", resp.StatusCode)
			}
		})
	}
}

func TestOrigin_CrossOriginForbidden(t *testing.T) {
	ts := startTestServer(t, time.Hour)

	resp := doRequest(t, "GET", ts.baseURL+"/api/tools", map[string]string{
		"Authorization": "Bearer " + ts.token,
		"Origin":        "http://evil.example.com",
	}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestOrigin_SameOriginAllowed(t *testing.T) {
	ts := startTestServer(t, time.Hour)

	resp := doRequest(t, "GET", ts.baseURL+"/api/tools", map[string]string{
		"Authorization": "Bearer " + ts.token,
		"Origin":        ts.baseURL,
	}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestNoCookiesOrCORSHeaders(t *testing.T) {
	ts := startTestServer(t, time.Hour)

	resp := doRequest(t, "GET", ts.baseURL+"/api/tools", map[string]string{
		"Authorization": "Bearer " + ts.token,
		"Origin":        ts.baseURL,
	}, nil)
	defer resp.Body.Close()

	if resp.Header.Get("Set-Cookie") != "" {
		t.Errorf("Set-Cookie header present: %q", resp.Header.Get("Set-Cookie"))
	}
	for _, h := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers"} {
		if resp.Header.Get(h) != "" {
			t.Errorf("CORS header %s present: %q", h, resp.Header.Get(h))
		}
	}
}
