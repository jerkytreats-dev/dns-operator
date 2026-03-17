# Phase 4: Split-DNS Bootstrap and Repair

## Goal

Make sure Tailscale clients resolve `internal.jerkytreats.dev` against the correct authoritative nameserver endpoint, even when that endpoint changes.

## Scope

- Implement the `TailnetDNSConfig` controller or equivalent bootstrap/repair automation path.
- Manage Tailscale restricted nameserver configuration for `internal.jerkytreats.dev`.
- Define drift detection, rerun safety, and a manual break-glass path.
- Keep split-DNS management outside the hot path for every `PublishedService` reconcile.

## Current Reference Inputs

The current split-DNS configuration is managed manually in the Tailscale admin portal rather than by the application itself. The internal zone currently points at the old authoritative nameserver IP, and that endpoint will change during migration.

This means split-DNS must be treated as required platform automation, not as an optional post-cutover task.

## Deliverables

- `TailnetDNSConfig` reconcile or repair path with idempotent behavior.
- `Secret` based credential handling for Tailscale admin access.
- Status fields for configured nameserver, last apply time, and detected drift.
- Operational guidance for bootstrap, repair, and break-glass manual recovery.

## Controller Responsibilities

- Watch `TailnetDNSConfig` resources.
- Configure restricted nameserver settings for the managed internal zone.
- Detect drift between desired and effective split-DNS state.
- Reapply safely when the authoritative nameserver endpoint changes.
- Surface auth failures, API failures, and drift clearly.

## Design Notes

- Split-DNS is required in v1, but it should be bootstrap and repair automation only.
- Successful service publishing should not require rewriting tailnet DNS settings on every service update.
- The nameserver endpoint should come from durable desired state, not from hand-maintained portal notes.
- A manual recovery path must exist in case Tailscale automation is unavailable.
- Device discovery is intentionally out of scope for this phase.

## Exit Criteria

- A fresh install can configure split-DNS for `internal.jerkytreats.dev`.
- A changed authoritative nameserver endpoint can be repaired safely and rerun idempotently.
- Status shows whether split-DNS is configured, stale, or broken.
- Error handling covers missing credentials, Tailscale API failure, and partial configuration drift.
- Production cutover is blocked if clients inside Tailscale cannot resolve the internal zone through the intended nameserver.
