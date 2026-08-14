package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"gopad/internal/document"
	"gopad/internal/ot"
)

// testClient implements the classic ot.js client state machine
// (Synchronized / AwaitingConfirm / AwaitingWithBuffer) over the wire
// protocol — the same logic the browser frontend uses.
type testClient struct {
	t  *testing.T
	ws *websocket.Conn

	mu          sync.Mutex
	me          uint64
	revision    int // number of server operations integrated
	doc         string
	outstanding *ot.Operation
	buffer      *ot.Operation
	err         error
}

func newTestClient(t *testing.T, url string) *testClient {
	t.Helper()
	ws, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	ws.SetReadLimit(1 << 22)
	c := &testClient{t: t, ws: ws}
	go c.readLoop()
	return c
}

func (c *testClient) readLoop() {
	for {
		_, data, err := c.ws.Read(context.Background())
		if err != nil {
			return
		}
		var msg document.ServerMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			c.fail(fmt.Errorf("bad server message %s: %w", data, err))
			return
		}
		if msg.Identity != nil {
			c.mu.Lock()
			c.me = *msg.Identity
			c.mu.Unlock()
		}
		if msg.History != nil {
			c.applyHistory(msg.History)
		}
	}
}

func (c *testClient) applyHistory(h *document.HistoryMsg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, uop := range h.Operations {
		rev := h.Start + i
		if rev < c.revision {
			continue // already integrated
		}
		if uop.ID == c.me && c.outstanding != nil {
			// Acknowledgement of our own outstanding operation.
			c.revision++
			c.outstanding = c.buffer
			c.buffer = nil
			if c.outstanding != nil {
				c.sendEditLocked()
			}
			continue
		}
		op := uop.Operation
		var err error
		if c.outstanding != nil {
			c.outstanding, op, err = ot.Transform(c.outstanding, op)
			if err != nil {
				c.fail(fmt.Errorf("transform outstanding: %w", err))
				return
			}
		}
		if c.buffer != nil {
			c.buffer, op, err = ot.Transform(c.buffer, op)
			if err != nil {
				c.fail(fmt.Errorf("transform buffer: %w", err))
				return
			}
		}
		c.doc, err = op.Apply(c.doc)
		if err != nil {
			c.fail(fmt.Errorf("apply remote op: %w", err))
			return
		}
		c.revision++
	}
}

// edit applies a locally generated operation and queues it for the server.
func (c *testClient) edit(gen func(doc string) *ot.Operation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	op := gen(c.doc)
	newDoc, err := op.Apply(c.doc)
	if err != nil {
		c.fail(fmt.Errorf("apply local op: %w", err))
		return
	}
	c.doc = newDoc
	switch {
	case c.outstanding == nil:
		c.outstanding = op
		c.sendEditLocked()
	case c.buffer == nil:
		c.buffer = op
	default:
		c.buffer, err = ot.Compose(c.buffer, op)
		if err != nil {
			c.fail(fmt.Errorf("compose buffer: %w", err))
		}
	}
}

func (c *testClient) sendEditLocked() {
	msg := document.ClientMsg{Edit: &document.EditMsg{Revision: c.revision, Operation: c.outstanding}}
	b, _ := json.Marshal(msg)
	if err := c.ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		c.fail(fmt.Errorf("write edit: %w", err))
	}
}

func (c *testClient) send(msg document.ClientMsg) {
	b, _ := json.Marshal(msg)
	if err := c.ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		c.fail(fmt.Errorf("write: %w", err))
	}
}

func (c *testClient) close() {
	c.ws.CloseNow()
}

// waitDoc polls until the client's document equals want.
func (c *testClient) waitDoc(want string) {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		doc, _, _, err := c.state()
		if err != nil {
			c.t.Fatal(err)
		}
		if doc == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	doc, _, _, _ := c.state()
	c.t.Fatalf("document never became %q, still %q", want, doc)
}

func (c *testClient) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
}

// state returns (doc, revision, synchronized, err) atomically.
func (c *testClient) state() (string, int, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.doc, c.revision, c.outstanding == nil && c.buffer == nil, c.err
}

func startServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(New(Config{}))
	t.Cleanup(srv.Close)
	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	return srv, wsBase
}

var testAlphabet = []rune("abcdef 12Ω中🌍\n")

func randText(rng *rand.Rand) string {
	n := 1 + rng.Intn(4)
	runes := make([]rune, n)
	for i := range runes {
		runes[i] = testAlphabet[rng.Intn(len(testAlphabet))]
	}
	return string(runes)
}

func randOpFor(rng *rand.Rand, doc string) *ot.Operation {
	op := ot.New()
	remaining := len([]rune(doc))
	edited := false
	for remaining > 0 {
		n := 1 + rng.Intn(remaining)
		switch rng.Intn(4) {
		case 0, 1:
			op.Retain(n)
			remaining -= n
		case 2:
			op.Delete(n)
			remaining -= n
			edited = true
		default:
			op.Insert(randText(rng))
			edited = true
		}
	}
	if !edited || rng.Intn(3) == 0 {
		op.Insert(randText(rng))
	}
	return op
}

