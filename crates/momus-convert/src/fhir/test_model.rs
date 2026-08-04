#![allow(dead_code, clippy::type_complexity)]

//! FHIR test model types for generated test plans.
//!
//! These types are used by the test plan generator (ported from fhir-autotest)
//! to represent test cases, groups, and assertions. They are currently defined
//! here but consumed by the planner module which is ported in a follow-up.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// What to assert about a server response.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ResponseAssertion {
    #[serde(default)]
    pub bundle_type: Option<String>,
    #[serde(default)]
    pub min_entries: Option<usize>,
    #[serde(default)]
    pub max_entries: Option<usize>,
    #[serde(default)]
    pub resource_types: Vec<String>,
    #[serde(default)]
    pub field_values: HashMap<String, HashMap<String, serde_json::Value>>,
    #[serde(default)]
    pub include_types: HashMap<String, String>,
    #[serde(default)]
    pub include_requires_distinct_from: Option<String>,
    #[serde(default)]
    pub sort_by: Option<SortAssertion>,
    #[serde(default)]
    pub absent_fields: Vec<String>,
    #[serde(default)]
    pub outcome_severity: Option<String>,
    #[serde(default)]
    pub required_fields: HashMap<String, Vec<String>>,
    #[serde(default)]
    pub response_contains_key: Option<String>,
    #[serde(default)]
    pub response_resource_types: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct SortAssertion {
    pub field: String,
    pub direction: String,
}

impl ResponseAssertion {
    pub fn none() -> Self {
        Self {
            bundle_type: None,
            min_entries: None,
            max_entries: None,
            resource_types: Vec::new(),
            field_values: HashMap::new(),
            include_types: HashMap::new(),
            include_requires_distinct_from: None,
            sort_by: None,
            absent_fields: Vec::new(),
            outcome_severity: None,
            required_fields: HashMap::new(),
            response_contains_key: None,
            response_resource_types: Vec::new(),
        }
    }
}

/// Supported FHIR RESTful interactions.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum Interaction {
    Read,
    Vread,
    Update,
    Patch,
    Delete,
    Create,
    SearchType,
    HistoryInstance,
    HistoryType,
    Operation(String),
}

impl Interaction {
    pub fn from_code(code: &str) -> Option<Self> {
        match code {
            "read" => Some(Interaction::Read),
            "vread" => Some(Interaction::Vread),
            "update" => Some(Interaction::Update),
            "patch" => Some(Interaction::Patch),
            "delete" => Some(Interaction::Delete),
            "create" => Some(Interaction::Create),
            "search-type" => Some(Interaction::SearchType),
            "history-instance" => Some(Interaction::HistoryInstance),
            "history-type" => Some(Interaction::HistoryType),
            other => {
                tracing::warn!("Unknown interaction code '{other}', treating as operation");
                Some(Interaction::Operation(other.to_string()))
            }
        }
    }

    pub fn http_method(&self) -> &'static str {
        match self {
            Interaction::Read
            | Interaction::Vread
            | Interaction::SearchType
            | Interaction::HistoryInstance
            | Interaction::HistoryType => "GET",
            Interaction::Create => "POST",
            Interaction::Update => "PUT",
            Interaction::Patch => "PATCH",
            Interaction::Delete => "DELETE",
            Interaction::Operation(_) => "POST",
        }
    }

    pub fn label(&self) -> String {
        match self {
            Interaction::Operation(name) => format!("operation-{name}"),
            other => format!("{other:?}").to_lowercase(),
        }
    }
}

/// Search parameter modifiers per FHIR R4 spec.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum SearchModifier {
    Exact,
    Contains,
    Missing,
    Not,
    Above,
    Below,
    Text,
    In,
    NotIn,
    BelowType,
    AboveType,
}

impl SearchModifier {
    pub fn applicable_to(param_type: &str) -> Vec<SearchModifier> {
        let mut modifiers = vec![SearchModifier::Missing];
        match param_type {
            "string" => modifiers.extend([SearchModifier::Exact, SearchModifier::Contains]),
            "token" => modifiers.extend([
                SearchModifier::Not,
                SearchModifier::Text,
                SearchModifier::Above,
                SearchModifier::Below,
            ]),
            "reference" => modifiers.extend([SearchModifier::Above, SearchModifier::Below]),
            "uri" => modifiers.extend([SearchModifier::Above, SearchModifier::Below]),
            _ => {}
        }
        modifiers
    }

