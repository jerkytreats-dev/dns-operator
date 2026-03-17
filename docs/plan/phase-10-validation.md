# Phase 10: Cutover and Production Validation

## Goal

Move from parallel planning and slice delivery into a controlled production rollout.

## Scope

- Validate that all core slices work together in a representative cluster.
- Rehearse migration, rollback, and disaster recovery steps.
- Define the final cutover sequence away from the reference runtime.
- Remove or archive superseded reference behaviors once the operator path is trusted.
- Validate the final Argo based install path from the infra repository.

## Current Cutover Assumptions

The reference runtime currently combines:

- CoreDNS served from file rendered config
- Caddy served from file rendered config
- one shared certificate plus SAN set
- Tailscale and Cloudflare credentials in plain text config
- manually configured Tailscale split-DNS pointing the internal zone at the old nameserver endpoint

Production validation must prove the operator can replace the user-visible behavior that matters:

- authoritative internal resolution inside Tailscale
- successful browser access over HTTPS for published hosts
- correct SAN coverage for published hostnames
- safe split-DNS ownership of the internal zone

It must also prove that Argo can install the operator from this repository using deploy artifacts only.

## Deliverables

- Cutover checklist.
- Rollback checklist.
- Production validation report.
- Decommission plan for HTTP paths and file based persistence that are no longer needed.

## Validation Priorities

- End to end reconciliation across published services, authoritative DNS, shared SAN certificate management, and runtime config.
- Upgrade safety for CRD changes and controller images.
- Recovery behavior after operator restart, runtime restart, and transient dependency failure.
- Verification that rendered runtime artifacts match desired cluster state.
- Evidence that imported runtime data preserves current hostname, backend, and SAN behavior.
- Evidence that Tailscale clients resolve `internal.example.test` through the new authoritative endpoint after cutover.
- Evidence that browser access succeeds under HSTS expectations from inside Tailscale.
- Evidence that the infra repository can point Argo at the install overlay in this repository and reach a healthy sync.

## Exit Criteria

- Production rehearsal succeeds with documented evidence.
- Rollback steps are tested, not just written down.
- Reference era persistence and control paths are no longer required for normal operations.
- The team has clear runbooks for day two support.
- Secret migration away from plain text provider credentials is complete.
- Split-DNS ownership has moved from hand-crafted portal state to documented bootstrap and repair automation.
- The Argo application in the infra repository deploys `dns-operator` from deploy artifacts only.
