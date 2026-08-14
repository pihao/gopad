package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"gopad/internal/document"
	"gopad/internal/ot"
)

func fetchReadonlyID(t *testing.T, srv *httptest.Server, id string) string {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/readonlyid/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		ReadonlyID string `json:"readonlyId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ReadonlyID == "" {
		t.Fatal("empty readonly id")
	}
	return out.ReadonlyID
}

func TestReadonlyConnectionSeesEditsButCannotWrite(t *testing.T) {
	srv, wsBase := startServer(t)

	writer := newTestClient(t, wsBase+"/api/socket/shared")
	writer.edit(func(string) *ot.Operation { return ot.New().Insert("shared text") })
	waitClientSync(t, writer)

	roID := fetchReadonlyID(t, srv, "shared")
	if roID == "shared" {
		t.Fatal("readonly id must differ from the document id")
	}

	reader := newTestClient(t, wsBase+"/api/readonly/"+roID)
	reader.waitDoc("shared text")

	// Live edits still flow to the read-only side.
	writer.edit(func(doc string) *ot.Operation {
		return ot.New().Retain(len([]rune(doc))).Insert("!")
	})
	reader.waitDoc("shared text!")

	// A write from the read-only side must close the connection.
	ws, _, err := websocket.Dial(context.Background(), wsBase+"/api/readonly/"+roID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	msg, _ := json.Marshal(document.ClientMsg{Edit: &document.EditMsg{Revision: 0, Operation: ot.New().Insert("hack")}})
	if err := ws.Write(context.Background(), websocket.MessageText, msg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := ws.Read(ctx); err != nil {
			if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Errorf("expected policy violation, got %v", err)
			}
			break
		}
	}
	// The document must be unchanged.
	if text := fetchText(t, srv, "shared"); text != "shared text!" {
		t.Errorf("document mutated by readonly connection: %q", text)
	}
}

func TestReadonlyUnknownIDRejected(t *testing.T) {
	_, wsBase := startServer(t)
	_, _, err := websocket.Dial(context.Background(), wsBase+"/api/readonly/does-not-exist", nil)
	if err == nil {
		t.Fatal("expected dial to fail for unknown readonly id")
	}
}

func TestAdminDisabledWithoutCredentials(t *testing.T) {
	srv, _ := startServer(t)
	resp, err := http.Get(srv.URL + "/api/admin/documents")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// Unregistered route falls through to the SPA handler, never to data.
	if resp.StatusCode == http.StatusOK && strings.Contains(resp.Header.Get("Content-Type"), "json") {
		t.Error("admin API served data without credentials configured")
	}
}

func adminServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(New(Config{AdminUser: "root", AdminPassword: "secret"}))
	t.Cleanup(srv.Close)
	return srv, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func adminReq(t *testing.T, method, url string, auth bool) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, nil)
	if auth {
		req.SetBasicAuth("root", "secret")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestAdminAuthAndListing(t *testing.T) {
	srv, wsBase := adminServer(t)

	c := newTestClient(t, wsBase+"/api/socket/admindoc")
	c.edit(func(string) *ot.Operation { return ot.New().Insert("hello admin") })
	waitClientSync(t, c)

	if resp := adminReq(t, "GET", srv.URL+"/api/admin/documents", false); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("without auth: status %d, want 401", resp.StatusCode)
	}

	resp := adminReq(t, "GET", srv.URL+"/api/admin/documents?page=1&size=10", true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with auth: status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var list adminListResponse
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("bad response %s: %v", body, err)
	}
	if list.Total != 1 || len(list.Documents) != 1 {
		t.Fatalf("list = %s", body)
	}
	d := list.Documents[0]
	if d.ID != "admindoc" || d.Connections != 1 || d.SizeBytes != len("hello admin") {
		t.Errorf("doc = %+v", d)
	}
}

func TestAdminDeleteKicksConnections(t *testing.T) {
	srv, wsBase := adminServer(t)

	c := newTestClient(t, wsBase+"/api/socket/doomed")
	c.edit(func(string) *ot.Operation { return ot.New().Insert("bye") })
	waitClientSync(t, c)

	if resp := adminReq(t, "DELETE", srv.URL+"/api/admin/documents/doomed", true); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status %d", resp.StatusCode)
	}
	if text := fetchText(t, srv, "doomed"); text != "" {
		t.Errorf("document still readable after delete: %q", text)
	}
	if resp := adminReq(t, "DELETE", srv.URL+"/api/admin/documents/doomed", true); resp.StatusCode != http.StatusNotFound {
		t.Errorf("second delete status %d, want 404", resp.StatusCode)
	}
}
