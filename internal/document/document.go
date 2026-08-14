// Package document holds the server-side state of one collaborative
// document: the text, the operation history, connected users, and their
// cursors. It is the Go equivalent of rustpad's Rustpad struct.
package document

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"gopad/internal/ot"
)

var (
	// ErrReadOnly is returned when a read-only connection sends a message
	// that would modify the document.
	ErrReadOnly = errors.New("document: connection is read-only")
	// ErrTooLarge is returned when an edit would grow the document past the
	// configured size limit.
	ErrTooLarge = errors.New("document: document size limit exceeded")
)

// TTL bounds from the requirements: 1 minute to 100 years.
const (
	MinTTL = time.Minute
	MaxTTL = 100 * 365 * 24 * time.Hour
)

// outboundBuffer is the per-connection queue of marshaled server messages.
// A connection that falls this far behind is dropped rather than allowed to
// stall the whole document.
const outboundBuffer = 512

// Conn is one subscriber of a document. The WebSocket handler owns the
// network side; the document pushes marshaled ServerMsg frames into ch.
type Conn struct {
	ID       uint64
	ReadOnly bool
	doc      *Document
	ch       chan []byte
	closed   bool // guarded by doc.mu
}

// Outbound returns the channel of marshaled server messages to write to the
// socket. It is closed when the connection is dropped by the document.
func (c *Conn) Outbound() <-chan []byte { return c.ch }

// Document is the in-memory state of one pad. All fields are guarded by mu;
// operations on different documents proceed in parallel.
type Document struct {
	mu sync.Mutex

	id         string
	readonlyID string
	text       string
	ops        []UserOp // history since revision 0; revision == len(ops)
	language   string

	users   map[uint64]UserInfo
	cursors map[uint64]CursorData
	conns   map[uint64]*Conn
	nextID  uint64

	ttl       time.Duration
	createdAt time.Time
	updatedAt time.Time
	dirty     bool
	killed    bool

	maxSize int // document size limit in bytes
}

// New creates an empty document.
func New(id, readonlyID string, ttl time.Duration, maxSize int) *Document {
	now := time.Now()
	return &Document{
		id:         id,
		readonlyID: readonlyID,
		users:      make(map[uint64]UserInfo),
		cursors:    make(map[uint64]CursorData),
		conns:      make(map[uint64]*Conn),
		nextID:     1, // connection id 0 is reserved for the restore op
		ttl:        ttl,
		createdAt:  now,
		updatedAt:  now,
		maxSize:    maxSize,
	}
}

// FromSnapshot reconstructs a document from persisted state. The text is
// replayed as a single synthetic insert operation (attributed to connection
// id 0, which is never assigned), so newly connecting clients receive the
// full content as regular history — the same trick rustpad uses.
func FromSnapshot(snap Snapshot, maxSize int) *Document {
	d := New(snap.ID, snap.ReadonlyID, snap.TTL, maxSize)
	d.text = snap.Text
	d.language = snap.Language
	d.createdAt = snap.CreatedAt
	d.updatedAt = snap.UpdatedAt
	if snap.Text != "" {
		d.ops = append(d.ops, UserOp{ID: 0, Operation: ot.New().Insert(snap.Text)})
	}
	return d
}

