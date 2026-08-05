# momus-convert

[![Crates.io](https://img.shields.io/crates/v/momus-convert.svg)](https://crates.io/crates/momus-convert)
[![Docs.rs](https://img.shields.io/docsrs/momus-convert)](https://docs.rs/momus-convert)

**Convert API descriptions (OpenAPI, Postman, HAR, cURL, GraphQL, gRPC, FHIR) into Momus test plans.**

## What is this?

`momus-convert` translates existing API descriptions into executable Momus test plans. Instead of writing test plans by hand, you can convert your existing API specifications — OpenAPI 3.x specs, Postman collections, HAR files (browser DevTools exports), cURL commands, GraphQL SDL schemas, gRPC proto definitions, and FHIR Implementation Guides — into structured test plans ready to run.

## Key Features

- **OpenAPI 3.x** — convert OpenAPI specs (YAML/JSON) into test plans with endpoint coverage
- **Postman Collections** — convert Postman collection v2.1 exports into test plans
- **HAR Files** — convert HTTP Archive (browser DevTools export) files into test plans
- **cURL Commands** — parse cURL command strings into test plans
- **GraphQL SDL** — convert GraphQL schema definitions into test plans
- **gRPC Proto** — convert gRPC service definitions into test plans
- **FHIR IG** — convert FHIR Implementation Guide packages into conformance test plans (resource generation, profile validation, search, operations)

## Usage

```rust
use momus_convert::convert;

fn main() -> anyhow::Result<()> {
    // Convert an OpenAPI spec
    let plan = convert("openapi", "path/to/spec.yaml")?;
    println!("Plan: {} ({} tests)", plan.name, plan.total_tests());

    // Convert a cURL command
    let plan = convert("curl", "curl -X GET https://api.example.com/health")?;

    // Convert a HAR file
    let plan = convert("har", "path/to/traffic.har")?;

    // Convert a FHIR IG package
    let plan = convert("fhir", "path/to/ig.tar.gz")?;

    Ok(())
}
```

### Feature Flags

| Feature | Description | Default |
|---------|-------------|---------|
| `openapi` | OpenAPI 3.x converter | yes |
| `postman` | Postman Collection converter | yes |
| `har` | HAR file converter | yes |
| `curl` | cURL command converter | yes |
| `graphql` | GraphQL SDL converter | no |
| `grpc` | gRPC proto converter | no |
| `fhir` | FHIR IG converter | no |

---

Part of the [Momus](https://github.com/jlcoulter/momus) project — a generic API test harness with a composable assertion AST.
