/// Composable assertion nodes for API response validation.
///
/// Assertions form a tree: `AllOf`, `AnyOf`, and `Not` combine sub-assertions,
/// while leaf nodes check specific response properties.
///
/// # Examples
///
/// ```ignore
/// // Status is 200 AND body is a Bundle
/// Assertion::AllOf(vec![
///     Assertion::Status(200),
///     Assertion::JsonPath("$.resourceType", JsonPredicate::Eq(json!("Bundle"))),
/// ])
///
/// // Either 200 or 304 (conditional read)
/// Assertion::AnyOf(vec![
///     Assertion::Status(200),
///     Assertion::Status(304),
/// ])
/// ```
use serde::{Deserialize, Serialize};

// ---------------------------------------------------------------------------
// Top-level assertion tree
// ---------------------------------------------------------------------------

/// A composable response assertion.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Assertion {
    // -- Combinators --------------------------------------------------------
    /// All sub-assertions must pass (logical AND).
    AllOf(Vec<Assertion>),
    /// At least one sub-assertion must pass (logical OR).
    AnyOf(Vec<Assertion>),
    /// The sub-assertion must NOT pass (logical NOT).
    Not(Box<Assertion>),

    // -- HTTP-level assertions ---------------------------------------------
    /// Expected HTTP status code.
    Status(u16),
    /// Status code must be in this set.
    StatusIn(Vec<u16>),

    /// A response header must be present and match a predicate.
    Header {
        name: String,
        predicate: ValuePredicate,
    },

    /// Response body size in bytes.
    BodyLength(BodyLengthPredicate),

    // -- JSON body assertions -----------------------------------------------
    /// Assert a JSONPath expression against the response body.
    JsonPath {
        path: String,
        predicate: JsonPredicate,
    },

    /// Validate the response body against a JSON Schema.
    Schema {
        /// Inline JSON Schema.
        schema: serde_json::Value,
    },

    /// Response body must be valid JSON.
    ValidJson,

    // -- Content-type assertions -------------------------------------------
    /// Response Content-Type must match (substring match).
    ContentType(String),
}

// ---------------------------------------------------------------------------
// Predicates
// ---------------------------------------------------------------------------

/// Predicates for scalar values (headers, simple fields).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ValuePredicate {
    /// Exact string match.
    Eq(String),
    /// Substring / regex match.
    Contains(String),
    /// Regex match.
    Regex(String),
    /// Value is present (header exists).
    Present,
    /// Value is absent (header does not exist).
    Absent,
}

/// Predicates for JSONPath query results.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum JsonPredicate {
    /// The path must exist (returns at least one node).
    Exists,
    /// The path must NOT exist (returns zero nodes).
    NotExists,
    /// The first result must equal this value.
    Eq(serde_json::Value),
    /// The first result must NOT equal this value.
    NotEq(serde_json::Value),
    /// The first result (if numeric) must satisfy a comparison.
    Cmp { op: CmpOp, value: serde_json::Value },
    /// The result array must have this length.
    Length(LengthPredicate),
    /// Every result must satisfy this sub-predicate.
    Every(Box<JsonPredicate>),
    /// At least one result must satisfy this sub-predicate.
    Some(Box<JsonPredicate>),
    /// The result count must satisfy this.
    Count(CountPredicate),
    /// Match the result against a JSON Schema.
    Schema(serde_json::Value),
}

/// Comparison operators for numeric values.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CmpOp {
    Gt,
    Lt,
    Ge,
    Le,
}

/// Body length predicates.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum BodyLengthPredicate {
    /// Exact byte count.
    Eq(usize),
    /// Minimum byte count.
    Min(usize),
    /// Maximum byte count.
    Max(usize),
    /// Inclusive range.
    Range { min: usize, max: usize },
}

/// Array length predicates.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum LengthPredicate {
    Eq(usize),
    Min(usize),
    Max(usize),
    Range { min: usize, max: usize },
}

/// Count predicates (for JSONPath result counts).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CountPredicate {
    Eq(usize),
    Min(usize),
    Max(usize),
    Range { min: usize, max: usize },
}

// ---------------------------------------------------------------------------
// Convenience constructors
// ---------------------------------------------------------------------------

impl Assertion {
    /// Assert the response status is exactly `code`.
    pub fn status(code: u16) -> Self {
        Assertion::Status(code)
    }

    /// Assert the response status is one of the given codes.
    pub fn status_in(codes: Vec<u16>) -> Self {
        Assertion::StatusIn(codes)
    }

    /// Assert a JSONPath expression exists in the response body.
    pub fn json_path_exists(path: impl Into<String>) -> Self {
        Assertion::JsonPath {
            path: path.into(),
            predicate: JsonPredicate::Exists,
        }
    }

    /// Assert a JSONPath expression equals a value.
    pub fn json_path_eq(path: impl Into<String>, value: serde_json::Value) -> Self {
        Assertion::JsonPath {
            path: path.into(),
            predicate: JsonPredicate::Eq(value),
        }
    }

    /// Assert a response header matches a predicate.
    pub fn header(name: impl Into<String>, predicate: ValuePredicate) -> Self {
        Assertion::Header {
            name: name.into(),
            predicate,
        }
    }

    /// Assert the response Content-Type matches.
    pub fn content_type(ct: impl Into<String>) -> Self {
        Assertion::ContentType(ct.into())
    }

    /// Assert the response body is valid JSON.
    pub fn valid_json() -> Self {
        Assertion::ValidJson
    }

    /// Assert the response body matches a JSON Schema.
    pub fn schema(schema: serde_json::Value) -> Self {
        Assertion::Schema { schema }
    }
}

// ---------------------------------------------------------------------------
// Assertion evaluation result
// ---------------------------------------------------------------------------

/// The result of evaluating a single assertion.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AssertionResult {
    /// A human-readable description of what was checked.
    pub description: String,
    /// Whether the assertion passed.
    pub passed: bool,
    /// If failed, why.
    pub message: Option<String>,
    /// Nested sub-results (for AllOf/AnyOf/Not).
    #[serde(default)]
    pub children: Vec<AssertionResult>,
}

impl AssertionResult {
    pub fn pass(description: impl Into<String>) -> Self {
        Self {
            description: description.into(),
            passed: true,
            message: None,
            children: vec![],
        }
    }

    pub fn fail(description: impl Into<String>, message: impl Into<String>) -> Self {
        Self {
            description: description.into(),
            passed: false,
            message: Some(message.into()),
            children: vec![],
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn status_assertion_constructors() {
        let a = Assertion::status(200);
        assert_eq!(a, Assertion::Status(200));

        let b = Assertion::status_in(vec![200, 304]);
        assert_eq!(b, Assertion::StatusIn(vec![200, 304]));
    }

    #[test]
    fn json_path_constructors() {
        let a = Assertion::json_path_exists("$.resourceType");
        assert_eq!(
            a,
            Assertion::JsonPath {
                path: "$.resourceType".into(),
                predicate: JsonPredicate::Exists,
            }
        );

        let b = Assertion::json_path_eq("$.total", serde_json::json!(42));
        assert_eq!(
            b,
            Assertion::JsonPath {
                path: "$.total".into(),
                predicate: JsonPredicate::Eq(serde_json::json!(42)),
            }
        );
    }

    #[test]
    fn assertion_result_pass() {
        let r = AssertionResult::pass("status is 200");
        assert!(r.passed);
        assert!(r.message.is_none());
    }

    #[test]
    fn assertion_result_fail() {
        let r = AssertionResult::fail("status is 200", "got 404");
        assert!(!r.passed);
        assert_eq!(r.message.unwrap(), "got 404");
    }
}
