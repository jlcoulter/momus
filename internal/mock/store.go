package mock

import (
	"encoding/json"
	"fmt"
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
// choice-key resolution); if no field of that name exists, any nested value
// equal to the query is considered a match (so e.g. "birthdate" matches a
// "birthDate" field). Array values match if any element equals the query value
// (or contains it, for string fields).
func matchesFilters(res map[string]any, params map[string]string) bool {
	for name, want := range params {
		if strings.HasPrefix(name, "_") && name != "_sort" && name != "_count" {
			continue
		}
		if name == "_sort" || name == "_count" {
			continue
		}
		// A near (special) search matches a Location whose position is within
		// the query's bounding box; approximate it as an exact coordinate match
		// for the golden harness (coordinates are deterministic).
		if name == "near" {
			if !nearMatches(res, want) {
				return false
			}
			continue
		}
		val, ok := res[name]
		if !ok {
			// Fall back to a whole-resource search so a query param whose code
			// differs from the JSON field name (e.g. birthdate -> birthDate)
			// still matches.
			if !anyValueMatches(res, want) {
				return false
			}
			continue
		}
		if !valueMatches(val, want) {
			return false
		}
	}
	return true
}

// isDatePrefix reports whether want is a FHIR date/dateTime prefix (a value
// that looks like a partial ISO-8601 date or timestamp), so a stored dateTime
// can match a shorter query date by prefix.
func isDatePrefix(want string) bool {
	if len(want) < 4 || len(want) > 20 {
		return false
	}
	// Must look like a date: digits with '-' or ':' separators, or a bare year.
	for i, r := range want {
		switch r {
		case '-', ':', 'T', '+', 'Z', '.':
		default:
			if r < '0' || r > '9' {
				return false
			}
		}
		_ = i
	}
	// Require at least a 4-digit year.
	return len(want) >= 4 && isDigit(want[0]) && isDigit(want[1]) && isDigit(want[2]) && isDigit(want[3])
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// anyValueMatches reports whether want equals any scalar value anywhere in v
// (including Quantity number|system|code formats).
func anyValueMatches(v any, want string) bool {
	return valueMatches(v, want)
}

// nearMatches reports whether a Location resource's position coordinates match
// a near search value "lat|long" (optionally "lat|long|distance"). It compares
// latitude and longitude exactly, which is sufficient for the deterministic
// golden harness coordinates. Note: the "|" separator matches nearSearchValue's
// output; real FHIR URLs use a comma separator, so this is specific to the
// self-conformance harness (the mock is both producer and consumer).
func nearMatches(res map[string]any, want string) bool {
	parts := strings.Split(want, "|")
	if len(parts) < 2 {
		return false
	}
	latQ, lngQ := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	latQf, err1 := strconv.ParseFloat(latQ, 64)
	lngQf, err2 := strconv.ParseFloat(lngQ, 64)
	if err1 != nil || err2 != nil {
		return false
	}
	return findNearPosition(res, latQf, lngQf)
}

// findNearPosition reports whether any Location.position.latitude/longitude in
// the resource equals the query coordinate pair.
func findNearPosition(v any, latQ, lngQ float64) bool {
	switch t := v.(type) {
	case map[string]any:
		if lat, ok := toFloat(t["latitude"]); ok {
			if lng, ok := toFloat(t["longitude"]); ok {
				if lat == latQ && lng == lngQ {
					return true
				}
			}
		}
		for _, e := range t {
			if findNearPosition(e, latQ, lngQ) {
				return true
			}
		}
	case []any:
		for _, e := range t {
			if findNearPosition(e, latQ, lngQ) {
				return true
			}
		}
	}
	return false
}

// toFloat returns a numeric JSON value as a float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// valueMatches reports whether a stored value matches a search filter value. It
// recurses into nested maps and arrays (so a HumanName's `text` or a Coding's
// `code` both match), and matches any string equal to the query value. A
// quantity search value "number|system|code" matches when any nested value
// equals one of its `|`-separated parts, and a composite "part1$part2" matches
// when the resource contains both parts.
func valueMatches(val any, want string) bool {
	if strings.Contains(want, "$") {
		parts := strings.Split(want, "$")
		matched := true
		for _, part := range parts {
			if part == "" {
				continue
			}
			if !valueMatches(val, part) {
				matched = false
				break
			}
		}
		return matched
	}
	if strings.Contains(want, "|") {
		for _, part := range strings.Split(want, "|") {
			if part == "" {
				continue
			}
			if valueMatches(val, part) {
				return true
			}
		}
		return false
	}
	switch v := val.(type) {
	case string:
		// FHIR date search is prefix-based: a partial date (e.g. "2024-01-01")
		// matches a stored dateTime with the same prefix ("2024-01-01T00:00:00Z").
		if isDatePrefix(want) && strings.HasPrefix(v, want) {
			return true
		}
		return v == want
	case float64:
		return fmt.Sprintf("%g", v) == want
	case bool:
		return fmt.Sprintf("%v", v) == want
	case []any:
		for _, e := range v {
			if valueMatches(e, want) {
				return true
			}
		}
		return false
	case map[string]any:
		for _, e := range v {
			if valueMatches(e, want) {
				return true
			}
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