// Snapshot is the persistent subset of a document's state.
type Snapshot struct {
	ID         string
	ReadonlyID string
	Text       string
	Language   string
	TTL        time.Duration
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Connect registers a new subscriber and queues the initial state messages
// (identity, full history, language, expiry, users, cursors) on its channel.
func (d *Document) Connect(readonly bool) *Conn {
	d.mu.Lock()
	defer d.mu.Unlock()
	id := d.nextID
	d.nextID++
	c := &Conn{ID: id, ReadOnly: readonly, doc: d, ch: make(chan []byte, outboundBuffer)}
	d.conns[id] = c

	d.sendLocked(c, ServerMsg{Identity: &id})
	if len(d.ops) > 0 {
		d.sendLocked(c, ServerMsg{History: &HistoryMsg{Start: 0, Operations: d.ops}})
	}
	if d.language != "" {
		lang := d.language
		d.sendLocked(c, ServerMsg{Language: &lang})
	}
	d.sendLocked(c, ServerMsg{Expiry: d.expiryLocked()})
	for uid, info := range d.users {
		info := info
		d.sendLocked(c, ServerMsg{UserInfo: &UserInfoMsg{ID: uid, Info: &info}})
	}
	for uid, data := range d.cursors {
		d.sendLocked(c, ServerMsg{UserCursor: &UserCursorMsg{ID: uid, Data: data}})
	}
	return c
}

// Disconnect removes a subscriber and announces the departure. Safe to call
// more than once.
func (d *Document) Disconnect(c *Conn) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dropLocked(c)
	if _, ok := d.users[c.ID]; ok {
		delete(d.users, c.ID)
		delete(d.cursors, c.ID)
		d.broadcastLocked(ServerMsg{UserInfo: &UserInfoMsg{ID: c.ID}})
	}
}

// Handle processes one raw client frame from conn c.
// A returned error means the connection should be closed.
func (d *Document) Handle(c *Conn, raw []byte) error {
	var msg ClientMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("document: invalid message: %w", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.killed {
		return errors.New("document: document was deleted")
	}
	switch {
	case msg.Edit != nil:
		if c.ReadOnly {
			return ErrReadOnly
		}
		return d.applyEditLocked(c, msg.Edit)
	case msg.SetLanguage != nil:
		if c.ReadOnly {
			return ErrReadOnly
		}
		d.language = *msg.SetLanguage
		d.dirty = true
		d.broadcastLocked(ServerMsg{Language: msg.SetLanguage})
	case msg.ClientInfo != nil:
		d.users[c.ID] = *msg.ClientInfo
		d.broadcastLocked(ServerMsg{UserInfo: &UserInfoMsg{ID: c.ID, Info: msg.ClientInfo}})
	case msg.CursorData != nil:
		d.cursors[c.ID] = *msg.CursorData
		d.broadcastLocked(ServerMsg{UserCursor: &UserCursorMsg{ID: c.ID, Data: *msg.CursorData}})
	case msg.SetExpiry != nil:
		if c.ReadOnly {
			return ErrReadOnly
		}
		ttl := time.Duration(msg.SetExpiry.TTLSeconds) * time.Second
		if ttl < MinTTL || ttl > MaxTTL {
			return fmt.Errorf("document: ttl %d out of range [%d, %d] seconds",
				msg.SetExpiry.TTLSeconds, int64(MinTTL/time.Second), int64(MaxTTL/time.Second))
		}
		d.ttl = ttl
		d.dirty = true
		d.broadcastLocked(ServerMsg{Expiry: d.expiryLocked()})
	default:
		return errors.New("document: unknown message type")
	}
	return nil
}

// applyEditLocked is the heart of the OT server: transform the incoming
// operation over every operation the client had not yet seen, apply it,
// append it to the history, and broadcast it.
func (d *Document) applyEditLocked(c *Conn, e *EditMsg) error {
	if e.Operation == nil {
		return errors.New("document: edit without operation")
	}
	if e.Revision < 0 || e.Revision > len(d.ops) {
		return fmt.Errorf("document: got revision %d, but current is %d", e.Revision, len(d.ops))
	}
	op := e.Operation
	for _, h := range d.ops[e.Revision:] {
		var err error
		op, _, err = ot.Transform(op, h.Operation)
		if err != nil {
			return fmt.Errorf("document: transform failed: %w", err)
		}
	}
	newText, err := op.Apply(d.text)
	if err != nil {
		return fmt.Errorf("document: apply failed: %w", err)
	}
	if len(newText) > d.maxSize {
		return ErrTooLarge
	}
	d.text = newText
	userOp := UserOp{ID: c.ID, Operation: op}
	d.ops = append(d.ops, userOp)
	for uid, cd := range d.cursors {
		d.cursors[uid] = transformCursor(cd, op)
	}
	d.updatedAt = time.Now()
	d.dirty = true
	d.broadcastLocked(ServerMsg{History: &HistoryMsg{
		Start:      len(d.ops) - 1,
		Operations: []UserOp{userOp},
	}})
	return nil
}

func transformCursor(cd CursorData, op *ot.Operation) CursorData {
	out := CursorData{
		Cursors:    make([]int, len(cd.Cursors)),
		Selections: make([][2]int, len(cd.Selections)),
	}
	for i, p := range cd.Cursors {
		out.Cursors[i] = op.TransformIndex(p)
	}
	for i, s := range cd.Selections {
		out.Selections[i] = [2]int{op.TransformIndex(s[0]), op.TransformIndex(s[1])}
	}
	return out
}

// Kill announces deletion to every subscriber and drops all connections.
func (d *Document) Kill(reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.killed = true
	d.broadcastLocked(ServerMsg{Killed: &KilledMsg{Reason: reason}})
	for _, c := range d.conns {
		d.dropLocked(c)
	}
}

// Text returns the current document text.
func (d *Document) Text() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.text
}

