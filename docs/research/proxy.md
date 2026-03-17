# Proxy Domain Research

## Executive Summary

The proxy domain is really the HTTPS publishing runtime. It should be modeled around published internal services and browser success, not around a standalone `ProxyRule` resource.

## Product Role

The runtime must:

- terminate HTTPS for internal hosts
- present certificates that satisfy browser and HSTS requirements
- proxy to backends reachable inside the homelab or tailnet
- support backend transport options such as HTTPS upstreams and insecure skip verify when explicitly requested

## Architectural Direction

### Current pattern

The legacy app:

- stores proxy rules in JSON
- renders a Caddyfile from templates
- reloads Caddy through local process management

### Target pattern

The operator should:

- watch `PublishedService`
- derive the effective set of HTTPS sites
- render a deterministic Caddyfile or equivalent runtime config
- deploy or update the runtime artifact
- surface readiness in resource status

## Resource Direction

Do not keep `ProxyRule` as the main user-facing resource.

Instead:

- `PublishedService` is the primary publish contract
- backend transport settings live inside the publish contract
- runtime-specific output is generated, not user-authored

## Important Runtime Concerns

- exact hostname match for each published site
- HTTP to HTTPS redirect behavior
- certificate secret attachment for matching SAN coverage
- support for upstream `http` and `https`
- support for `insecureSkipVerify` when required for local bootstrap
- stable config generation order to minimize churn

## Example Derived Runtime Concept

A single `PublishedService` should be enough to derive:

- the Caddy site label
- reverse proxy target
- transport options
- certificate secret reference
- readiness expectation

## Migration Implications

- Replace JSON rule persistence with Kubernetes durable state.
- Preserve Caddyfile generation concepts, but derive them from `PublishedService`.
- Preserve support for special backend transport behavior from the legacy `Caddyfile`.
- Remove supervisord assumptions from the target runtime.

## Testing Priorities

- config generation from multiple `PublishedService` resources
- backend transport rendering, including insecure skip verify
- certificate-to-site attachment validation
- HTTP to HTTPS redirect behavior
- real browser validation against internal published hosts

## Summary

The proxy domain should be reframed as the HTTPS publishing runtime. The resource the user cares about is a published service, while the generated Caddy configuration is an implementation detail.

