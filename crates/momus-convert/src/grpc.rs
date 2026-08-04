use anyhow::{Context, Result};
use momus_core::ast::*;
use regex::Regex;
use std::collections::HashMap;

/// Convert a gRPC .proto file to a TestPlan.
///
/// Reads a `.proto` file, extracts `service` definitions and their `rpc` methods,
/// and generates one request step per RPC method.
///
/// Since the Momus runner is HTTP-based and cannot make native gRPC calls yet,
/// the generated steps document the RPC method signature in the body and use
/// a placeholder URL following the gRPC URL convention:
/// `{base_url}/{package}.{Service}/{Method}`
///
/// All steps use POST (gRPC always uses HTTP/2 POST) and get a status-200 assertion.
pub fn convert(path: &str) -> Result<TestPlan> {
    let content = std::fs::read_to_string(path)
        .with_context(|| format!("Failed to read .proto file: {path}"))?;

    let package = extract_package(&content);
    let services = extract_services(&content);

    if services.is_empty() {
        anyhow::bail!("No `service` definitions found in .proto file: {path}");
    }

    let mut steps = Vec::new();

    for (service_name, methods) in &services {
        for method in methods {
            let grpc_url = build_grpc_url(&package, service_name, method);
            let step = RequestStep {
                name: format!("rpc_{}_{}", service_name, method),
                method: Method::Post,
                url: grpc_url,
                headers: {
                    let mut h = HashMap::new();
                    h.insert("Content-Type".to_string(), "application/grpc".to_string());
                    h
                },
                body: Some(serde_json::json!({
                    "note": format!("gRPC method: {}.{} / {}", package, service_name, method),
                    "proto_file": path.rsplit('/').next().unwrap_or(path),
                })),
                assert: vec![Assertion::Status(200)],
                save_as: String::new(),
                soft_fail: false,
            };
            steps.push(Step::Request(step));
        }
    }

    let total_methods: usize = services.values().map(|m| m.len()).sum();
    let plan_name = format!(
        "gRPC: {} services, {} methods from {}",
        services.len(),
        total_methods,
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

/// Extract the `package` declaration from a .proto file.
///
/// Returns the package name (e.g. `"my.package.v1"`) or an empty string if none found.
fn extract_package(proto: &str) -> String {
    let re = match Regex::new(r"(?m)^\s*package\s+([a-zA-Z_][a-zA-Z0-9_.]*)\s*;") {
        Ok(r) => r,
        Err(_) => return String::new(),
    };
    re.captures(proto)
        .and_then(|cap| cap.get(1))
        .map(|m| m.as_str().to_string())
        .unwrap_or_default()
}

/// Extract service definitions and their RPC methods from a .proto file.
///
/// Returns a map of `service_name -> Vec<method_name>`.
///
/// Handles:
/// - `service Foo { rpc Bar(Request) returns (Response); }`
/// - `rpc Bar(Request) returns (Response) {}` (with body)
/// - `rpc Bar(stream Request) returns (stream Response);`
/// - Comments and whitespace between tokens
fn extract_services(proto: &str) -> HashMap<String, Vec<String>> {
    let mut services: HashMap<String, Vec<String>> = HashMap::new();

    // Find service blocks: `service <Name> { ... }`
    // We use a brace-depth approach to handle nested braces (e.g. option blocks, message blocks inside services)
    let service_start_re = match Regex::new(r"(?m)^\s*service\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\{") {
        Ok(r) => r,
        Err(_) => return services,
    };
    let rpc_re = match Regex::new(r"rpc\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(") {
        Ok(r) => r,
        Err(_) => return services,
    };

    for cap in service_start_re.captures_iter(proto) {
        let service_name = cap[1].to_string();
        // Position after the opening `{` — use the end of the full match
        let start = cap.get(0).map(|m| m.end()).unwrap_or(0);

        // Find the matching closing brace
        let mut depth = 1u32;
        let mut end = start;
        for (i, ch) in proto[start..].char_indices() {
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
            // Unmatched braces — skip this service
            continue;
        }

        let block = &proto[start..end];

        // Extract RPC methods from the service block
        let methods: Vec<String> = rpc_re
            .captures_iter(block)
            .map(|m| m[1].to_string())
            .collect();

        if !methods.is_empty() {
            services.insert(service_name, methods);
        }
    }

    services
}

/// Build a gRPC URL following the standard convention:
/// `{base_url}/{package}.{Service}/{Method}`
///
/// If no package is declared, the URL omits the package prefix.
fn build_grpc_url(package: &str, service: &str, method: &str) -> String {
    let base = "http://localhost:50051";
    if package.is_empty() {
        format!("{base}/{service}/{method}")
    } else {
        format!("{base}/{package}.{service}/{method}")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const SAMPLE_PROTO: &str = r#"
// Copyright 2024 Example Corp.
syntax = "proto3";

package my.package.v1;

// A simple user service
service UserService {
    // Get a user by ID
    rpc GetUser(GetUserRequest) returns (User);

    // List all users
    rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);

    // Create a new user
    rpc CreateUser(CreateUserRequest) returns (User);
}

service HealthService {
    // Health check
    rpc Check(HealthCheckRequest) returns (HealthCheckResponse);

    // Watch health status (server streaming)
    rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
}

message GetUserRequest {
    string user_id = 1;
}

message User {
    string id = 1;
    string name = 2;
    string email = 3;
}

message ListUsersRequest {
    int32 page_size = 1;
    string page_token = 2;
}

message ListUsersResponse {
    repeated User users = 1;
    string next_page_token = 2;
}

message CreateUserRequest {
    string name = 1;
    string email = 2;
}

message HealthCheckRequest {
    string service = 1;
}

message HealthCheckResponse {
    enum ServingStatus {
        UNKNOWN = 0;
        SERVING = 1;
        NOT_SERVING = 2;
    }
    ServingStatus status = 1;
}
"#;

    #[test]
    fn extract_package_name() {
        let pkg = extract_package(SAMPLE_PROTO);
        assert_eq!(pkg, "my.package.v1");
    }

    #[test]
    fn extract_package_empty_when_missing() {
        let proto = "syntax = \"proto3\";\n\nservice Foo { rpc Bar(Baz) returns (Qux); }";
        let pkg = extract_package(proto);
        assert_eq!(pkg, "");
    }

    #[test]
    fn extract_services_and_methods() {
        let services = extract_services(SAMPLE_PROTO);
        assert_eq!(services.len(), 2);

        let user_methods = services.get("UserService").unwrap();
        assert_eq!(user_methods, &vec!["GetUser", "ListUsers", "CreateUser"]);

        let health_methods = services.get("HealthService").unwrap();
        assert_eq!(health_methods, &vec!["Check", "Watch"]);
    }

    #[test]
    fn extract_services_no_services() {
        let proto = "syntax = \"proto3\";\n\nmessage Foo { string bar = 1; }";
        let services = extract_services(proto);
        assert!(services.is_empty());
    }

    #[test]
    fn build_grpc_url_with_package() {
        let url = build_grpc_url("my.package.v1", "UserService", "GetUser");
        assert_eq!(
            url,
            "http://localhost:50051/my.package.v1.UserService/GetUser"
        );
    }

    #[test]
    fn build_grpc_url_without_package() {
        let url = build_grpc_url("", "UserService", "GetUser");
        assert_eq!(url, "http://localhost:50051/UserService/GetUser");
    }

    #[test]
    fn convert_proto_to_test_plan() {
        use std::io::Write;
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{}", SAMPLE_PROTO).unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();

        // 3 UserService methods + 2 HealthService methods = 5 steps
        assert_eq!(plan.steps.len(), 5);

        // Find the UserService/GetUser step by name (HashMap iteration is non-deterministic)
        let get_user = plan
            .steps
            .iter()
            .find(|s| matches!(s, Step::Request(r) if r.name == "rpc_UserService_GetUser"))
            .expect("Expected rpc_UserService_GetUser step");

        if let Step::Request(step) = get_user {
            assert_eq!(step.method, Method::Post);
            assert_eq!(
                step.url,
                "http://localhost:50051/my.package.v1.UserService/GetUser"
            );
            assert_eq!(
                step.headers.get("Content-Type").unwrap(),
                "application/grpc"
            );
            let body = step.body.as_ref().unwrap();
            assert_eq!(
                body["note"],
                "gRPC method: my.package.v1.UserService / GetUser"
            );
            assert!(
                step.assert
                    .iter()
                    .any(|a| matches!(a, Assertion::Status(200)))
            );
        } else {
            panic!("Expected Request step for rpc_UserService_GetUser");
        }

        // Check second step: UserService/ListUsers
        if let Step::Request(step) = &plan.steps[1] {
            assert_eq!(step.name, "rpc_UserService_ListUsers");
            assert_eq!(step.method, Method::Post);
            assert_eq!(
                step.url,
                "http://localhost:50051/my.package.v1.UserService/ListUsers"
            );
        } else {
            panic!("Expected Request step for rpc_UserService_ListUsers");
        }

        // Check third step: UserService/CreateUser
        if let Step::Request(step) = &plan.steps[2] {
            assert_eq!(step.name, "rpc_UserService_CreateUser");
            assert_eq!(step.method, Method::Post);
            assert_eq!(
                step.url,
                "http://localhost:50051/my.package.v1.UserService/CreateUser"
            );
        } else {
            panic!("Expected Request step for rpc_UserService_CreateUser");
        }

        // Check fourth step: HealthService/Check
        if let Step::Request(step) = &plan.steps[3] {
            assert_eq!(step.name, "rpc_HealthService_Check");
            assert_eq!(step.method, Method::Post);
            assert_eq!(
                step.url,
                "http://localhost:50051/my.package.v1.HealthService/Check"
            );
        } else {
            panic!("Expected Request step for rpc_HealthService_Check");
        }

        // Check fifth step: HealthService/Watch (streaming)
        if let Step::Request(step) = &plan.steps[4] {
            assert_eq!(step.name, "rpc_HealthService_Watch");
            assert_eq!(step.method, Method::Post);
            assert_eq!(
                step.url,
                "http://localhost:50051/my.package.v1.HealthService/Watch"
            );
        } else {
            panic!("Expected Request step for rpc_HealthService_Watch");
        }

        // Check plan name
        assert!(plan.name.starts_with("gRPC:"));
        assert!(plan.name.contains("2 services"));
        assert!(plan.name.contains("5 methods"));
    }

    #[test]
    fn convert_proto_without_package() {
        use std::io::Write;
        let proto = r#"
syntax = "proto3";

service SimpleService {
    rpc DoThing(Input) returns (Output);
    rpc DoOther(OtherInput) returns (OtherOutput);
}
"#;
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{}", proto).unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        assert_eq!(plan.steps.len(), 2);

        if let Step::Request(step) = &plan.steps[0] {
            assert_eq!(step.name, "rpc_SimpleService_DoThing");
            // Without package, URL should omit the package prefix
            assert_eq!(step.url, "http://localhost:50051/SimpleService/DoThing");
        } else {
            panic!("Expected Request step");
        }
    }

    #[test]
    fn convert_empty_proto_fails() {
        use std::io::Write;
        let proto = "syntax = \"proto3\";\n\nmessage Foo { string bar = 1; }";
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{}", proto).unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let result = convert(&path);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("No `service` definitions found")
        );
    }

    #[test]
    fn convert_single_service() {
        use std::io::Write;
        let proto = r#"
service Greeter {
    rpc SayHello(HelloRequest) returns (HelloReply);
    rpc SayGoodbye(GoodbyeRequest) returns (GoodbyeReply);
}
"#;
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{}", proto).unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        assert_eq!(plan.steps.len(), 2);
        assert!(plan.name.contains("1 services"));
        assert!(plan.name.contains("2 methods"));
    }

    #[test]
    fn extract_services_with_streaming() {
        let proto = r#"
service StreamService {
    rpc ServerStream(Request) returns (stream Response);
    rpc ClientStream(stream Request) returns (Response);
    rpc BidiStream(stream Request) returns (stream Response);
}
"#;
        let services = extract_services(proto);
        let methods = services.get("StreamService").unwrap();
        assert_eq!(methods.len(), 3);
        assert_eq!(methods[0], "ServerStream");
        assert_eq!(methods[1], "ClientStream");
        assert_eq!(methods[2], "BidiStream");
    }

    #[test]
    fn extract_services_with_rpc_body() {
        // Some proto files have rpc with a body block
        let proto = r#"
service Foo {
    rpc Bar(Request) returns (Response) {
        option (google.api.http) = {
            post: "/v1/bar"
        };
    }
}
"#;
        let services = extract_services(proto);
        let methods = services.get("Foo").unwrap();
        assert_eq!(methods, &vec!["Bar"]);
    }

    #[test]
    fn plan_name_format() {
        use std::io::Write;
        let mut tmp = tempfile::NamedTempFile::new().unwrap();
        write!(tmp, "{}", SAMPLE_PROTO).unwrap();
        let path = tmp.path().to_str().unwrap().to_string();

        let plan = convert(&path).unwrap();
        assert!(plan.name.starts_with("gRPC:"));
        assert!(plan.name.contains("2 services"));
        assert!(plan.name.contains("5 methods"));
    }
}
