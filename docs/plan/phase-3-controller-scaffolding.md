# Phase 3: Authoritative Internal DNS Slice

## Goal

Land the first end to end controller path for authoritative DNS of `internal.example.test`.

## Scope

- Implement the `DNSRecord` reconciler.
- Project `PublishedService` hostnames into authoritative DNS records.
- Aggregate records by zone and render zone data into `ConfigMap` resources.
- Reuse proven validation and rendering behavior from the reference implementation where it fits the controller model.
- Publish clear status, events, and error handling for DNS publication.

## Current Reference Inputs

The current export shows a live zone with mixed direct records, nested labels, and names that later front proxied services. Migration logic for this phase should consume:

- `config.yaml` for the managed domain
- the current zone file for current record data
- `proxy_rules.json` only when needed to classify which imported hostnames become `PublishedService` resources later

This phase should focus on authoritative DNS correctness first, not on backend discovery.

## Deliverables

- `DNSRecord` controller with create, update, and delete reconciliation.
- A projection path from `PublishedService` to the authoritative DNS records it needs.
- Zone rendering package that produces stable output for the internal zone.
- `ConfigMap` contract for CoreDNS consumption.
- Sample manifests that prove nested hostnames and shared-zone aggregation.

## Controller Responsibilities

- Watch `DNSRecord` resources.
- Watch `PublishedService` resources only to derive their authoritative DNS entries.
- Group records by zone.
- Write rendered zone data only when effective output changes.
- Update status with rendered DNS state and record ownership.

## Design Notes

- Treat the rendered zone as a shared artifact owned by the operator.
- Keep `DNSRecord` as the authoritative primitive, but keep `PublishedService` as the primary user-facing publishing interface.
- Avoid direct runtime reload logic in the first DNS slice.
- Do not block the DNS slice on certificate issuance or proxy rendering.
- Accept nested record labels and full hostnames.
- Normalize or explicitly reject case-only collisions during import.

## Exit Criteria

- Creating or updating a `DNSRecord` updates the expected zone artifact.
- Creating a `PublishedService` produces the expected authoritative DNS entry for its hostname.
- Updating a record changes only the affected zone output.
- Deleting a record removes it from the zone output.
- Status and events make failures diagnosable without reading controller code.
- Imported records from the current zone file round trip without unexpected hostname drift.
