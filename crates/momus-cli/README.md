# momus-cli

[![Crates.io](https://img.shields.io/crates/v/momus-cli.svg)](https://crates.io/crates/momus-cli)
[![Docs.rs](https://img.shields.io/docsrs/momus-cli)](https://docs.rs/momus-cli)

**Momus API test harness — CLI runner.**

## What is this?

`momus-cli` is the command-line binary for the Momus API test harness. It provides a unified interface to all Momus capabilities: running and validating test plans, starting a mock server, load testing, fuzzing, chaos engineering, contract testing, security scanning, diff testing, and converting API descriptions into test plans.

## Commands

```text
Usage: momus <COMMAND>

Commands:
  run       Run a test plan from a JSON file
  validate  Validate a test plan JSON file
  mock      Start a mock server for testing
  bench     Load test a plan (steady, max-throughput, or soak)
  fuzz      Fuzz test a plan with payload mutations
  chaos     Run chaos experiments against a plan
  convert   Convert an API description into a test plan
  contract  Validate API responses against an OpenAPI/GraphQL spec
  guard     Security scan a plan for common vulnerabilities
  diff      Diff responses between two environments
```

### `momus run`

```bash
# Run with default output directory
momus run plan.json

# Override base URL
momus run plan.json --base-url http://other-server:3000

# Generate HTML report
momus run plan.json --output report.html
```

### `momus validate`

```bash
momus validate plan.json
# ✓ Valid test plan: 'health check'
#   Total tests: 3
#   Steps: 1
```

### `momus mock`

```bash
momus mock --port 8091
# Momus mock server listening on http://127.0.0.1:8091
```

### `momus bench`

```bash
# Steady load: 50 concurrent users for 60 seconds
momus bench plan.json --concurrency 50 --duration 60

# Generate HTML report
momus bench plan.json --concurrency 100 --duration 30 --output report.html
```

### `momus fuzz`

```bash
# Generate 5000 mutations
momus fuzz plan.json --iterations 5000
```

### `momus chaos`

```bash
# Run chaos experiments
momus chaos plan.json
```

### `momus convert`

```bash
# Convert a cURL command into a test plan
momus convert curl 'curl -X POST https://api.example.com/users -H "Content-Type: application/json" -d "{\"name\":\"test\"}"'

# Convert a HAR file into a test plan
momus convert har traffic.har

# Convert an OpenAPI spec into a test plan
momus convert openapi spec.yaml

# Convert a Postman collection into a test plan
momus convert postman collection.json

# Convert a GraphQL schema into a test plan
momus convert graphql schema.graphql

# Convert a gRPC proto into a test plan
momus convert grpc service.proto

# Convert a FHIR IG package into a test plan
momus convert fhir ig.tar.gz
```

### `momus contract`

```bash
# Validate responses against an OpenAPI spec
momus contract plan.json --spec openapi.yaml
```

### `momus guard`

```bash
# Security scan a plan
momus guard plan.json
```

### `momus diff`

```bash
# Diff responses between two environments
momus diff plan.json --baseline https://api-v1.example.com --target https://api-v2.example.com
```

---

Part of the [Momus](https://github.com/jlcoulter/momus) project — a generic API test harness with a composable assertion AST.
