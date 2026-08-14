package server

import (
	"context"
	"time"

	"gopad/internal/document"
	"gopad/internal/store"
)

// loadFromStore restores a document snapshot from SQLite; expired rows are
// treated as absent.
func (s *Server) loadFromStore(id string) *document.Document {
	row, err := s.store.Load(id)
	if err != nil {
		s.log.Error("load document", "id", id, "err", err)
		return nil
	}
	if row == nil || row.ExpiresAt < time.Now().Unix() {
		return nil
	}
	return document.FromSnapshot(document.Snapshot{
		ID:         row.ID,
		ReadonlyID: row.ReadonlyID,
		Text:       row.Text,
		Language:   row.Language,
		TTL:        time.Duration(row.TTLSeconds) * time.Second,
		CreatedAt:  time.Unix(row.CreatedAt, 0),
		UpdatedAt:  time.Unix(row.UpdatedAt, 0),
	}, s.registry.MaxDocSize)
}

// saveDoc snapshots one document to SQLite if it has unsaved changes.
func (s *Server) saveDoc(doc *document.Document) {
	if s.store == nil {
		return
	}
	snap, dirty := doc.TakeSnapshot()
	if !dirty {
		return
	}
	if err := s.store.Save(snapshotRow(snap)); err != nil {
		s.log.Error("save document", "id", snap.ID, "err", err)
		doc.MarkDirty()
	}
}

func snapshotRow(snap document.Snapshot) store.Row {
	return store.Row{
		ID:         snap.ID,
		ReadonlyID: snap.ReadonlyID,
		Text:       snap.Text,
		Language:   snap.Language,
		TTLSeconds: int64(snap.TTL / time.Second),
		CreatedAt:  snap.CreatedAt.Unix(),
		UpdatedAt:  snap.UpdatedAt.Unix(),
		ExpiresAt:  snap.UpdatedAt.Add(snap.TTL).Unix(),
	}
}

// Flush persists every dirty resident document. Called periodically and on
// shutdown.
func (s *Server) Flush() {
	for _, doc := range s.registry.All() {
		s.saveDoc(doc)
	}
}

// sweep removes expired documents (memory and store) and evicts idle,
// fully-persisted documents from memory.
func (s *Server) sweep() {
	now := time.Now()
	for _, doc := range s.registry.All() {
		switch {
		case doc.Expired(now):
			doc.Kill("expired")
			s.registry.Remove(doc.ID())
			if s.store != nil {
				if err := s.store.Delete(doc.ID()); err != nil {
					s.log.Error("delete expired document", "id", doc.ID(), "err", err)
				}
			}
			s.log.Info("expired document removed", "id", doc.ID())
		case s.store != nil && doc.Evictable(now, s.evictAfter):
			s.registry.Remove(doc.ID())
		}
	}
	if s.store != nil {
		if n, err := s.store.DeleteExpired(now.Unix()); err != nil {
			s.log.Error("delete expired rows", "err", err)
		} else if n > 0 {
			s.log.Info("expired rows deleted", "count", n)
		}
	}
}

// Run drives the background flush and sweep loops until ctx is cancelled,
// then flushes everything one final time.
func (s *Server) Run(ctx context.Context) {
	flush := time.NewTicker(s.flushInterval)
	sweep := time.NewTicker(s.sweepInterval)
	defer flush.Stop()
	defer sweep.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Flush()
			return
		case <-flush.C:
			s.Flush()
		case <-sweep.C:
			s.sweep()
		}
	}
}