// waitConverged polls until both clients are synchronized at the same
// revision with identical documents.
func waitConverged(t *testing.T, a, b *testClient) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		docA, revA, syncA, errA := a.state()
		docB, revB, syncB, errB := b.state()
		if errA != nil {
			t.Fatalf("client a failed: %v", errA)
		}
		if errB != nil {
			t.Fatalf("client b failed: %v", errB)
		}
		if syncA && syncB && revA == revB {
			if docA != docB {
				t.Fatalf("clients converged to different texts:\n a=%q\n b=%q", docA, docB)
			}
			return docA
		}
		time.Sleep(10 * time.Millisecond)
	}
	docA, revA, syncA, _ := a.state()
	docB, revB, syncB, _ := b.state()
	t.Fatalf("no convergence: a(rev=%d sync=%v doc=%q) b(rev=%d sync=%v doc=%q)",
		revA, syncA, docA, revB, syncB, docB)
	return ""
}

func fetchText(t *testing.T, srv *httptest.Server, id string) string {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/text/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestBasicCollaboration(t *testing.T) {
	srv, wsBase := startServer(t)
	a := newTestClient(t, wsBase+"/api/socket/basic")
	b := newTestClient(t, wsBase+"/api/socket/basic")

	a.edit(func(string) *ot.Operation { return ot.New().Insert("hello") })
	waitConverged(t, a, b)
	b.edit(func(doc string) *ot.Operation {
		return ot.New().Retain(len([]rune(doc))).Insert(" world")
	})
	got := waitConverged(t, a, b)
	if got != "hello world" {
		t.Errorf("got %q, want \"hello world\"", got)
	}
	if text := fetchText(t, srv, "basic"); text != "hello world" {
		t.Errorf("/api/text got %q", text)
	}
}

func TestConcurrentRandomEditsConverge(t *testing.T) {
	srv, wsBase := startServer(t)
	a := newTestClient(t, wsBase+"/api/socket/fuzz")
	b := newTestClient(t, wsBase+"/api/socket/fuzz")
	rngA := rand.New(rand.NewSource(1))
	rngB := rand.New(rand.NewSource(2))

	for i := 0; i < 200; i++ {
		a.edit(func(doc string) *ot.Operation { return randOpFor(rngA, doc) })
		b.edit(func(doc string) *ot.Operation { return randOpFor(rngB, doc) })
		if i%20 == 0 {
			time.Sleep(5 * time.Millisecond) // let acks interleave with edits
		}
	}
	got := waitConverged(t, a, b)
	if text := fetchText(t, srv, "fuzz"); text != got {
		t.Errorf("server text diverged from clients:\n server=%q\n client=%q", text, got)
	}
}

func TestLateJoinerCatchesUp(t *testing.T) {
	_, wsBase := startServer(t)
	a := newTestClient(t, wsBase+"/api/socket/late")
	a.edit(func(string) *ot.Operation { return ot.New().Insert("early bird") })
	waitClientSync(t, a)

	b := newTestClient(t, wsBase+"/api/socket/late")
	got := waitConverged(t, a, b)
	if got != "early bird" {
		t.Errorf("late joiner got %q", got)
	}
}

func waitClientSync(t *testing.T, c *testClient) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, sync, err := c.state(); err != nil {
			t.Fatal(err)
		} else if sync {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("client never synchronized")
}

func TestInvalidMessageClosesConnection(t *testing.T) {
	_, wsBase := startServer(t)
	ws, _, err := websocket.Dial(context.Background(), wsBase+"/api/socket/bad", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if err := ws.Write(context.Background(), websocket.MessageText, []byte(`{"Edit":{"revision":99,"operation":[1]}}`)); err != nil {
		t.Fatal(err)
	}
	// The server should close the connection; reads must eventually error.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := ws.Read(ctx); err != nil {
			if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Errorf("expected policy violation close, got %v", err)
			}
			return
		}
	}
}

func TestUserPresenceBroadcast(t *testing.T) {
	_, wsBase := startServer(t)
	a := newTestClient(t, wsBase+"/api/socket/users")
	a.send(document.ClientMsg{ClientInfo: &document.UserInfo{Name: "alice", Hue: 120}})

	ws, _, err := websocket.Dial(context.Background(), wsBase+"/api/socket/users", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	// The second connection's initial state must include alice.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var msg document.ServerMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.UserInfo != nil && msg.UserInfo.Info != nil && msg.UserInfo.Info.Name == "alice" {
			return
		}
	}
	t.Fatal("never saw alice's UserInfo")
}

func TestTextOfUnknownDocIsEmpty(t *testing.T) {
	srv, _ := startServer(t)
	if text := fetchText(t, srv, "nope"); text != "" {
		t.Errorf("got %q, want empty", text)
	}
}
