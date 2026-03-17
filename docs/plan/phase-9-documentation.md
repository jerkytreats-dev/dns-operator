# Phase 9: Testing, Migration Tooling, and Release Flow

## Goal

Prove that the internal publishing platform works, migrate existing data safely, keep the API convenience path aligned with CRDs, and make builds repeatable.

## Scope

- Build unit, envtest, integration, and end to end coverage around the delivered slices.
- Create import tools for reference DNS, proxy, certificate, and split-DNS related data where needed.
- Define release automation for manifests and images.
- Write user facing guidance for install, upgrade, migration, and rollback.
- Write user facing guidance for the convenience API and how it maps to durable resources.
- Define how CI updates cluster overlays with the published image tag.

## Migration Inputs To Support

The current plan must support import from the real runtime data set:

- `config.yaml`
- `proxy_rules.json`
- `certificate_domains.json`
- zone file
- Tailscale DNS admin state

The import path should work from either a live appdata directory or an exported archive.

The concrete source to target mapping lives in [Current reference migration matrix](current-reference-migration-matrix.md).

## Deliverables

- Controller focused test suites.
- Idempotent migration tools that can be run more than once safely.
- Build and release pipeline for images and generated manifests.
- Operator guides with sample resources, API bootstrap guidance, and troubleshooting steps.
- Argo installation guidance for the infra repository.

## Testing Priorities

- Schema validation tests for all custom resources.
- Reconcile tests for happy path and failure path behavior.
- Migration tests that confirm source data becomes the expected resource set.
- End to end smoke tests for authoritative DNS, split-DNS repair, certificate, and HTTPS publishing flows.
- Migration tests for case variants, nested labels, and shared certificate imports.
- Browser validation tests that confirm published hosts succeed over HTTPS from inside Tailscale.
- API tests that confirm the convenience layer creates the same durable state as direct CRD operations.
- Deployment tests that confirm the Argo target overlay installs cleanly.

## Exit Criteria

- Tests cover the current vertical slices well enough to catch regressions.
- Migration tooling is documented and safe to rerun.
- Release flow publishes consistent artifacts.
- User docs match the actual install, upgrade, API bootstrap, and split-DNS repair paths.
- Import tooling handles the current exported appdata layout without manual reshaping.
- CI can publish an image and update the target overlay that Argo watches.
