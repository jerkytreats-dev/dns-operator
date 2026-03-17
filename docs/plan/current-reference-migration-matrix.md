# Current Reference Migration Matrix

## Purpose

This document maps the exported reference runtime data into the target operator resources.

It exists to answer one question clearly:

What should be imported from each current file, into which Kubernetes resource, with which field mapping and migration rules.

## Input Files

- `config.yaml`
- `proxy_rules.json`
- `certificate_domains.json`
- zone file
- `Corefile`
- `Caddyfile`
- Tailscale DNS admin settings

## Import Order

The import path should run in this order:

1. shared secrets and operator config
2. `DNSRecord` resources for imported authoritative records
3. `PublishedService` resources for browser-facing internal hosts
4. one shared `CertificateBundle` resource for the current base domain and SAN set
5. `TailnetDNSConfig` for split-DNS bootstrap and repair
6. rendered runtime verification against CoreDNS and Caddy output
7. browser and DNS validation from inside Tailscale

## Mapping Rules

### `config.yaml`

**Target resources**

- operator `ConfigMap`
- operator `Secret`
- shared `CertificateBundle`
- `TailnetDNSConfig`
- default runtime deployment values

**Field mapping**

- `dns.domain` to operator default managed zone
- `dns.internal.origin` to operator default zone origin
- `tailscale.api_key` to `Secret` named `tailscale-admin-credentials`, key `api-key`
- `tailscale.tailnet` to `TailnetDNSConfig.spec.tailnet`
- `certificate.email` to `CertificateBundle.spec.issuer.email`
- `certificate.cloudflare_api_token` to `Secret` named `cloudflare-credentials`, key `api-token`
- `certificate.use_production_certs` to `CertificateBundle.spec.issuer.provider`
- `certificate.renewal.renew_before` to `CertificateBundle.spec.renewBefore`

**Migration notes**

- Secrets must never stay in plain text config after import.
- `ca_dir_url` can remain an implementation detail if provider selection already captures production versus staging.
- Current file paths for certs and runtime config should become deployment wiring, not user managed spec fields.
- Tailscale admin credentials exist to manage split-DNS, not to drive device discovery in v1.

### zone file

**Target resources**

- one `DNSRecord` per desired hostname after normalization and dedupe
- optional `PublishedService` seeds when a hostname is clearly browser-facing and paired with proxy state

**Field mapping**

- zone label and origin to `DNSRecord.spec.hostname`
- record type to `DNSRecord.spec.type`
- record values to `DNSRecord.spec.values`

**Classification rules**

- If a hostname is only authoritative DNS state with no published HTTPS behavior, import it as a standalone `DNSRecord`.
- If a hostname also appears in `proxy_rules.json`, import a `PublishedService` for that hostname and keep the `DNSRecord` as the derived or imported authoritative record.
- If a hostname has no proxy rule and appears to be device- or infrastructure-facing, import it as a lower-level `DNSRecord` and flag it for later product review if needed.

**Migration notes**

- Nested labels must remain valid import targets.
- DNS is case insensitive, so labels that differ only by case represent one desired hostname and require dedupe.
- The importer should default to lower case host labels and emit a collision report when source labels differ only by case.
- `DNSRecord` remains the authoritative DNS primitive, but `PublishedService` is the primary publishing surface.

### `proxy_rules.json`

**Target resources**

- one `PublishedService` per browser-facing internal host

**Field mapping**

- source key or `hostname` to `PublishedService.spec.hostname`
- `target_ip` to `PublishedService.spec.backend.address`
- `target_port` to `PublishedService.spec.backend.port`
- `protocol` to `PublishedService.spec.backend.protocol`
- `enabled` to whether the service should exist as published desired state

**Derived fields**

- `PublishedService.spec.publishMode` should default to `httpsProxy`
- shared certificate linkage to `PublishedService.spec.tls.mode=sharedSAN`
- `PublishedService.spec.auth.mode` should default to `none`
- backend transport settings such as `insecureSkipVerify` should be derived from the rendered `Caddyfile` when present

