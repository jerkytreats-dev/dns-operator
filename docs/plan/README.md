# Operator Delivery Roadmap

## Goal

Build a Kubernetes-hosted internal publishing platform for `internal.jerkytreats.dev` that gives users a real domain experience inside the tailnet:

- authoritative DNS for `internal.jerkytreats.dev`
- browser-safe HTTPS for published internal hosts
- behavior that succeeds under real browser HSTS expectations
- support for nested names such as `bar.foo.internal.jerkytreats.dev`
- CRD-first durable state with an API convenience layer for fast bootstrap
- required Tailscale split-DNS bootstrap and repair automation

This roadmap is not primarily about transposing the current DNS manager into CRDs. It is about delivering a better internal publishing product while preserving enough migration compatibility to cut over safely.

## Product Requirements

The roadmap entrypoint exists to make the product contract explicit for future contributors and agents:

- `internal.jerkytreats.dev` must resolve only inside the Tailscale network.
- The system is the authoritative nameserver for that zone.
- Browser-facing hosts under that zone must succeed over HTTPS in real browsers.
- Browser success includes HSTS-safe behavior, not just passing a local `curl`.
- Nested published names are a first-class requirement.
- Wildcard certificates are not the target model.
- Shared explicit SAN management is required for published HTTPS hosts.
- Publishing internal services is the primary workflow.
- Device discovery is out of scope for v1.
- Zero application auth is an intentional default for many internal services because the tailnet is the primary trust boundary.
- CRDs are the durable source of truth.
- An API convenience layer must remain available for fast bootstrap by humans and agents.
- Tailscale split-DNS must be bootstrapped and repairable because the authoritative nameserver endpoint may change over time.

## Planning Principles

- Deliver one vertical slice at a time, starting with the user-visible publishing path.
- Make Kubernetes resources the source of truth while keeping the API as a first-class convenience interface.
- Keep the reference app as a migration and behavior guide, not as a design center.
- Treat migration, rollback, split-DNS repair, and browser validation as core work.
- Prefer one primary product-facing resource over a collection of loosely coupled low-level resources.
- Keep split-DNS automation out of the per-service reconcile hot path.

## Phase Order

- [Phase 1: Foundation and control loop](phase-1-project-initialization.md)
- [Phase 2: API and resource model](phase-2-core-crd-definitions.md)
- [Phase 3: Authoritative internal DNS slice](phase-3-controller-scaffolding.md)
- [Phase 4: Split-DNS bootstrap and repair](phase-4-development-environment-setup.md)
- [Phase 5: Shared SAN certificate management](phase-5-testing-infrastructure.md)
- [Phase 6: HTTPS publishing runtime](phase-6-build-and-cicd-setup.md)
- [Phase 7: Security, secrets, and RBAC](phase-7-rbac-and-security.md)
- [Phase 8: Observability and operational readiness](phase-8-observability.md)
- [Phase 9: Testing, migration tooling, release flow, and API guidance](phase-9-documentation.md)
- [Phase 10: Cutover and production validation](phase-10-validation.md)

## Delivery Shape

The roadmap is intentionally front loaded toward the end-user outcome:

1. publish an internal hostname
2. resolve it authoritatively inside Tailscale
3. serve it over valid HTTPS
4. make that state durable, debuggable, and safe to migrate

Each phase should produce four things:

- a clear control surface (`CRD`, `API`, or both)
- a runnable reconciliation or automation path
- status, event, and operational feedback
- an upgrade, rollback, or repair story

## Cross Phase Decisions

- Shared condition names should be defined once and reused across all resources.
- Secret material must enter the system through `Secret` references, never raw spec fields.
- File persistence from the reference app should be treated as migration input only.
- Runtime config should be rendered into `ConfigMap` or `Secret` resources that CoreDNS and Caddy consume.
- `PublishedService` is the primary user-facing concept for internal publishing.
- `DNSRecord` remains available as a lower-level authoritative DNS primitive for manual records, migration inputs, and challenge records.
- Shared SAN certificate membership should be derived from desired published hosts, not managed ad hoc per runtime process.
- Split-DNS should be managed through bootstrap and repair automation, not through every service reconcile.
- Any optional HTTP compatibility layer should remain outside the core controller design.
- Argo should install `dns-operator` from deploy artifacts in this repository, while the operator owns generated runtime state.
- The initial install surface should use `Kustomize` overlays instead of a `Helm` chart.

## Observed Reference Reality

The current reference system is not just source code in `reference/`. It also has a live runtime data shape captured in a local export plus external Tailscale admin state that are intentionally kept outside the repository.

- One active managed zone
- persisted proxy rule state
- persisted certificate SAN state
- one shared certificate pattern used across many Caddy hosts
- manually configured Tailscale split-DNS pointing the internal zone at the current nameserver IP
- plain text provider credentials in the legacy runtime config

The detailed source of truth for planning is in [Current reference state](current-reference-state.md).

The concrete source to target import mapping is in [Current reference migration matrix](current-reference-migration-matrix.md).

The target Argo deployment model is in [Deployment shape](deployment-shape.md).

## Success Criteria

The roadmap is complete when:

- `PublishedService`, `DNSRecord`, `CertificateBundle`, and `TailnetDNSConfig` have stable schemas and status contracts.
- Users inside Tailscale can resolve and browse published hosts under `internal.jerkytreats.dev`.
- Published HTTPS hosts succeed under browser HSTS expectations with valid SAN coverage.
- CoreDNS and Caddy runtime artifacts are rendered from operator owned resources.
- Split-DNS can be bootstrapped and safely repaired when the authoritative nameserver endpoint changes.
- Migration tooling exists for reference data and is safe to rerun.
- Validation, tests, metrics, and rollback guidance exist for production cutover.

## Reference Inputs

- Research summaries in `docs/research/`
- Domain migration guides in `docs/migration/`
- Runtime behavior in `reference/`
- Exported runtime state in `docs/plan/current-reference-state.md`
- Source to target migration mapping in `docs/plan/current-reference-migration-matrix.md`
- Argo install model in `docs/plan/deployment-shape.md`
