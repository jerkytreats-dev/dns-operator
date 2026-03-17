# Phase 5: Shared SAN Certificate Management

## Goal

Manage one shared certificate set for published HTTPS hosts under `internal.jerkytreats.dev` so browser access succeeds without wildcard assumptions.

## Scope

- Implement the `CertificateBundle` controller.
- Store issued material in Kubernetes `Secret` resources.
- Derive SAN membership from desired published HTTPS hosts plus any explicit additional domains.
- Model renewal, backoff, and failure states in resource status.
- Integrate DNS challenge behavior with the authoritative DNS slice.

## Current Reference Inputs

The current export shows certificate state split across:

- `config.yaml` with provider email, ACME directory URL, and Cloudflare token
- `certificate_domains.json` with one base domain and a SAN list
- shared certificate file paths used by CoreDNS and Caddy

The current product requirement keeps shared explicit SAN management as the target model because published hostnames may be nested and do not fit a simple wildcard strategy.

## Deliverables

- `CertificateBundle` reconciler with issuance and renewal flow.
- Stable secret naming and ownership rules.
- Status fields for issuance, expiry, effective SAN set, and last failure.
- Clear dependency contract between published hosts, DNS challenge records, and the shared certificate secret.

## Controller Responsibilities

- Watch `CertificateBundle` resources.
- Watch published HTTPS hosts when building the effective SAN set.
- Resolve issuer and challenge credentials through `Secret` references.
- Request and renew certificates.
- Update readiness, expiry, and effective-domain conditions.
- Coordinate with DNS when challenge records are required.

## Design Notes

- Keep certificate issuance separate from proxy rollout so each concern is debuggable.
- SAN management should be explicit and reviewable from resource spec and status.
- The controller should avoid implicit domain expansion that users cannot see.
- The first compatible target should preserve the current shared SAN behavior while shifting control into durable resources.
- Renewal logic should debounce SAN churn so creating several published hosts does not cause unnecessary certificate thrash.
- Browser success, not just secret issuance, is the real acceptance criterion.

## Exit Criteria

- A `CertificateBundle` can issue successfully and publish a target `Secret`.
- The effective SAN set matches the desired published HTTPS hostnames.
- Renewal flow is defined and testable.
- Failure states are visible through conditions and events.
- DNS coordination for challenge records is documented and implemented.
- Import from `certificate_domains.json` preserves current SAN behavior during migration.
