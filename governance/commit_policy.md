# Commit Policy

Date: 2026-03-16
Status: active

## Intent

Define commit message and scoping rules for this repository.

## Conventional Commit Use

Use conventional commits when instructed to commit.

Approved commit `type` values:

- `feat`
- `fix`
- `perf`
- `refactor`
- `docs`
- `design`
- `test`
- `build`
- `ci`
- `chore`
- `policy`

## Type Selection

- Use `design` for changes that primarily update roadmap, architecture, migration, or research material under `docs/plan/`, `docs/research/`, and `docs/migration/`.
- When one commit mixes runtime code with design updates, keep the runtime-focused commit type and describe design impact in the commit body.
- Use `policy` for repository governance updates such as `AGENTS.md` and files under `governance/`.

## Governance Trace For Policy Commits

For `policy` commits include at least one governance trace footer such as `Policy-Ref:` or `Discussion:`.

## Subject Rules

- Write the subject as a declarative summary of what changed.
- Describe concrete behavior or ownership changes, not phase labels.
- Keep the subject specific to the diff.
- Prefer `type(scope): summary`.

Examples:

- good `feat(publish): derive caddy sites from PublishedService`
- good `design(plan): rebuild roadmap around internal publishing outcomes`
- good `policy(agents): add repo governance and domain architecture rules`
- bad `refactor: implement phase 2`

## Push Guard

Verify with the user before push.

## Required Checks

- Before every commit, run `make check-commit`.
- A commit is not ready until `make check-commit` exits with success.
- If your change touches controllers, CRDs, manifests, install paths, or release automation, also run `make test-e2e` before you ask for final ship approval.

## Clean Branch Rule

- Do not treat red checks as out of scope.
- If `Lint`, `Tests`, or `E2E Tests` is red on the branch you are extending, fix the branch before you stack unrelated work on top.
- Do not commit on top of a knowingly broken branch and defer cleanup to later.
