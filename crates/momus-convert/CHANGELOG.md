# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
## [0.5.0] - 2026-08-05

### 🚀 Features

- Deepen FHIR test generation with _has, all-pair combos, expanded result params, and value resolution
- Wire up fhir-generate CLI subcommand for bulk NDJSON test data generation

### 🐛 Bug Fixes

- Review and improve cargo-husky pre-commit hook configuration (#34)
- Bump workspace version from 0.3.4 to 0.5.0

### ⚙️ Miscellaneous Tasks

- Release v0.3.4
## [0.3.4] - 2026-08-05

### 🚀 Features

- *(convert)* Implement cURL command to TestPlan converter
- *(convert)* Implement HAR file to TestPlan converter
- *(convert)* Port FHIR IG package parser and model types
- *(convert)* Complete FHIR IG package parser and model port
- *(fhir)* Port response assertions, validator, resource generator, and test plan generator
- *(convert)* Implement OpenAPI 3.x to TestPlan converter
- *(fhir)* Port profile resolver with parent chain resolution and registry download
- *(fhir)* Port value set resolution
- Phase 2-4 — FHIR near/chained/conformance tests, GraphQL converter
- *(convert)* Implement Postman Collection v2.1 converter
- Phase 3-5 — FHIR bulk data, chaos experiments, remaining tasks
- Complete v0.3.0 — bulk loader, HCPD/AU generation, FEATURES.md update
- Port remaining fhir-autotest modules (value_resolver, test_helpers, enhanced resource_gen)

### 🐛 Bug Fixes

- Remove readme field from crate manifests (no per-crate README.md exists)
- GRPC test uses find() for non-deterministic HashMap iteration
- All gRPC tests use find() for non-deterministic HashMap iteration
- Add readme field to all crate manifests
- Add version to all internal path dependencies for crates.io publishing

### 📚 Documentation

- Add README.md to all 11 crates, update main README for v0.4.0

### 🎨 Styling

- Cargo fmt pass across workspace
- Fix clippy warnings and fmt issues across workspace

### ⚙️ Miscellaneous Tasks

- Add /output to .gitignore, fix HashMap import in assertions test
- Bump all crates to v0.3.0 via workspace version inheritance
## [0.3.3] - 2026-08-05

### 🚀 Features

- *(convert)* Implement cURL command to TestPlan converter
- *(convert)* Implement HAR file to TestPlan converter
- *(convert)* Port FHIR IG package parser and model types
- *(convert)* Complete FHIR IG package parser and model port
- *(fhir)* Port response assertions, validator, resource generator, and test plan generator
- *(convert)* Implement OpenAPI 3.x to TestPlan converter
- *(fhir)* Port profile resolver with parent chain resolution and registry download
- *(fhir)* Port value set resolution
- Phase 2-4 — FHIR near/chained/conformance tests, GraphQL converter
- *(convert)* Implement Postman Collection v2.1 converter
- Phase 3-5 — FHIR bulk data, chaos experiments, remaining tasks
- Complete v0.3.0 — bulk loader, HCPD/AU generation, FEATURES.md update
- Port remaining fhir-autotest modules (value_resolver, test_helpers, enhanced resource_gen)

### 🐛 Bug Fixes

- Remove readme field from crate manifests (no per-crate README.md exists)
- GRPC test uses find() for non-deterministic HashMap iteration
- All gRPC tests use find() for non-deterministic HashMap iteration
- Add readme field to all crate manifests

### 📚 Documentation

- Add README.md to all 11 crates, update main README for v0.4.0

### 🎨 Styling

- Cargo fmt pass across workspace
- Fix clippy warnings and fmt issues across workspace

### ⚙️ Miscellaneous Tasks

- Add /output to .gitignore, fix HashMap import in assertions test
- Bump all crates to v0.3.0 via workspace version inheritance
