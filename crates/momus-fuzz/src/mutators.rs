//! Built-in mutators for payload fuzzing.
//!
//! Each mutator implements the `Mutator` trait and produces a specific
//! class of mutation on a valid JSON payload.

use crate::Mutator;
use serde_json::{Map, Value};

// ---------------------------------------------------------------------------
// BoundaryMutator
// ---------------------------------------------------------------------------

/// Produces boundary-value mutations: empty strings, very long strings,
/// zero/negative/NaN numbers, extreme dates, null values.
pub struct BoundaryMutator;

impl Mutator for BoundaryMutator {
    fn name(&self) -> &'static str {
        "boundary"
    }

    fn mutate(&self, base: &Value, seed: u64) -> Value {
        let mut rng = SimpleRng::new(seed);
        mutate_value(base, &mut rng, &BoundaryStrategy)
    }
}

struct BoundaryStrategy;

impl MutationStrategy for BoundaryStrategy {
    fn mutate_scalar(&self, value: &Value, rng: &mut SimpleRng) -> Value {
        match value {
            Value::String(s) => {
                let choices: [&[u8]; 4] = [b"", b"\0", &vec![b'A'; 65536], s.as_bytes()];
                let idx = rng.next() as usize % choices.len();
                Value::String(String::from_utf8_lossy(choices[idx]).to_string())
            }
            Value::Number(n) => {
                let choices: [f64; 5] = [
                    0.0,
                    -1.0,
                    f64::NAN,
                    f64::INFINITY,
                    n.as_f64().unwrap_or(0.0),
                ];
                let idx = rng.next() as usize % choices.len();
                serde_json::json!(choices[idx])
            }
            Value::Bool(_) => Value::Null,
            Value::Null => Value::Null,
            Value::Array(_) | Value::Object(_) => value.clone(),
        }
    }
}

// ---------------------------------------------------------------------------
// EncodingMutator
// ---------------------------------------------------------------------------

/// Produces encoding-related mutations: JSON injection, deeply nested objects,
/// duplicate keys, unicode normalization attacks, null bytes.
pub struct EncodingMutator;

impl Mutator for EncodingMutator {
    fn name(&self) -> &'static str {
        "encoding"
    }

    fn mutate(&self, base: &Value, seed: u64) -> Value {
        let mut rng = SimpleRng::new(seed);
        mutate_value(base, &mut rng, &EncodingStrategy)
    }
}

struct EncodingStrategy;

impl MutationStrategy for EncodingStrategy {
    fn mutate_scalar(&self, value: &Value, rng: &mut SimpleRng) -> Value {
        match value {
            Value::String(s) => {
                let choices: [&str; 5] = [
                    "{{constructor}}",
                    "\u{202E}",
                    "\u{0000}",
                    &s.replace('<', "\\u003c"),
                    s,
                ];
                let idx = rng.next() as usize % choices.len();
                Value::String(choices[idx].to_string())
            }
            _ => value.clone(),
        }
    }

    fn post_process_object(&self, obj: &mut Map<String, Value>, rng: &mut SimpleRng) {
        // Add a deeply nested key
        if rng.next().is_multiple_of(3) {
            let mut target = obj;
            for _ in 0..10 {
                let key = format!("_deep_{}", rng.next());
                target.insert(key.clone(), Value::Object(Map::new()));
                target = target
                    .get_mut(&key)
                    .and_then(|v| v.as_object_mut())
                    .unwrap();
            }
            target.insert("value".into(), Value::String("deeply nested".into()));
        }
    }
}

// ---------------------------------------------------------------------------
// TypeMismatchMutator
// ---------------------------------------------------------------------------

/// Swaps types: string where number expected, array where object expected,
/// boolean where string expected, etc.
pub struct TypeMismatchMutator;

impl Mutator for TypeMismatchMutator {
    fn name(&self) -> &'static str {
        "type_mismatch"
    }

    fn mutate(&self, base: &Value, seed: u64) -> Value {
        let mut rng = SimpleRng::new(seed);
        mutate_value(base, &mut rng, &TypeMismatchStrategy)
    }
}

