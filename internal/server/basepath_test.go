package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

func TestNormalizeBasePath(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"/":        "",
		"  ":       "",
		"gopad":    "/gopad",
		"/gopad":   "/gopad",
		"/gopad/":  "/gopad",
		" /gopad ": "/gopad",
		"a/b":      "/a/b",
	}
	for in, want := range cases {
		if got := normalizeBasePath(in); got != want {
			t.Errorf("normalizeBasePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBasePathRouting(t *testing.T) {
	srv := httptest.NewServer(New(Config{BasePath: "/gopad"}))
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// The bare prefix redirects so relative URLs resolve under the mount.
	resp, err := client.Get(srv.URL + "/gopad")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("GET /gopad = %d, want 301", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/gopad/" {
		t.Fatalf("redirect to %q, want /gopad/", loc)
	}

	// Nothing is served at the domain root any more.
	for _, path := range []string{"/", "/api/text/x", "/assets/"} {
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
	}

	// The API is reachable under the prefix.
	resp, err = client.Get(srv.URL + "/gopad/api/text/nosuchdoc")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /gopad/api/text/nosuchdoc = %d, want 200", resp.StatusCode)
	}

	// The collaboration socket too.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/gopad/api/socket/prefixed"
	ws, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", wsURL, err)
	}
	ws.Close(websocket.StatusNormalClosure, "")
}

func TestBasePathPageRewrite(t *testing.T) {
	resp, err := http.Get(mustServe(t, "/gopad") + "/gopad/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readAll(t, resp)
	if !strings.Contains(body, `<base href="/gopad/">`) {
		t.Errorf("index page is missing the /gopad/ <base href>:\n%s", head(body))
	}
	if strings.Contains(body, `="/assets/`) {
		t.Errorf("index page still points at root-absolute assets:\n%s", head(body))
	}
	if !strings.Contains(body, `="/gopad/assets/`) {
		t.Errorf("index page is missing prefixed asset URLs:\n%s", head(body))
	}

	// An unknown path under the prefix still serves the app shell: the
	// document id lives in the URL hash.
	resp, err = http.Get(mustServe(t, "/gopad") + "/gopad/whatever")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !strings.Contains(readAll(t, resp), `<base href="/gopad/">`) {
		t.Error("SPA fallback did not serve the rewritten shell")
	}
}

func TestNoBasePathServesRootPages(t *testing.T) {
	resp, err := http.Get(mustServe(t, "") + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readAll(t, resp)
	if !strings.Contains(body, `<base href="/">`) {
		t.Errorf("index page is missing the root <base href>:\n%s", head(body))
	}
	if !strings.Contains(body, `="/assets/`) {
		t.Errorf("index page lost its asset URLs:\n%s", head(body))
	}
}

// mustServe starts a server with the given base path and closes it when the
// test ends.
func mustServe(t *testing.T, basePath string) string {
	t.Helper()
	srv := httptest.NewServer(New(Config{BasePath: basePath}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func head(s string) string {
	if len(s) > 400 {
		return s[:400]
	}
	return s
}
