# Contributing to Momus

Thank you for your interest in contributing to Momus! This guide covers everything
you need to get started — from setting up your development environment to opening
a pull request.

- [Prerequisites](#prerequisites)
- [Getting Started](#getting-started)
- [Building](#building)
- [Running Tests](#running-tests)
- [Code Quality](#code-quality)
  - [Formatting](#formatting)
  - [Linting](#linting)
- [Pre-commit Hooks](#pre-commit-hooks)
  - [What runs when](#what-runs-when)
  - [Installing hooks](#installing-hooks)
  - [Skipping hooks](#skipping-hooks)
- [Commit Message Conventions](#commit-message-conventions)
- [Pull Request Workflow](#pull-request-workflow)
- [Contributor License Agreement](#contributor-license-agreement)

---

## Prerequisites

- **Rust stable** — Momus uses the latest stable Rust toolchain. The project
  pins a minimum version in [`rust-toolchain.toml`](./rust-toolchain.toml)
  (`channel = "stable"`, edition 2024). If you use `rustup`, it will
  automatically select the correct channel when you enter the workspace.
- **No system OpenSSL required** — Momus uses `rustls` for TLS, so you don't
  need OpenSSL development headers installed. A stock Rust installation is
  sufficient.

## Getting Started

Clone the repository:

```sh
git clone https://github.com/jlcoulter/momus.git
cd momus
```

## Building

Build the entire workspace (all crates):

```sh
cargo build
```

To build only the CLI binary:

```sh
cargo build -p momus-cli
```

## Running Tests

Run the full test suite across all crates:

```sh
cargo test --all-targets
```

This includes unit tests, integration tests, doc tests, and benchmark tests.
The test suite is also executed automatically on every push via CI
([GitHub Actions](.github/workflows/ci.yml)).

## Code Quality

### Formatting

All Rust code must be formatted with `rustfmt`. Check formatting before
committing:

```sh
cargo fmt --check
```

To auto-format your code:

```sh
cargo fmt
```

### Linting

Run Clippy with the same strictness as CI:

```sh
cargo clippy --all-targets -- -D warnings
```

This treats all Clippy warnings as errors. The project aims to keep a clean
clippy slate at all times.

## Pre-commit Hooks

This project uses [cargo-husky](https://github.com/rhysd/cargo-husky) to manage
git hooks. The configuration lives in [`husky.toml`](./husky.toml) at the
workspace root.

### What runs when

| Hook | Commands | Purpose |
|------|----------|---------|
| **pre-commit** | `cargo fmt --check`, `cargo clippy --all-targets -- -D warnings`, `cargo check --all-targets` | Fast quality checks — formatting, linting, and compilation |
| **pre-push** | `cargo test --all-targets` | Full test suite before pushing |

### Installing hooks

Hooks are installed automatically when any crate is built with dev-dependencies
enabled. The simplest way to trigger installation:

```sh
cargo test
```

After installation, verify the hooks are in place:

```sh
ls -la .git/hooks/pre-commit .git/hooks/pre-push
```

### Skipping hooks

To bypass hooks for a particular commit or push:

```sh
git commit --no-verify -m "wip: ..."
git push --no-verify
```

## Commit Message Conventions

Momus follows the [Conventional Commits](https://www.conventionalcommits.org/)
specification. This enables automatic changelog generation via
[git-cliff](https://git-cliff.org) (see [`cliff.toml`](./cliff.toml)).

Commit messages should be structured as:

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

### Types

| Type | Usage |
|------|-------|
| `feat` | A new feature |
| `fix` | A bug fix |
| `docs` | Documentation changes |
| `perf` | Performance improvements |
| `refactor` | Code refactoring (no feature or fix) |
| `style` | Formatting, whitespace (no logic change) |
| `test` | Adding or updating tests |
| `chore` | Build, CI, dependencies, tooling |
| `ci` | CI configuration changes |
| `revert` | Reverting a previous commit |

### Examples

```
feat(cli): add --timeout flag to run command
fix(core): handle empty response body in assertion eval
docs: add CONTRIBUTING.md with setup and PR workflow
test(convert): add tests for OpenAPI converter
chore(deps): update tokio to 1.40
```

### Breaking changes

Append a `!` after the type/scope to indicate a breaking change:

```
feat(core)!: redesign assertion AST for v2
```

Breaking changes will appear prominently in the changelog.

## Pull Request Workflow

Momus uses a **trunk-based development** workflow:

1. **Create a feature branch** off `master`:
   ```sh
   git checkout master
   git pull
   git checkout -b feat/my-feature
   ```

   Use a descriptive branch name. Common prefixes:
   - `feat/` — new features
   - `fix/` — bug fixes
   - `docs/` — documentation
   - `refactor/` — refactoring
   - `test/` — testing
   - `chore/` — tooling, CI, dependencies

2. **Make your changes** and commit them following the
   [commit message conventions](#commit-message-conventions).

3. **Run quality checks** locally:
   ```sh
   cargo fmt --check
   cargo clippy --all-targets -- -D warnings
   cargo test --all-targets
   ```

4. **Push your branch**:
   ```sh
   git push origin feat/my-feature
   ```

5. **Open a pull request** against `master` on GitHub. Include:
   - A clear title following conventional commits format
   - A description of what the PR does and why
   - Reference to any related issues (e.g., `Closes #123`)

6. **Address review feedback** by pushing additional commits to the same branch.
   The pre-push hooks will run the full test suite automatically.

7. **Squash merge** — the maintainer will squash your commits into a single
   conventional commit when merging.

## Contributor License Agreement

Before your first pull request can be merged, you must sign the
[Contributor License Agreement](CLA.md).

To sign:

1. Comment on any open pull request with the text:
   ```
   I have read the CLA Document and I hereby sign the CLA
   ```

2. The [CLA Assistant](https://github.com/contributor-assistant/github-action)
   bot will record your signature and add a status check to your pull requests.

You only need to sign once. After that, the bot will recognize you on future
pull requests.

---

If you have questions or need help, feel free to open a
[Discussion](https://github.com/jlcoulter/momus/discussions) or ask in the
relevant pull request or issue.