struct TypeMismatchStrategy;

impl MutationStrategy for TypeMismatchStrategy {
    fn mutate_scalar(&self, value: &Value, rng: &mut SimpleRng) -> Value {
        let choices: [Value; 5] = [
            Value::Null,
            Value::Bool(true),
            Value::Number(serde_json::Number::from(0)),
            Value::String("".into()),
            value.clone(),
        ];
        let idx = rng.next() as usize % choices.len();
        choices[idx].clone()
    }

    fn mutate_array(&self, _value: &Value, rng: &mut SimpleRng) -> Value {
        let choices: [Value; 3] = [
            Value::Null,
            Value::Object(Map::new()),
            Value::String("[]".into()),
        ];
        let idx = rng.next() as usize % choices.len();
        choices[idx].clone()
    }

    fn mutate_object(&self, _value: &Value, rng: &mut SimpleRng) -> Value {
        let choices: [Value; 3] = [
            Value::Null,
            Value::Array(vec![]),
            Value::String("{}".into()),
        ];
        let idx = rng.next() as usize % choices.len();
        choices[idx].clone()
    }
}

// ---------------------------------------------------------------------------
// CardinalityMutator
// ---------------------------------------------------------------------------

/// Modifies cardinality: removes required fields, duplicates array elements,
/// adds unexpected fields, empties arrays.
pub struct CardinalityMutator;

impl Mutator for CardinalityMutator {
    fn name(&self) -> &'static str {
        "cardinality"
    }

    fn mutate(&self, base: &Value, seed: u64) -> Value {
        let mut rng = SimpleRng::new(seed);
        mutate_value(base, &mut rng, &CardinalityStrategy)
    }
}

struct CardinalityStrategy;

impl MutationStrategy for CardinalityStrategy {
    fn post_process_object(&self, obj: &mut Map<String, Value>, rng: &mut SimpleRng) {
        // Remove a random field
        if !obj.is_empty() && rng.next().is_multiple_of(2) {
            let keys: Vec<String> = obj.keys().cloned().collect();
            let idx = rng.next() as usize % keys.len();
            obj.remove(&keys[idx]);
        }
        // Add an unexpected field
        if rng.next().is_multiple_of(3) {
            obj.insert(
                format!("_unexpected_{}", rng.next()),
                Value::String("unexpected".into()),
            );
        }
    }

    fn post_process_array(&self, arr: &mut Vec<Value>, rng: &mut SimpleRng) {
        if arr.is_empty() {
            return;
        }
        // Duplicate a random element
        if rng.next().is_multiple_of(2) {
            let idx = rng.next() as usize % arr.len();
            arr.push(arr[idx].clone());
        }
        // Or clear the array
        if rng.next().is_multiple_of(5) {
            arr.clear();
        }
    }
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

/// A simple deterministic PRNG for reproducible mutations.
struct SimpleRng(u64);

impl SimpleRng {
    fn new(seed: u64) -> Self {
        Self(seed)
    }

    fn next(&mut self) -> u64 {
        self.0 = self
            .0
            .wrapping_mul(6364136223846793005)
            .wrapping_add(1442695040888963407);
        self.0 >> 33
    }
}

trait MutationStrategy {
    /// Mutate a scalar value (string, number, bool, null).
    fn mutate_scalar(&self, value: &Value, rng: &mut SimpleRng) -> Value {
        let _ = (value, rng);
        Value::Null
    }

    /// Mutate an array value (default: replace with null).
    fn mutate_array(&self, _value: &Value, rng: &mut SimpleRng) -> Value {
        let _ = rng;
        Value::Null
    }

    /// Mutate an object value (default: replace with null).
    fn mutate_object(&self, _value: &Value, rng: &mut SimpleRng) -> Value {
        let _ = rng;
        Value::Null
    }

    /// Post-process an object after recursive mutation.
    fn post_process_object(&self, _obj: &mut Map<String, Value>, _rng: &mut SimpleRng) {}

