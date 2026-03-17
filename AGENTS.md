# AGENTS.md

## Domain Architecture Rule

- Organize implementation by domain first.
- Put CRD types and versioned API surface under `api/<group>/<version>/`.
- Keep controller-runtime reconcilers thin under `internal/controller/`. Reconcile code should translate Kubernetes events into domain operations, not hold the core business logic.
- Put product logic under explicit domains such as `internal/publish/`, `internal/dns/`, `internal/certificate/`, and `internal/tailnetdns/`.
- Inside a domain, prefer behavior-oriented files and packages over generic technical-layer buckets.
- Cross-domain calls must use explicit contracts or interfaces. Do not reach into another domain's internal details.
- Avoid spreading business rules across controllers, API types, and runtime renderers. Pick a clear owner for each rule.
- For migrations from the reference implementation, use compatibility wrappers when needed and require characterization plus parity tests before removing legacy paths.

## Governance Index

- [Commit Policy](governance/commit_policy.md)
- [Compatibility Policy](governance/compatibility_policy.md)
- [Docs Style Policy](governance/docs_style_policy.md)
- [Policy Proposal Flow](governance/policy_proposal_flow.md)
- [Complex Change Workflow Governance](governance/complex_change_workflow.md)

## Commit Governance Rule

- For every user request that asks for a commit, review [Commit Policy](governance/commit_policy.md) before running `git commit` or `git commit --amend`.

## Complex Workflow Note

- Complex workflow mode is user triggered.
- CI does not enforce complex workflow artifacts.
