# API Domain Research

## Executive Summary

The API should remain part of the product. It is not just migration compatibility for the legacy DNS manager; it is the fast bootstrap surface that lets humans and agents publish working internal services without hand-authoring CRDs first.

The target shape is:

- CRDs remain the durable source of truth
- the API creates or updates the same durable resources
- the main API concept is internal publishing, not raw record manipulation

## Product Role

The API exists to support workflows like:

1. start a small service on a node
2. call one endpoint
3. get a working hostname under `internal.example.test`
4. have it resolve only inside Tailscale
5. have it load in a browser over HTTPS

That means the API should be shaped around `PublishedService`, not around low-level DNS, proxy, and cert operations as separate user concepts.

## Recommended API Surface

### Primary operations

- create or update a `PublishedService`
- inspect publish readiness and effective URL
- list published services
- remove or pause a published service

### Secondary operations

- inspect authoritative DNS state
- inspect certificate bundle state
- trigger or inspect split-DNS repair

### Example request shape

```json
{
  "hostname": "app.internal.example.test",
  "publishMode": "httpsProxy",
  "backend": {
    "address": "192.0.2.10",
    "port": 80,
    "protocol": "http"
  },
  "auth": {
    "mode": "none"
  }
}
```

## Architectural Direction

### Current pattern

The legacy API is synchronous and imperative:

- accept a request
- write files
- restart or reload runtime components
- return success immediately

### Target pattern

The new API should be asynchronous over durable state:

- accept a publish request
- create or update a CRD such as `PublishedService`
- return accepted plus a durable resource reference
- let controllers drive DNS, certificate, and runtime convergence
- expose readiness via status or API reads

## Resource Mapping

- API publish request -> `PublishedService`
- API manual DNS request -> `DNSRecord`
- API certificate inspection -> `CertificateBundle`
- API split-DNS repair request -> `TailnetDNSConfig` repair action or equivalent

## Design Constraints

- The API must not become a second source of truth.
- API-created resources and Git-managed resources must reconcile identically.
- The API should preserve the “spin up quickly” workflow for agents.
- Zero app auth as the default should be expressible directly in the publish contract.
- Browser success is the real outcome, not just resource creation.

## Operational Implications

- API docs remain useful and should not be treated as legacy clutter.
- API responses should surface durable resource identifiers and current readiness.
- API validation should align with CRD validation to avoid drift.
- The API should expose enough status for agents to decide whether a service is really ready.

## Testing Strategy

- API request validation tests
- API-to-CRD mapping tests
- controller convergence tests after API writes
- end-to-end publish tests that validate browser-facing readiness

## Summary

The API should stay. The change is not “remove the API because Kubernetes exists.” The change is:

1. keep CRDs as durable state
2. keep the API as a first-class convenience interface
3. center both on `PublishedService` and internal publishing outcomes


