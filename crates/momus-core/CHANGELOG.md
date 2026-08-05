# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
## [0.5.0] - 2026-08-05

### 🚀 Features

- Add JUnit XML output for CI integration (#35)
## [0.4.0] - 2026-08-05

### 🚀 Features

- Add single-file config system with per-crate TOML sections
- Add {random.uuid}, {random.int}, and {random.string} template functions (#28)
- Add 'momus plan' subcommand to show all requests in a test plan
- Add per-command output directory config

### 🐛 Bug Fixes

- Add version to all internal path dependencies for crates.io publishing
- Upgrade momus-core rand from 0.8 to 0.9 to resolve cargo-deny duplicate ban
- Teardown steps now always run even if setup or main steps fail
- Bump workspace version from 0.3.4 to 0.5.0
- Downgrade crate versions from 0.5.0 to 0.3.3

### 🚜 Refactor

- Extract shared info-leak detection into momus-core (#84)

### ⚙️ Miscellaneous Tasks

- Release v0.5.0
## [0.5.0] - 2026-08-05

### 🚀 Features

- Add single-file config system with per-crate TOML sections
- Add {random.uuid}, {random.int}, and {random.string} template functions (#28)
- Add 'momus plan' subcommand to show all requests in a test plan
- Add per-command output directory config

### 🐛 Bug Fixes

- Add version to all internal path dependencies for crates.io publishing
- Upgrade momus-core rand from 0.8 to 0.9 to resolve cargo-deny duplicate ban
- Teardown steps now always run even if setup or main steps fail
- Bump workspace version from 0.3.4 to 0.5.0
## [0.3.4] - 2026-08-05

### 🐛 Bug Fixes

- Add version to all internal path dependencies for crates.io publishing
