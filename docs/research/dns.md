# DNS Domain Research

## Executive Summary

The DNS domain should be treated as the authoritative engine for `internal.jerkytreats.dev`, not just as file-backed record management. The product requirement is that published internal hostnames resolve correctly inside the tailnet, including nested names, and that the DNS layer cleanly supports the HTTPS publishing workflow.

## Product Role

DNS must provide:

- authoritative answers for `internal.jerkytreats.dev`
- support for nested hostnames such as `bar.foo.internal.jerkytreats.dev`
- deterministic zone rendering for CoreDNS
- a clean projection path from `PublishedService` into authoritative records

## Resource Model Direction

### Primary user-facing resource

`PublishedService` should be the main contract used by humans and agents.

### Lower-level resource

`DNSRecord` remains useful for:

- direct authoritative records that are not full published services
- generated service records owned by `PublishedService`
- bootstrap or migration exceptions

## Architectural Direction

### Current pattern

The legacy app:

- accepts imperative requests
- mutates a file-backed record set
- rewrites the zone
- reloads CoreDNS

### Target pattern

The operator should:

- watch `PublishedService` and `DNSRecord`
- compute the effective authoritative record set
- render a deterministic zone artifact
- update runtime config only when the rendered result changes

## Important Constraints

- Do not model DNS around device discovery.
- Do not assume wildcard-first naming.
- Keep the operator authoritative for the internal zone.
- Treat split-DNS as client routing to the authoritative nameserver, not as part of every record reconcile.

## Resource Notes

Recommended `DNSRecord` characteristics:

- `hostname`: full hostname under `internal.jerkytreats.dev`
- `type`: explicit record type
- `ttl`: bounded and validated
- `values`: one or more literal values
- `owner`: optional reference to the generating `PublishedService`

Useful status signals:

- effective FQDN
- rendered value set
- conflict information
- whether the record is direct or generated

## Rendering Considerations

- sort records deterministically
- render full hostnames rather than reconstructing from short labels
- maintain stable SOA serial behavior
- avoid unnecessary runtime reloads when content is unchanged
- preserve nested-name correctness in validation and output

## Testing Priorities

- nested hostname validation
- authoritative zone rendering for mixed direct and generated records
- hostname conflict detection
- `PublishedService` to DNS projection
- CoreDNS reload behavior only on meaningful changes

## Summary

`DNSRecord` is still valuable, but the main user workflow should begin with `PublishedService`. The DNS domain then turns durable product state into authoritative internal zone output for `internal.jerkytreats.dev`.

