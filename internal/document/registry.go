package document

import (
	"sync"
	"time"

	"gopad/internal/ids"
)

// Registry tracks every document currently resident in memory.
type Registry struct {
	mu   sync.Mutex
	docs map[string]*Document

	// DefaultTTL is assigned to newly created documents.
	DefaultTTL time.Duration
	// MaxDocSize is the per-document size limit in bytes.
	MaxDocSize int
	// Loader, when set, is consulted on a miss before creating a new
	// document (used to restore snapshots from the store). It runs while
	// the registry lock is held, keeping load-vs-create race-free.
	Loader func(id string) *Document
}

func NewRegistry(defaultTTL time.Duration, maxDocSize int) *Registry {
	return &Registry{
		docs:       make(map[string]*Document),
		DefaultTTL: defaultTTL,
		MaxDocSize: maxDocSize,
	}
}

// Get returns the document with the given id, loading it through Loader if
// necessary. When create is true a missing document is created empty;
// otherwise nil is returned.
func (r *Registry) Get(id string, create bool) *Document {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.docs[id]; ok {
		return d
	}
	if r.Loader != nil {
		if d := r.Loader(id); d != nil {
			r.docs[id] = d
			return d
		}
	}
	if !create {
		return nil
	}
	d := New(id, ids.New(16), r.DefaultTTL, r.MaxDocSize)
	r.docs[id] = d
	return d
}

// Peek returns the resident document with the given id without loading or
// creating anything.
func (r *Registry) Peek(id string) *Document {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.docs[id]
}

// All returns a snapshot of every resident document.
func (r *Registry) All() []*Document {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Document, 0, len(r.docs))
	for _, d := range r.docs {
		out = append(out, d)
	}
	return out
}

// Remove drops a document from memory. It does not touch its connections;
// call Kill first when the document is being deleted rather than evicted.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.docs, id)
}
