package bulk

import (
	"hash/fnv"
	"regexp"
	"sort"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// Link builds a coherent, distributed web of resources from generated datasets.
//
// All instances are retained, grouped into a pool per resource type, and any
// dangling references (Type/unknown) are rewired to a real instance of that
// type, chosen deterministically from the source resource so references spread
// across the pool and several resources share a common target. The returned
// slice is deterministic: instances in dataset order.
func Link(datasets []*model.Dataset) []*model.ResourceInstance {
	var instances []*model.ResourceInstance
	pool := make(map[string][]*model.ResourceInstance)
	for _, ds := range datasets {
		if ds == nil {
			continue
		}
		// Sort each dataset's resources by local id so the output order is
		// deterministic (Go map iteration order is randomised).
		ids := make([]string, 0, len(ds.Resources))
		for id := range ds.Resources {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			inst := ds.Resources[id]
			if inst == nil || inst.Resource == nil {
				continue
			}
			instances = append(instances, inst)
			pool[inst.ResourceType] = append(pool[inst.ResourceType], inst)
		}
	}

	for _, inst := range instances {
		rewriteReferences(inst.Resource, inst.LocalID, pool)
	}
	return instances
}

// danglingRef matches a FHIR reference of the form Type/unknown or
// Type/momus-setup-*, our sentinels for an unresolved reference. The former is
// the historical bulk placeholder; the latter is produced by the shared
// generation core (generation.SynthesizeBody) via referencePlaceholder, which
// points at a setup resource that does not exist in a bulk corpus. URLs
// (http://…) and other non-resource strings do not match because the character
// after the type prefix is not a slash.
var danglingRef = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*/(unknown|momus-setup-[A-Za-z0-9-]+)$`)

// rewriteReferences replaces dangling Type/unknown references in a resource
// with a reference to a pool instance of the target type, distributed by a hash
// of the source id so different resources land on different pool members.
func rewriteReferences(value any, sourceID string, pool map[string][]*model.ResourceInstance) {
	switch typed := value.(type) {
	case map[string]any:
		if ref, ok := typed["reference"].(string); ok && danglingRef.MatchString(ref) {
			targetType := ref[:strings.IndexByte(ref, '/')]
			members := pool[targetType]
			if len(members) > 0 {
				idx := int(hashLink(sourceID+"|"+targetType)) % len(members)
				inst := members[idx]
				typed["reference"] = inst.ResourceType + "/" + inst.LocalID
				typed["type"] = inst.ResourceType
			}
		}
		for _, v := range typed {
			rewriteReferences(v, sourceID, pool)
		}
	case []any:
		for _, item := range typed {
			rewriteReferences(item, sourceID, pool)
		}
	}
}

func hashLink(seed string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return h.Sum32()
}
