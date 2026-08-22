package mock

import "sync"

// Store is an in-memory resource store keyed by resource type and id. It gives
// the mock real FHIR semantics: PUT/POST stores a resource, GET retrieves it,
// and DELETE removes it, so create→read→update→delete sequences behave like a
// real server.
type Store struct {
	mu    sync.RWMutex
	items map[string]map[string][]byte // resourceType -> id -> raw JSON body
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{items: make(map[string]map[string][]byte)}
}

// Put stores the raw body for a resource type and id.
func (s *Store) Put(resourceType, id string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items[resourceType] == nil {
		s.items[resourceType] = make(map[string][]byte)
	}
	s.items[resourceType][id] = body
}

// Get returns the stored body for a resource type and id, and whether it exists.
func (s *Store) Get(resourceType, id string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	body, ok := s.items[resourceType][id]
	return body, ok
}

// Delete removes a resource type and id from the store.
func (s *Store) Delete(resourceType, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items[resourceType], id)
}

// List returns every stored body for a resource type.
func (s *Store) List(resourceType string) [][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([][]byte, 0, len(s.items[resourceType]))
	for _, body := range s.items[resourceType] {
		out = append(out, body)
	}
	return out
}
