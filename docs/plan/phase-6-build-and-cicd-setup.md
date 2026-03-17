# Phase 6: HTTPS Publishing Runtime

## Goal

Render and run the HTTPS publishing tier that terminates browser traffic for internal hosts and serves them correctly under the shared SAN certificate model.

## Scope

- Implement the `PublishedService` controller.
- Render Caddy configuration into operator owned resources.
- Define how CoreDNS and Caddy consume rendered config and certificate material.
- Establish deployment manifests for runtime components that the operator feeds.
- Define the Argo install path for the operator manager, API service, and runtime components.

## Current Reference Inputs

The current export shows multiple persisted proxy rules in `proxy_rules.json` and a rendered `Caddyfile` with many host blocks using a shared certificate path.

Backends currently include both:

- plain HTTP targets
- HTTPS targets such as `media` that require backend transport TLS handling

The rendered `Caddyfile` also carries backend transport details that are not present in `proxy_rules.json`, including `tls_insecure_skip_verify` for some HTTPS backends.

## Deliverables

- `PublishedService` reconciler for the HTTPS publishing path.
- Caddy config renderer with stable output.
- Deployment, service, and config mount contracts for CoreDNS and Caddy.
- Runtime reload strategy that is safe and observable.
- A documented Git path in this repository that Argo can sync directly.

## Controller Responsibilities

- Watch `PublishedService` resources.
- Resolve served hostnames, backend targets, TLS mode, and auth mode.
- Render desired proxy config into `ConfigMap` or `Secret` resources.
- Ensure published HTTPS hosts reference the correct shared certificate secret.
- Report whether runtime config is in sync with desired state.

## Design Notes

- Keep runtime processes separate from the operator manager.
- The operator should publish desired config, not embed Caddy or CoreDNS process management.
- Remove local proxy rule file storage from the design.
- `PublishedService` should be sufficient for the common internal publishing workflow.
- The first operator-compatible renderer should preserve current shared certificate behavior for Caddy hosts.
- Migration logic should preserve backend protocol differences from `proxy_rules.json`.
- Migration logic should preserve backend transport settings from the rendered `Caddyfile`, including insecure verification where currently required.
- HTTP should redirect to HTTPS consistently for published browser-facing hosts.
- The install path should be `Kustomize` based and cluster overlay driven.
- Argo should point at this repository for install artifacts, while the infra repository owns the `Application` manifest.

## Exit Criteria

- A `PublishedService` change updates the expected runtime config artifact.
- Runtime manifests deploy cleanly in a test cluster.
- Caddy reload behavior is defined, measurable, and recoverable.
- No proxy state relies on local files.
- Imported proxy rules produce equivalent host behavior to the current rendered `Caddyfile`.
- A browser inside Tailscale can successfully open a published host over HTTPS with the expected certificate and redirect behavior.
- A cluster specific overlay can be consumed by Argo without custom build steps.