// ID returns the document's identifier.
func (d *Document) ID() string { return d.id }

// ReadonlyID returns the identifier used by read-only share links.
func (d *Document) ReadonlyID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.readonlyID
}

// ConnCount returns the number of live connections.
func (d *Document) ConnCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.conns)
}

// TakeSnapshot returns the persistent state and clears the dirty flag.
// The second result is false when nothing changed since the last snapshot.
func (d *Document) TakeSnapshot() (Snapshot, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.dirty {
		return Snapshot{}, false
	}
	d.dirty = false
	return d.snapshotLocked(), true
}

func (d *Document) snapshotLocked() Snapshot {
	return Snapshot{
		ID:         d.id,
		ReadonlyID: d.readonlyID,
		Text:       d.text,
		Language:   d.language,
		TTL:        d.ttl,
		CreatedAt:  d.createdAt,
		UpdatedAt:  d.updatedAt,
	}
}

// MarkDirty re-flags the document after a failed save so the next flush
// retries it.
func (d *Document) MarkDirty() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dirty = true
}

// Expired reports whether the document's TTL has run out.
func (d *Document) Expired(now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return now.After(d.updatedAt.Add(d.ttl))
}

// Evictable reports whether the document can be dropped from memory: no
// connections, nothing unsaved, and idle for longer than idleFor.
func (d *Document) Evictable(now time.Time, idleFor time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.conns) == 0 && !d.dirty && now.Sub(d.updatedAt) > idleFor
}

// Stats returns live counters for the admin console.
func (d *Document) Stats() (connections int, sizeBytes int, language string, updatedAt, expiresAt time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.conns), len(d.text), d.language, d.updatedAt, d.updatedAt.Add(d.ttl)
}

func (d *Document) expiryLocked() *ExpiryMsg {
	return &ExpiryMsg{
		TTLSeconds: int64(d.ttl / time.Second),
		ExpiresAt:  d.updatedAt.Add(d.ttl).Unix(),
	}
}

// dropLocked closes a connection's channel and removes it from the fan-out
// set without announcing a user departure.
func (d *Document) dropLocked(c *Conn) {
	if !c.closed {
		c.closed = true
		close(c.ch)
	}
	delete(d.conns, c.ID)
}

func (d *Document) broadcastLocked(msg ServerMsg) {
	b, err := json.Marshal(msg)
	if err != nil {
		panic(fmt.Sprintf("document: marshal server message: %v", err))
	}
	for _, c := range d.conns {
		d.sendRawLocked(c, b)
	}
}

func (d *Document) sendLocked(c *Conn, msg ServerMsg) {
	b, err := json.Marshal(msg)
	if err != nil {
		panic(fmt.Sprintf("document: marshal server message: %v", err))
	}
	d.sendRawLocked(c, b)
}

func (d *Document) sendRawLocked(c *Conn, b []byte) {
	if c.closed {
		return
	}
	select {
	case c.ch <- b:
	default:
		// The consumer is too slow to keep up; drop it. Its handler will
		// notice the closed channel and finish the disconnect.
		d.dropLocked(c)
	}
}
