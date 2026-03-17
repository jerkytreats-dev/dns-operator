# Current Reference State

## Purpose

This document records the observed state of the current reference DNS manager from the exported runtime data plus the manually configured Tailscale DNS state. The roadmap should stay aligned with this inventory so migration work targets the real system, not an abstract design.

## Source Snapshot

Observed from a local runtime export stored outside the repository.

## Runtime Layout

- Main configuration file at `configs/config.yaml`
- CoreDNS config at `coredns/Corefile`
- CoreDNS zone data at `coredns/zones/<zone>.zone`
- Device inventory at `data/devices.json`
- Certificate domain state at `data/certificate_domains.json`
- Proxy rule state at `data/proxy_rules.json`
- Rendered Caddy config at `configs/Caddyfile`
- Tailscale DNS settings currently managed outside the repository through the admin portal

## Observed Inventory

- One managed zone
- multiple DNS A records in the zone file
- multiple stored proxy rules
- multiple certificate SAN domains
- one manually configured restricted nameserver for the internal zone in Tailscale

## DNS Reality

- The current system serves one zone from a single zone file.
- Some names map directly to device IPs.
- Many service names map to the DNS manager host IP and rely on Caddy for the final backend hop.
- The zone includes mixed case names and lower case equivalents.
- The zone also includes nested labels.
- The current authoritative nameserver endpoint is tied to one specific Tailscale IP.

## Tailscale DNS Reality

- `internal.jerkytreats.dev` currently resolves only inside Tailscale via restricted nameserver settings.
- That split-DNS configuration is managed manually in the Tailscale admin portal today.
- The current split-DNS target points at the old authoritative nameserver endpoint, which will change during migration.
- The current runtime also stores raw Tailscale admin credentials in `config.yaml`.

## Certificate Reality

- The current runtime appears to use one base domain certificate plus a SAN list.
- Caddy is configured to use the same existing certificate path for many hosts.
- Certificate domain membership is tracked in `certificate_domains.json`.
- The current runtime also stores the Cloudflare token in `config.yaml`.
- The SAN list includes browser-facing nested hostnames and mixed-case variants that matter during migration.

## Proxy Reality

- Proxy rules are persisted as hostname to backend mappings with target IP, target port, protocol, enabled flag, and created time.
- Rendered Caddy config is file based.
- Proxy backends include both plain HTTP and HTTPS.
- At least one backend uses HTTPS with insecure verification disabled at the backend hop.
- Some backend transport details exist only in the rendered `Caddyfile`, not in `proxy_rules.json`.

## Migration Implications

- Migration tooling must read appdata style file layouts, not just source repo structures.
- DNS migration must preserve the distinction between direct authoritative records and browser-facing proxied service names.
- Certificate migration should preserve the current single certificate plus SAN model as the first compatible operator slice.
- Import logic must normalize or flag case collisions in host labels.
- Secret migration is mandatory because current config stores provider credentials in plain text.
- Split-DNS migration is mandatory because the authoritative nameserver endpoint will move.
- Device inventory is migration context only and should not drive the v1 product model.

## Planning Constraints

- The roadmap should treat `config.yaml`, `proxy_rules.json`, `certificate_domains.json`, the zone file, and the Tailscale DNS admin state as first class migration inputs.
- `devices.json` may still help explain direct A records, but should not drive the primary resource design.
- The first operator milestone should preserve current browser-visible behavior before introducing more granular certificate or proxy models.
- Validation and migration steps must account for nested host labels, case variants, shared certificate usage, and split-DNS cutover.