    pub fn suffix(&self) -> &str {
        match self {
            SearchModifier::Exact => ":exact",
            SearchModifier::Contains => ":contains",
            SearchModifier::Missing => ":missing",
            SearchModifier::Not => ":not",
            SearchModifier::Above => ":above",
            SearchModifier::Below => ":below",
            SearchModifier::Text => ":text",
            SearchModifier::In => ":in",
            SearchModifier::NotIn => ":not-in",
            SearchModifier::BelowType => ":below",
            SearchModifier::AboveType => ":above",
        }
    }
}

/// Search comparison prefixes for number/date/quantity params.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum SearchPrefix {
    Eq,
    Ne,
    Gt,
    Lt,
    Ge,
    Le,
    Sa,
    Eb,
    Ap,
}

impl SearchPrefix {
    pub fn applicable_to(param_type: &str) -> Vec<SearchPrefix> {
        match param_type {
            "number" | "quantity" => vec![
                SearchPrefix::Eq,
                SearchPrefix::Ne,
                SearchPrefix::Gt,
                SearchPrefix::Lt,
                SearchPrefix::Ge,
                SearchPrefix::Le,
            ],
            "date" | "dateTime" => vec![
                SearchPrefix::Eq,
                SearchPrefix::Ne,
                SearchPrefix::Gt,
                SearchPrefix::Lt,
                SearchPrefix::Ge,
                SearchPrefix::Le,
                SearchPrefix::Sa,
                SearchPrefix::Eb,
                SearchPrefix::Ap,
            ],
            _ => vec![],
        }
    }

    pub fn prefix_str(&self) -> &str {
        match self {
            SearchPrefix::Eq => "eq",
            SearchPrefix::Ne => "ne",
            SearchPrefix::Gt => "gt",
            SearchPrefix::Lt => "lt",
            SearchPrefix::Ge => "ge",
            SearchPrefix::Le => "le",
            SearchPrefix::Sa => "sa",
            SearchPrefix::Eb => "eb",
            SearchPrefix::Ap => "ap",
        }
    }
}

/// What kind of test case this is.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum TestCaseKind {
    Interaction,
    SearchSingle {
        param_name: String,
        param_type: String,
    },
    SearchModifier {
        param_name: String,
        modifier: SearchModifier,
    },
    SearchPrefix {
        param_name: String,
        prefix: SearchPrefix,
    },
    SearchNear {
        param: String,
    },
    SearchCombo {
        params: Vec<String>,
    },
    SearchChained {
        param: String,
        chain: String,
    },
    Include {
        param: String,
        revinclude: bool,
    },
    ResultParam {
        param: String,
    },
    Operation {
        code: String,
    },
    Negative {
        description: String,
    },
    Conformance {
        resource_type: String,
        profile_url: String,
    },
}

/// An HTTP request template for a test case.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HttpRequest {
    pub method: String,
    pub url: String,
    #[serde(default)]
    pub headers: HashMap<String, String>,
    pub body: Option<serde_json::Value>,
}

/// Validation specification for a test case response.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ValidationSpec {
    pub expected_status: u16,
    pub profile_url: Option<String>,
    #[serde(default)]
    pub required_elements: Vec<String>,
    #[serde(default)]
    pub forbidden_elements: Vec<String>,
    #[serde(default)]
    pub response_assertion: Option<ResponseAssertion>,
}

/// A single test case: one request + its validation criteria.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TestCase {
    pub name: String,
    pub kind: TestCaseKind,
    pub interaction: Interaction,
    pub resource_type: String,
    pub profile_url: Option<String>,
    pub request: HttpRequest,
    pub validation: ValidationSpec,
}

/// A group of test cases for one resource type.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TestGroup {
    pub resource_type: String,
    pub profile_url: Option<String>,
    pub tests: Vec<TestCase>,
}

/// The full FHIR test plan generated from an IG package.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FhirTestPlan {
    pub name: String,
    pub ig_url: Option<String>,
    #[serde(default)]
    pub test_groups: Vec<TestGroup>,
    #[serde(default)]
    pub creation_order: Vec<String>,
}

impl FhirTestPlan {
    pub fn total_tests(&self) -> usize {
        self.test_groups.iter().map(|g| g.tests.len()).sum()
    }
}
