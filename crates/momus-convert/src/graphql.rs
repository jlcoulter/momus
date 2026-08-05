use anyhow::{Context, Result};
use momus_core::ast::*;
use regex::Regex;
use std::collections::HashMap;

/// Convert a GraphQL SDL file to a TestPlan.
///
/// Reads a `.graphql`/`.gql` SDL file, extracts `type Query { ... }` and
/// `type Mutation { ... }` blocks, and generates one request step per field.
///
/// - Query fields → GET requests with the query as a JSON body
/// - Mutation fields → POST requests with the mutation as a JSON body
/// - All steps get a status-200 assertion
///
/// The generated URL is a placeholder (`http://localhost:4000/graphql`) that
/// users should override.
pub fn convert(path: &str) -> Result<TestPlan> {
    let content = std::fs::read_to_string(path)
        .with_context(|| format!("Failed to read GraphQL SDL file: {path}"))?;

    let query_fields = extract_fields(&content, "Query");
    let mutation_fields = extract_fields(&content, "Mutation");

    if query_fields.is_empty() && mutation_fields.is_empty() {
        anyhow::bail!("No `type Query` or `type Mutation` blocks found in GraphQL SDL: {path}");
    }

    let mut steps = Vec::new();

    for field in &query_fields {
        let query_body = build_query_body(field);
        let step = RequestStep {
            name: format!("query_{field}"),
            method: Method::Get,
            url: "http://localhost:4000/graphql".to_string(),
            headers: {
                let mut h = HashMap::new();
                h.insert("Content-Type".to_string(), "application/json".to_string());
                h
            },
            body: Some(serde_json::json!({ "query": query_body })),
            assert: vec![Assertion::Status(200)],
            save_as: String::new(),
            soft_fail: false,
        };
        steps.push(Step::Request(step));
    }

    for field in &mutation_fields {
        let mutation_body = build_mutation_body(field);
        let step = RequestStep {
            name: format!("mutation_{field}"),
            method: Method::Post,
            url: "http://localhost:4000/graphql".to_string(),
            headers: {
                let mut h = HashMap::new();
                h.insert("Content-Type".to_string(), "application/json".to_string());
                h
            },
            body: Some(serde_json::json!({ "query": mutation_body })),
            assert: vec![Assertion::Status(200)],
            save_as: String::new(),
            soft_fail: false,
        };
        steps.push(Step::Request(step));
    }

    let plan_name = format!(
        "GraphQL: {} queries, {} mutations from {}",
        query_fields.len(),
        mutation_fields.len(),
        path.rsplit('/').next().unwrap_or(path)
    );

    Ok(TestPlan {
        name: plan_name,
        base_url: String::new(),
        default_headers: HashMap::new(),
        steps,
        setup: vec![],
        teardown: vec![],
    })
}

/// Generate seed data setup steps from a GraphQL SDL file.
///
/// For each mutation field, generates a setup step that runs the mutation
/// with placeholder arguments to pre-populate the server.
pub fn generate_seed_data(path: &str) -> Result<Vec<Step>> {
    let content = std::fs::read_to_string(path)
        .with_context(|| format!("Failed to read GraphQL SDL file: {path}"))?;

    let mutation_fields = extract_fields(&content, "Mutation");

    let mut seed_steps = Vec::new();
    for field in &mutation_fields {
        let mutation_body = build_mutation_body(field);
        seed_steps.push(Step::Request(RequestStep {
            name: format!("seed_mutation_{field}"),
            method: Method::Post,
            url: "http://localhost:4000/graphql".to_string(),
            headers: {
                let mut h = HashMap::new();
                h.insert("Content-Type".to_string(), "application/json".to_string());
                h
            },
            body: Some(serde_json::json!({ "query": mutation_body })),
            assert: vec![Assertion::Status(200)],
            save_as: String::new(),
            soft_fail: true,
        }));
    }

    Ok(seed_steps)
}

