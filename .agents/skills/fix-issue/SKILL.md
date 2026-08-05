---
name: fix-issue
description: Fix a GitHub issue in the jlcoulter/momus repository. Reads the issue, creates a branch, implements the fix, runs checks, commits, and opens a PR.
---

# Fix GitHub Issue

Use this skill when asked to fix a specific GitHub issue in the `jlcoulter/momus` repository. The workflow covers reading the issue, implementing the fix, running validation, and opening a PR.

## Workflow

### 1. Read the issue

Fetch the issue body from GitHub to understand the problem, the fix, and verification steps.

```bash
curl -s https://api.github.com/repos/jlcoulter/momus/issues/<NUMBER>
```

Read the issue body carefully — it contains the problem description, the expected fix, and verification steps.

### 2. Create a branch

Create a new branch off `master` with a descriptive name:

```bash
git checkout master
git pull origin master
git checkout -b fix/issue-<NUMBER>-<descriptive-name>
```

### 3. Implement the fix

Make minimal, targeted changes. Do not refactor unrelated code. Follow the project conventions from `AGENTS.md`:

- **Edition 2024** — all crates use `edition = "2024"`
- **No `rand` crate** — use `SimpleRng` for deterministic randomness
- **No `thiserror`** — use `anyhow` for error handling
- **No `async-trait`** — use `async fn` in traits
- **Feature-gated modules** — use `#[cfg(feature = "...")]` for optional functionality
- **AST is JSON-serializable** — types derive `Serialize`/`Deserialize`

### 4. Run validation

After the fix, run these checks in order:

```bash
# Format
cargo fmt --check
# If it fails, run: cargo fmt

# Lint
cargo clippy --all-targets -- -D warnings

# Tests
cargo test --all-targets
```

Fix any issues found by each command before moving to the next.

### 5. Commit

Commit with a conventional commit message referencing the issue:

```bash
git add -A
git commit -m "fix: short description of what was fixed (#<NUMBER>)"
```

### 6. Update docs

If the fix changes behavior or adds a feature, update `FEATURES.md` status and any relevant documentation.

### 7. Push and open a PR

```bash
git push origin fix/issue-<NUMBER>-<descriptive-name>
```

Then open a PR against `master` with a description that references the issue (e.g., `Closes #<NUMBER>`).