**Migration notes**

- Backend protocol differences must be preserved.
- HTTPS backends such as `media` require explicit backend transport handling in the first renderer.
- `created_at` is useful migration metadata and should move to an annotation instead of spec.
- Disabled rules should either be omitted or imported in a paused state, but should not silently publish traffic.

### `certificate_domains.json`

**Target resources**

- one initial shared `CertificateBundle`

**Field mapping**

- `base_domain` to the managed zone and to `CertificateBundle.spec.additionalDomains` when needed
- each `san_domains` entry to effective published SAN membership on the shared bundle
- imported email and provider config from `config.yaml` to `CertificateBundle.spec.issuer`
- imported Cloudflare token secret ref to `CertificateBundle.spec.challenge.cloudflare.apiTokenSecretRef`

**Migration notes**

- The first compatible operator state should preserve the current single certificate plus SAN model.
- Per host certificate splitting can happen later after functional parity is proven.
- Domain import should be deduped case insensitively.
- The imported SAN list should seed the shared bundle and then converge toward published-host-driven membership.
- SAN membership should remain inspectable and explicit in status even when derived from published hosts.

### `Corefile`

**Target resources**

- runtime deployment defaults
- verification data for generated CoreDNS config

**Migration notes**

- Treat `Corefile` as a rendered artifact to compare against, not as the main source of desired state.
- Current health and TLS listener behavior should be preserved in the runtime deployment plan.

### `Caddyfile`

**Target resources**

- runtime deployment defaults
- verification data for generated Caddy config
- derived backend transport settings for imported `PublishedService` resources

**Migration notes**

- Treat `Caddyfile` as a rendered artifact to compare against, not as the main source of desired state.
- The current shared certificate path pattern should remain the first compatible render target.
- Preserve backend transport details such as `tls_insecure_skip_verify` where they exist in the rendered config.

### Tailscale DNS admin settings

**Target resources**

- one `TailnetDNSConfig`

**Field mapping**

- managed zone to `TailnetDNSConfig.spec.zone`
- current restricted nameserver target to `TailnetDNSConfig.spec.nameserver.address`

**Migration notes**

- Current split-DNS state lives outside the exported runtime files and must be captured during migration planning.
- The initial automation path should be safe to rerun and must not assume the current nameserver endpoint is still correct after cutover.

## Resource Count Expectations

Based on the exported snapshot, the first import should expect roughly:

- multiple `DNSRecord` resources before dedupe and classification
- multiple `PublishedService` resources
- one shared `CertificateBundle` with the imported domain set
- one `TailnetDNSConfig`

## Known Edge Cases

### Case collisions

- mixed case and lower case variants of the same hostname

The importer should produce one lower case canonical DNS name and keep source variants in annotations or a migration report.

### Name normalization

Examples from the export that need careful handling:

- nested DNS labels within the managed zone
- SAN entries that differ only by case

### Shared ingress behavior

Many service DNS names currently point to the DNS manager host IP and rely on Caddy for the backend hop. The first operator migration should preserve that traffic pattern.

### Split-DNS ownership

The current restricted nameserver state is hand managed outside the application today. Migration must capture it explicitly so the new authoritative endpoint can take over without hidden portal state.

## Validation Checks

After import, validate:

- all imported hostnames resolve to the expected rendered DNS targets
- all imported published hosts render equivalent backend mappings
- imported HTTPS backends preserve required transport settings such as insecure verification flags
- all SAN domains appear on the imported shared certificate bundle
- split-DNS points `internal.example.test` at the intended authoritative nameserver endpoint
- no source credentials remain in plain text config used by the new operator runtime
- case collisions and normalization changes are captured in a migration report

## Decision Summary

- `config.yaml` seeds operator config and secrets
- zone file seeds `DNSRecord`
- `proxy_rules.json` seeds `PublishedService`
- `certificate_domains.json` seeds one shared `CertificateBundle`
- Tailscale DNS admin settings seed `TailnetDNSConfig`
- `Corefile` and `Caddyfile` are verification artifacts, not primary desired state inputs
