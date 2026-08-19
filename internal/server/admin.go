package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"gopad/internal/document"
)

// requireAdmin wraps a handler with HTTP Basic Auth. Credentials are hashed
// before comparison so the check is constant-time regardless of length.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Admin data is live state; no intermediary may cache it.
		w.Header().Set("Cache-Control", "no-cache")
		user, pass, ok := r.BasicAuth()
		if !ok || !timingSafeEqual(user, s.adminUser) || !timingSafeEqual(pass, s.adminPassword) {
			w.Header().Set("WWW-Authenticate", `Basic realm="gopad admin"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func timingSafeEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

// adminDoc is one row of the admin console listing.
type adminDoc struct {
	ID          string `json:"id"`
	SizeBytes   int    `json:"sizeBytes"`
	Language    string `json:"language"`
	Connections int    `json:"connections"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	ExpiresAt   int64  `json:"expiresAt"`
}

// liveAdminDoc builds a listing row from a resident document's live state.
func liveAdminDoc(doc *document.Document) adminDoc {
	conns, bytes, lang, created, updated, expires := doc.Stats()
	return adminDoc{
		ID:          doc.ID(),
		SizeBytes:   bytes,
		Language:    lang,
		Connections: conns,
		CreatedAt:   created.Unix(),
		UpdatedAt:   updated.Unix(),
		ExpiresAt:   expires.Unix(),
	}
}

type adminListResponse struct {
	Total     int        `json:"total"`
	Page      int        `json:"page"`
	Size      int        `json:"size"`
	Documents []adminDoc `json:"documents"`
}

func (s *Server) handleAdminList(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	sortKey := r.URL.Query().Get("sort")
	ascending := r.URL.Query().Get("order") == "asc"

	// Collect all documents first (metadata only), overlaying live state for
	// resident ones, then sort and paginate in one place. At the designed
	// scale (thousands of documents) this is cheap and keeps sorting by
	// live-only fields like connection counts consistent.
	var docs []adminDoc
	if s.store != nil {
		// Persist pending changes first so the store is a complete listing.
		s.Flush()
		metas, err := s.store.ListMeta()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		listed := make(map[string]bool, len(metas))
		for _, m := range metas {
			listed[m.ID] = true
			d := adminDoc{
				ID:        m.ID,
				SizeBytes: int(m.SizeBytes),
				Language:  m.Language,
				CreatedAt: m.CreatedAt,
				UpdatedAt: m.UpdatedAt,
				ExpiresAt: m.ExpiresAt,
			}
			if doc := s.registry.Peek(m.ID); doc != nil {
				d.Connections, d.SizeBytes, d.Language, _, _, _ = doc.Stats()
			}
			docs = append(docs, d)
		}
		// Resident documents that were never edited are not dirty, so Flush
		// did not store them — overlay them so the listing is store ∪ memory.
		for _, doc := range s.registry.All() {
			if !listed[doc.ID()] {
				docs = append(docs, liveAdminDoc(doc))
			}
		}
	} else {
		// Memory-only mode: list resident documents.
		for _, doc := range s.registry.All() {
			docs = append(docs, liveAdminDoc(doc))
		}
	}
	sortAdminDocs(docs, sortKey, ascending)

	resp := adminListResponse{Total: len(docs), Page: page, Size: size}
	start := min((page-1)*size, len(docs))
	end := min(start+size, len(docs))
	resp.Documents = docs[start:end]
	if resp.Documents == nil {
		resp.Documents = []adminDoc{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// sortAdminDocs orders docs by the given key ("size", "conns", "created",
// "updated", "expires"; anything else means "updated"), descending unless
// ascending is set, with the id as a stable tie-breaker.
func sortAdminDocs(docs []adminDoc, key string, ascending bool) {
	value := func(d adminDoc) int64 {
		switch key {
		case "size":
			return int64(d.SizeBytes)
		case "conns":
			return int64(d.Connections)
		case "created":
			return d.CreatedAt
		case "expires":
			return d.ExpiresAt
		default: // "updated"
			return d.UpdatedAt
		}
	}
	sort.Slice(docs, func(i, j int) bool {
		vi, vj := value(docs[i]), value(docs[j])
		if vi != vj {
			if ascending {
				return vi < vj
			}
			return vi > vj
		}
		return docs[i].ID < docs[j].ID
	})
}

func (s *Server) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !docIDPattern.MatchString(id) {
		http.Error(w, "invalid document id", http.StatusBadRequest)
		return
	}
	found := false
	if doc := s.registry.Peek(id); doc != nil {
		doc.Kill("deleted by admin")
		s.registry.Remove(id)
		found = true
	}
	if s.store != nil {
		if row, err := s.store.Load(id); err == nil && row != nil {
			found = true
		}
		if err := s.store.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.log.Info("document deleted by admin", "id", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	page := s.site.page("admin.html")
	if page == nil {
		http.Error(w, "admin page not built: run `cd frontend && npm run build`", http.StatusNotImplemented)
		return
	}
	writeHTML(w, page)
}

// handleReadonlyID returns the read-only share id of a document.
func (s *Server) handleReadonlyID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !docIDPattern.MatchString(id) {
		http.Error(w, "invalid document id", http.StatusBadRequest)
		return
	}
	doc := s.registry.Get(id, true)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"readonlyId": doc.ReadonlyID()})
}

// handleReadonlySocket serves the read-only WebSocket endpoint. Unlike the
// writable endpoint it never creates documents.
func (s *Server) handleReadonlySocket(w http.ResponseWriter, r *http.Request) {
	roID := r.PathValue("id")
	if !docIDPattern.MatchString(roID) {
		http.Error(w, "invalid document id", http.StatusBadRequest)
		return
	}
	doc := s.getByReadonlyID(roID)
	if doc == nil {
		http.Error(w, "document not found", http.StatusNotFound)
		return
	}
	ws, err := websocketAccept(w, r)
	if err != nil {
		s.log.Warn("websocket accept failed", "err", err)
		return
	}
	defer ws.CloseNow()
	s.serveConn(r.Context(), ws, doc, true)
}

func (s *Server) getByReadonlyID(roID string) *document.Document {
	for _, d := range s.registry.All() {
		if d.ReadonlyID() == roID && !d.Expired(time.Now()) {
			return d
		}
	}
	if s.store != nil {
		if row, err := s.store.LoadByReadonlyID(roID); err == nil && row != nil {
			return s.registry.Get(row.ID, false) // loader re-checks expiry
		}
	}
	return nil
}
