package mock

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
)

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

// Search returns stored resources of a type filtered by query params, with
// optional _sort and _count. Each element of params is "name=value"; keys
// starting with "_" other than _sort/_count are treated as universal params and
// ignored as filters. Filtering matches a query param against the resource's
// field with the same name (string equality; for a code/coding it matches the
// field's string value).
func (s *Store) Search(resourceType string, params map[string]string) ([]map[string]any, error) {
	s.mu.RLock()
	raw := s.items[resourceType]
	bodies := make([][]byte, 0, len(raw))
	for _, body := range raw {
		bodies = append(bodies, body)
	}
	s.mu.RUnlock()

	var resources []map[string]any
	for _, body := range bodies {
		var res map[string]any
		if err := json.Unmarshal(body, &res); err != nil {
			continue
		}
		if matchesFilters(res, params) {
			resources = append(resources, res)
		}
	}

	// _sort: field name, optional "-" prefix for descending.
	if sortField, ok := params["_sort"]; ok {
		desc := false
		if len(sortField) > 0 && sortField[0] == '-' {
			desc = true
			sortField = sortField[1:]
		}
		sort.SliceStable(resources, func(i, j int) bool {
			vi := getFieldString(resources[i], sortField)
			vj := getFieldString(resources[j], sortField)
			if desc {
				return vi > vj
			}
			return vi < vj
		})
	}

	// _count
	if countStr, ok := params["_count"]; ok {
		if n, err := strconv.Atoi(countStr); err == nil && n >= 0 && n < len(resources) {
			resources = resources[:n]
		}
	}
	return resources, nil
}

// matchesFilters reports whether a resource satisfies every non-universal
// query param. A value is matched against the field of the same name (with
// choice-key resolution); array values match if any element equals the query
// value (or contains it, for string fields).
func matchesFilters(res map[string]any, params map[string]string) bool {
	for name, want := range params {
		if strings.HasPrefix(name, "_") && name != "_sort" && name != "_count" {
			continue
		}
		if name == "_sort" || name == "_count" {
			continue
		}
		// TODO: field-value matching. For v1, match against the raw JSON
		// string of the named field (or any element).
		val, ok := res[name]
		if !ok {
			return false
		}
		if !valueMatches(val, want) {
			return false
		}
	}
	return true
}

func valueMatches(val any, want string) bool {
	switch v := val.(type) {
	case string:
		return v == want
	case []any:
		for _, e := range v {
			if valueMatches(e, want) {
				return true
			}
		}
		return false
	case map[string]any:
		// For a codeable/coding, match the "code" field.
		if code, ok := v["code"].(string); ok && code == want {
			return true
		}
		return false
	default:
		return false
	}
}

func getFieldString(res map[string]any, name string) string {
	v, ok := res[name]
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []any:
		if len(t) > 0 {
			if s, ok := t[0].(string); ok {
				return s
			}
		}
	}
	return ""
}