    /// Post-process an array after recursive mutation.
    fn post_process_array(&self, _arr: &mut Vec<Value>, _rng: &mut SimpleRng) {}
}

fn mutate_value(value: &Value, rng: &mut SimpleRng, strategy: &dyn MutationStrategy) -> Value {
    match value {
        Value::Null | Value::Bool(_) | Value::String(_) | Value::Number(_) => {
            strategy.mutate_scalar(value, rng)
        }
        Value::Array(arr) => {
            if rng.next().is_multiple_of(4) {
                return strategy.mutate_array(value, rng);
            }
            let mut new_arr: Vec<Value> =
                arr.iter().map(|v| mutate_value(v, rng, strategy)).collect();
            strategy.post_process_array(&mut new_arr, rng);
            Value::Array(new_arr)
        }
        Value::Object(obj) => {
            if rng.next().is_multiple_of(4) {
                return strategy.mutate_object(value, rng);
            }
            let mut new_obj: Map<String, Value> = obj
                .iter()
                .map(|(k, v)| (k.clone(), mutate_value(v, rng, strategy)))
                .collect();
            strategy.post_process_object(&mut new_obj, rng);
            Value::Object(new_obj)
        }
    }
}

/// Return all built-in mutators.
pub fn all_mutators() -> Vec<Box<dyn Mutator>> {
    vec![
        Box::new(BoundaryMutator),
        Box::new(EncodingMutator),
        Box::new(TypeMismatchMutator),
        Box::new(CardinalityMutator),
    ]
}

/// Look up a mutator by name.
pub fn mutator_by_name(name: &str) -> Option<Box<dyn Mutator>> {
    match name {
        "boundary" => Some(Box::new(BoundaryMutator)),
        "encoding" => Some(Box::new(EncodingMutator)),
        "type_mismatch" => Some(Box::new(TypeMismatchMutator)),
        "cardinality" => Some(Box::new(CardinalityMutator)),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_boundary_mutator_name() {
        assert_eq!(BoundaryMutator.name(), "boundary");
    }

    #[test]
    fn test_boundary_mutator_changes_value() {
        let base = json!({"name": "hello", "count": 42});
        let mutated = BoundaryMutator.mutate(&base, 1);
        // At least one field should differ
        assert_ne!(base, mutated);
    }

    #[test]
    fn test_boundary_mutator_deterministic() {
        let base = json!({"name": "hello"});
        let a = BoundaryMutator.mutate(&base, 42);
        let b = BoundaryMutator.mutate(&base, 42);
        assert_eq!(a, b);
    }

    #[test]
    fn test_encoding_mutator_name() {
        assert_eq!(EncodingMutator.name(), "encoding");
    }

    #[test]
    fn test_type_mismatch_mutator_name() {
        assert_eq!(TypeMismatchMutator.name(), "type_mismatch");
    }

    #[test]
    fn test_cardinality_mutator_name() {
        assert_eq!(CardinalityMutator.name(), "cardinality");
    }

    #[test]
    fn test_all_mutators_returns_four() {
        let mutators = all_mutators();
        assert_eq!(mutators.len(), 4);
    }

    #[test]
    fn test_mutator_by_name() {
        assert!(mutator_by_name("boundary").is_some());
        assert!(mutator_by_name("encoding").is_some());
        assert!(mutator_by_name("type_mismatch").is_some());
        assert!(mutator_by_name("cardinality").is_some());
        assert!(mutator_by_name("nonexistent").is_none());
    }

    #[test]
    fn test_cardinality_removes_field() {
        let base = json!({"a": 1, "b": 2, "c": 3});
        let mutated = CardinalityMutator.mutate(&base, 0);
        // With seed 0, the first field should be removed
        assert!(mutated.as_object().is_some_and(|o| o.len() < 3));
    }

    #[test]
    fn test_simple_rng_deterministic() {
        let mut a = SimpleRng::new(42);
        let mut b = SimpleRng::new(42);
        assert_eq!(a.next(), b.next());
        assert_eq!(a.next(), b.next());
    }
}
