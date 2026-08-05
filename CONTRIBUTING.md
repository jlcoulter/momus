# Contributing to Momus

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

## Development Workflow

1. Create a feature branch off `master`:
   ```sh
   git checkout -b feat/my-feature
   ```
2. Make your changes.
3. Run the quality checks locally:
   ```sh
   cargo fmt --check
   cargo clippy --all-targets -- -D warnings
   cargo test --all-targets
   ```
4. Commit and push — hooks will enforce the checks automatically.
5. Open a pull request against `master`.