/// Extract field names from a `type <Name> { ... }` block using regex.
///
/// Handles:
/// - Simple fields: `fieldName: ReturnType`
/// - Fields with args: `fieldName(arg: Type!): ReturnType`
/// - Fields with descriptions/comments
/// - Fields with directives: `fieldName @deprecated`
fn extract_fields(sdl: &str, type_name: &str) -> Vec<String> {
    // Find the type block: `type <Name> { ... }` — handle nested braces
    let pattern = format!(r"(?m)^\s*type\s+{}\s*\{{", regex_escape(type_name));
    let re = match Regex::new(&pattern) {
        Ok(r) => r,
        Err(_) => return vec![],
    };

    let start = match re.find(sdl) {
        Some(m) => m.end(),
        None => return vec![],
    };

    // Find the matching closing brace
    let mut depth = 1u32;
    let mut end = start;
    for (i, ch) in sdl[start..].char_indices() {
        match ch {
            '{' => depth += 1,
            '}' => {
                depth -= 1;
                if depth == 0 {
                    end = start + i;
                    break;
                }
            }
            _ => {}
        }
    }

    if depth != 0 {
        return vec![]; // Unmatched braces
    }

    let block = &sdl[start..end];

    // Extract field names: lines starting with optional whitespace,
    // followed by an identifier (field name), then optional args, then `:`
    let field_re = Regex::new(r"(?m)^\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*(?:\([^)]*\))?\s*:").unwrap();

    field_re
        .captures_iter(block)
        .map(|cap| cap[1].to_string())
        .collect()
}

/// Build a GraphQL query body for a given field.
///
/// Generates a simple query like:
/// ```graphql
/// query {
///   fieldName
/// }
/// ```
fn build_query_body(field: &str) -> String {
    format!("query {{\n  {}\n}}", field)
}

/// Build a GraphQL mutation body for a given field.
///
/// Generates a simple mutation like:
/// ```graphql
/// mutation {
///   fieldName
/// }
/// ```
fn build_mutation_body(field: &str) -> String {
    format!("mutation {{\n  {}\n}}", field)
}

/// Minimal regex escaping for literal type names.
fn regex_escape(s: &str) -> String {
    s.replace('\\', "\\\\")
        .replace('.', "\\.")
        .replace('+', "\\+")
        .replace('*', "\\*")
        .replace('?', "\\?")
        .replace('(', "\\(")
        .replace(')', "\\)")
        .replace('[', "\\[")
        .replace(']', "\\]")
        .replace('{', "\\{")
        .replace('}', "\\}")
        .replace('^', "\\^")
        .replace('$', "\\$")
        .replace('|', "\\|")
        .replace('-', "\\-")
}

#[cfg(test)]
mod tests {
    use super::*;

    const SAMPLE_SDL: &str = r#"
""" A simple GraphQL schema for testing """
schema {
  query: Query
  mutation: Mutation
}

type Query {
  """ Get a user by ID """
  user(id: ID!): User

  """ List all users """
  users(limit: Int, offset: Int): [User!]!

  """ Health check """
  health: HealthStatus
}

type Mutation {
  """ Create a new user """
  createUser(name: String!, email: String!): User

  """ Update an existing user """
  updateUser(id: ID!, name: String, email: String): User

  """ Delete a user """
  deleteUser(id: ID!): Boolean
}

type User {
  id: ID!
  name: String!
  email: String!
  posts: [Post!]!
}

type Post {
  id: ID!
  title: String!
  content: String!
}

type HealthStatus {
  status: String!
  uptime: Float!
}
"#;

    #[test]
    fn extract_query_fields() {
        let fields = extract_fields(SAMPLE_SDL, "Query");
        assert_eq!(fields, vec!["user", "users", "health"]);
    }

    #[test]
    fn extract_mutation_fields() {
        let fields = extract_fields(SAMPLE_SDL, "Mutation");
        assert_eq!(fields, vec!["createUser", "updateUser", "deleteUser"]);
    }

    #[test]
    fn extract_fields_nonexistent_type() {
        let fields = extract_fields(SAMPLE_SDL, "Subscription");
        assert!(fields.is_empty());
    }

    #[test]
    fn build_query_body_works() {
        let body = build_query_body("users");
        assert_eq!(body, "query {\n  users\n}");
    }

    #[test]
    fn build_mutation_body_works() {
        let body = build_mutation_body("createUser");
        assert_eq!(body, "mutation {\n  createUser\n}");
    }

