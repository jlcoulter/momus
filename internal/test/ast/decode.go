package ast

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// DecodeNode is the inverse of EncodeNode. It switches on the "type" tag and
// reconstructs the concrete node. Unknown or missing types produce an error.
func DecodeNode(raw map[string]any) (Node, error) {
	typeTag, ok := raw["type"]
	if !ok {
		return nil, fmt.Errorf("decode node: missing \"type\" tag")
	}
	typeStr, ok := typeTag.(string)
	if !ok {
		return nil, fmt.Errorf("decode node: \"type\" tag must be a string, got %T", typeTag)
	}

	switch typeStr {
	case "sequence":
		steps, err := decodeSteps(raw)
		if err != nil {
			return nil, fmt.Errorf("decode sequence: %w", err)
		}
		return &Sequence{Steps: steps}, nil
	case "parallel":
		steps, err := decodeSteps(raw)
		if err != nil {
			return nil, fmt.Errorf("decode parallel: %w", err)
		}
		return &Parallel{Steps: steps}, nil
	case "request":
		return decodeRequest(raw)
	case "capture":
		name, _ := raw["name"].(string)
		path, _ := raw["path"].(string)
		return &Capture{Name: name, Path: path}, nil
	case "assert":
		return decodeAssert(raw)
	default:
		return nil, fmt.Errorf("decode node: unknown type %q", typeStr)
	}
}

// decodeSteps decodes the "steps" slice of a sequence or parallel node. Each
// element must be a JSON object (map[string]any); other types produce an error.
func decodeSteps(raw map[string]any) ([]Node, error) {
	stepsRaw, ok := raw["steps"]
	if !ok {
		return nil, fmt.Errorf("missing \"steps\"")
	}
	arr, ok := stepsRaw.([]any)
	if !ok {
		return nil, fmt.Errorf("\"steps\" must be a JSON array, got %T", stepsRaw)
	}
	steps := make([]Node, 0, len(arr))
	for i, el := range arr {
		elMap, ok := el.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("step %d must be a JSON object, got %T", i, el)
		}
		node, err := DecodeNode(elMap)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		steps = append(steps, node)
	}
	return steps, nil
}

func decodeRequest(raw map[string]any) (Node, error) {
	method, _ := raw["method"].(string)
	url, _ := raw["url"].(string)

	headers := map[string]string{}
	if headersRaw, ok := raw["headers"]; ok {
		switch hm := headersRaw.(type) {
		case map[string]string:
			for k, v := range hm {
				headers[k] = v
			}
		case map[string]any:
			for k, v := range hm {
				if s, ok := v.(string); ok {
					headers[k] = s
				} else {
					headers[k] = fmt.Sprintf("%v", v)
				}
			}
		}
	}

	var body any
	if b, ok := raw["body"]; ok {
		body = normalizeJSONValue(b)
	}

	return &Request{
		Method:  method,
		URL:     url,
		Headers: headers,
		Body:    body,
	}, nil
}

func decodeAssert(raw map[string]any) (Node, error) {
	description, _ := raw["description"].(string)
	requirementID, _ := raw["requirementId"].(string)
	expression, _ := raw["expression"].(string)

	var trace *Trace
	if traceRaw, ok := raw["requirement"]; ok && traceRaw != nil {
		traceMap, ok := traceRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("decode assert: \"requirement\" must be a JSON object, got %T", traceRaw)
		}
		t, err := decodeTrace(traceMap)
		if err != nil {
			return nil, fmt.Errorf("decode assert: %w", err)
		}
		trace = t
	}

	return &Assert{
		Description:   description,
		RequirementID: requirementID,
		Expression:    expression,
		Trace:         trace,
	}, nil
}

// decodeTrace is the inverse of encodeTrace. Missing fields default to zero
// values; it only errors if raw is nil.
func decodeTrace(raw map[string]any) (*Trace, error) {
	if raw == nil {
		return nil, fmt.Errorf("decode trace: raw is nil")
	}
	return &Trace{
		ConstraintID: getString(raw, "constraintId"),
		ProfileURL:   getString(raw, "profileUrl"),
		ResourceType: getString(raw, "resourceType"),
		ElementPath:  getString(raw, "elementPath"),
		Domain:       getString(raw, "domain"),
		Variant:      getString(raw, "variant"),
		Expected:     getString(raw, "expected"),
	}, nil
}

// normalizeJSONValue recursively converts numeric values in v to json.Number so
// that integers survive a JSON round-trip without being mangled through
// float64. Whole-number float64 values and integer types are preserved as
// json.Number; all other values are returned unchanged.
func normalizeJSONValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, e := range val {
			out[k] = normalizeJSONValue(e)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, e := range val {
			out[i] = normalizeJSONValue(e)
		}
		return out
	case float64:
		if isWholeNumber(val) {
			return json.Number(strconv.FormatFloat(val, 'f', -1, 64))
		}
		return val
	case int:
		return json.Number(strconv.Itoa(val))
	case int64:
		return json.Number(strconv.FormatInt(val, 10))
	case int32:
		return json.Number(strconv.FormatInt(int64(val), 10))
	case json.Number:
		return val
	default:
		return v
	}
}

// isWholeNumber reports whether f is an integer value (no fractional part).
func isWholeNumber(f float64) bool {
	return f == math.Trunc(f)
}

// getString reads a string field from a map, returning "" for missing or
// non-string values.
func getString(raw map[string]any, key string) string {
	if v, ok := raw[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
