package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"gopad/internal/document"
)

// requireAdmin wraps a handler with HTTP Basic Auth. Credentials are hashed
// before comparison so the check is constant-time regardless of length.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

	var resp adminListResponse
	resp.Page, resp.Size = page, size

	if s.store != nil {
		// Persist pending changes first so the store is a complete listing.
		s.Flush()
		total, err := s.store.Count()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rows, err := s.store.List((page-1)*size, size)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp.Total = total
		for _, row := range rows {
			d := adminDoc{
				ID:        row.ID,
				SizeBytes: len(row.Text),
				Language:  row.Language,
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
				ExpiresAt: row.ExpiresAt,
			}
			if doc := s.registry.Peek(row.ID); doc != nil {
				d.Connections, d.SizeBytes, d.Language, _, _ = doc.Stats()
			}
			resp.Documents = append(resp.Documents, d)
		}
	} else {
		// Memory-only mode: list resident documents.
		docs := s.registry.All()
		resp.Total = len(docs)
		start := min((page-1)*size, len(docs))
		end := min(start+size, len(docs))
		for _, doc := range sortDocsByUpdated(docs)[start:end] {
			conns, bytes, lang, updated, expires := doc.Stats()
			resp.Documents = append(resp.Documents, adminDoc{
				ID:          doc.ID(),
				SizeBytes:   bytes,
				Language:    lang,
				Connections: conns,
				UpdatedAt:   updated.Unix(),
				ExpiresAt:   expires.Unix(),
			})
		}
	}
	if resp.Documents == nil {
		resp.Documents = []adminDoc{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func sortDocsByUpdated(docs []*document.Document) []*document.Document {
	sorted := make([]*document.Document, len(docs))
	copy(sorted, docs)
	for i := 1; i < len(sorted); i++ { // small n; insertion sort avoids extra imports
		for j := i; j > 0; j-- {
			_, _, _, uj, _ := sorted[j].Stats()
			_, _, _, uj1, _ := sorted[j-1].Stats()
			if !uj.After(uj1) {
				break
			}
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted
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
	data, err := distFS.ReadFile("dist/admin.html")
	if err != nil {
		http.Error(w, "admin page not built: run `cd frontend && npm run build`", http.StatusNotImplemented)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
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
