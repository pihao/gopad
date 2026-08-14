// Package server wires the HTTP routes and WebSocket handling around the
// document registry.
package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/coder/websocket"

	"gopad/internal/document"
	"gopad/internal/store"
)

// maxMessageSize bounds a single inbound WebSocket frame.
const maxMessageSize = 2 << 20 // 2 MiB

// docIDPattern constrains document ids to a sane URL-safe alphabet.
var docIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Config struct {
	// DefaultTTL is the time-to-live for new documents. Zero means 24h.
	DefaultTTL time.Duration
	// MaxDocSize is the per-document byte limit. Zero means 1 MiB.
	MaxDocSize int
	// Store enables SQLite persistence when non-nil.
	Store *store.Store
	// FlushInterval is how often dirty documents are persisted. Zero means 3s.
	FlushInterval time.Duration
	// SweepInterval is how often expiry/eviction runs. Zero means 60s.
	SweepInterval time.Duration
	// EvictAfter is how long an idle, fully-persisted document stays
	// resident without connections. Zero means 10m.
	EvictAfter time.Duration
	// AdminUser and AdminPassword protect the admin console. When either
	// is empty the console is disabled entirely.
	AdminUser     string
	AdminPassword string
}

type Server struct {
	registry      *document.Registry
	mux           *http.ServeMux
	log           *slog.Logger
	store         *store.Store
	flushInterval time.Duration
	sweepInterval time.Duration
	evictAfter    time.Duration
	adminUser     string
	adminPassword string
}

func New(cfg Config) *Server {
	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = 24 * time.Hour
	}
	if cfg.MaxDocSize == 0 {
		cfg.MaxDocSize = 1 << 20
	}
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 3 * time.Second
	}
	if cfg.SweepInterval == 0 {
		cfg.SweepInterval = time.Minute
	}
	if cfg.EvictAfter == 0 {
		cfg.EvictAfter = 10 * time.Minute
	}
	s := &Server{
		registry:      document.NewRegistry(cfg.DefaultTTL, cfg.MaxDocSize),
		mux:           http.NewServeMux(),
		log:           slog.Default(),
		store:         cfg.Store,
		flushInterval: cfg.FlushInterval,
		sweepInterval: cfg.SweepInterval,
		evictAfter:    cfg.EvictAfter,
		adminUser:     cfg.AdminUser,
		adminPassword: cfg.AdminPassword,
	}
	if s.store != nil {
		s.registry.Loader = s.loadFromStore
	}
	s.mux.HandleFunc("GET /api/socket/{id}", s.handleSocket)
	s.mux.HandleFunc("GET /api/readonly/{id}", s.handleReadonlySocket)
	s.mux.HandleFunc("GET /api/readonlyid/{id}", s.handleReadonlyID)
	s.mux.HandleFunc("GET /api/text/{id}", s.handleText)
	if s.adminUser != "" && s.adminPassword != "" {
		s.mux.HandleFunc("GET /admin", s.requireAdmin(s.handleAdminPage))
		s.mux.HandleFunc("GET /api/admin/documents", s.requireAdmin(s.handleAdminList))
		s.mux.HandleFunc("DELETE /api/admin/documents/{id}", s.requireAdmin(s.handleAdminDelete))
	}
	s.mux.Handle("GET /", staticHandler())
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Registry exposes the document registry (used by persistence and admin).
func (s *Server) Registry() *document.Registry { return s.registry }

func (s *Server) handleText(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !docIDPattern.MatchString(id) {
		http.Error(w, "invalid document id", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if doc := s.registry.Get(id, false); doc != nil {
		io.WriteString(w, doc.Text())
	}
}

func (s *Server) handleSocket(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !docIDPattern.MatchString(id) {
		http.Error(w, "invalid document id", http.StatusBadRequest)
		return
	}
	ws, err := websocketAccept(w, r)
	if err != nil {
		s.log.Warn("websocket accept failed", "err", err)
		return
	}
	defer ws.CloseNow()

	doc := s.registry.Get(id, true)
	s.serveConn(r.Context(), ws, doc, false)
}

// serveConn pumps messages between one WebSocket and a document until either
// side goes away.
func (s *Server) serveConn(ctx context.Context, ws *websocket.Conn, doc *document.Document, readonly bool) {
	conn := doc.Connect(readonly)
	defer doc.Disconnect(conn)

	// Writer: drain the document's outbound queue onto the socket. When the
	// document drops the connection (kill, slow consumer) the channel closes
	// and we close the socket to unblock the read loop below.
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for msg := range conn.Outbound() {
			if err := ws.Write(ctx, websocket.MessageText, msg); err != nil {
				return
			}
		}
		ws.Close(websocket.StatusGoingAway, "connection dropped by server")
	}()

	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			break
		}
		if err := doc.Handle(conn, data); err != nil {
			s.log.Warn("closing connection", "doc", conn.ID, "err", err)
			ws.Close(websocket.StatusPolicyViolation, truncate(err.Error(), 120))
			break
		}
	}
	doc.Disconnect(conn) // closes the outbound channel, ending the writer
	<-writeDone
	if doc.ConnCount() == 0 {
		s.saveDoc(doc) // persist promptly once the last user leaves
	}
}

// websocketAccept upgrades the request and applies the shared read limit.
func websocketAccept(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(maxMessageSize)
	return ws, nil
}

// truncate shortens s to at most n bytes (close reasons are limited to 125).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