    #[test]
    fn convert_sdl_to_test_plan() {
        use std::io::Write;
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{}", SAMPLE_SDL).unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();

        // 3 queries + 3 mutations = 6 steps
        assert_eq!(plan.steps.len(), 6);

        // Check query steps
        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.name, "query_user");
            assert_eq!(step.method, Method::Get);
            assert_eq!(step.url, "http://localhost:4000/graphql");
            assert_eq!(
                step.headers.get("Content-Type").unwrap(),
                "application/json"
            );
            let body = step.body.as_ref().unwrap();
            assert_eq!(body["query"], "query {\n  user\n}");
            assert!(
                step.assert
                    .iter()
                    .any(|a| matches!(a, Assertion::Status(200)))
            );
        } else {
            panic!("Expected Request step for query_user");
        }

        if let Step::Request(step) = &plan.steps[1] {
            assert_eq!(step.name, "query_users");
            assert_eq!(step.method, Method::Get);
            let body = step.body.as_ref().unwrap();
            assert_eq!(body["query"], "query {\n  users\n}");
        } else {
            panic!("Expected Request step for query_users");
        }

        if let Step::Request(step) = &plan.steps[2] {
            assert_eq!(step.name, "query_health");
            assert_eq!(step.method, Method::Get);
            let body = step.body.as_ref().unwrap();
            assert_eq!(body["query"], "query {\n  health\n}");
        } else {
            panic!("Expected Request step for query_health");
        }

        // Check mutation steps
        if let Step::Request(step) = &plan.steps[3] {
            assert_eq!(step.name, "mutation_createUser");
            assert_eq!(step.method, Method::Post);
            let body = step.body.as_ref().unwrap();
            assert_eq!(body["query"], "mutation {\n  createUser\n}");
            assert!(
                step.assert
                    .iter()
                    .any(|a| matches!(a, Assertion::Status(200)))
            );
        } else {
            panic!("Expected Request step for mutation_createUser");
        }

        if let Step::Request(step) = &plan.steps[4] {
            assert_eq!(step.name, "mutation_updateUser");
            assert_eq!(step.method, Method::Post);
            let body = step.body.as_ref().unwrap();
            assert_eq!(body["query"], "mutation {\n  updateUser\n}");
        } else {
            panic!("Expected Request step for mutation_updateUser");
        }

        if let Step::Request(step) = &plan.steps[5] {
            assert_eq!(step.name, "mutation_deleteUser");
            assert_eq!(step.method, Method::Post);
            let body = step.body.as_ref().unwrap();
            assert_eq!(body["query"], "mutation {\n  deleteUser\n}");
        } else {
            panic!("Expected Request step for mutation_deleteUser");
        }

        // Check plan name
        assert!(plan.name.contains("3 queries"));
        assert!(plan.name.contains("3 mutations"));
    }

    #[test]
    fn convert_empty_sdl_fails() {
        use std::io::Write;
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "type Foo {{ bar: String }}").unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let result = convert(&path);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("No `type Query` or `type Mutation`")
        );
    }

    #[test]
    fn convert_only_queries() {
        use std::io::Write;
        let sdl = r#"
type Query {
  ping: String
  version: String
}
"#;
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{}", sdl).unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        assert_eq!(plan.steps.len(), 2);
        for step in &plan.steps {
            if let Step::Request(s) = step {
                assert_eq!(s.method, Method::Get);
            } else {
                panic!("Expected all Request steps");
            }
        }
    }

    #[test]
    fn convert_only_mutations() {
        use std::io::Write;
        let sdl = r#"
type Mutation {
  doThing(input: String!): String
}
"#;
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{}", sdl).unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        assert_eq!(plan.steps.len(), 1);
        if let Step::Request(s) = &plan.steps[0] {
            assert_eq!(s.method, Method::Post);
            assert_eq!(s.name, "mutation_doThing");
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn extract_fields_with_directives() {
        let sdl = r#"
type Query {
  activeUsers: [User!]! @deprecated(reason: "use users with filter")
  users(limit: Int): [User!]!
}
"#;
        let fields = extract_fields(sdl, "Query");
        assert_eq!(fields, vec!["activeUsers", "users"]);
    }

    #[test]
    fn extract_fields_with_complex_args() {
        let sdl = r#"
type Query {
  search(query: String!, filters: [FilterInput!], sort: SortOrder = ASC): [Result!]!
}
"#;
        let fields = extract_fields(sdl, "Query");
        assert_eq!(fields, vec!["search"]);
    }

    #[test]
    fn plan_name_format() {
        use std::io::Write;
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{}", SAMPLE_SDL).unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        assert!(plan.name.starts_with("GraphQL:"));
        assert!(plan.name.contains("3 queries"));
        assert!(plan.name.contains("3 mutations"));
    }
}
