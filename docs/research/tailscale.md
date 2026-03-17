# Tailscale Domain Research

## Executive Summary

The Tailscale integration should focus on split-DNS ownership for `internal.example.test`. Device discovery, device inventory, and device-backed DNS are no longer the product direction.

## Product Role

Tailscale is required so that:

- `internal.example.test` resolves only inside the tailnet
- clients use the operator-managed authoritative nameserver for that zone
- split-DNS configuration can be bootstrapped and repaired without manual portal work

## Architectural Direction

### Current pattern

The legacy app uses Tailscale mostly for:

- device listing
- device IP resolution
- periodic device sync
- optional device-oriented HTTP endpoints

### Target pattern

The operator should use Tailscale for:

- restricted nameserver configuration for the internal zone
- drift detection for split-DNS settings
- repair or re-bootstrap when the authoritative nameserver endpoint changes

## Resource Direction

Use a `TailnetDNSConfig` style resource to model:

- the internal zone
- the target nameserver endpoint
- the tailnet identity
- the admin credentials reference
- whether to just observe, bootstrap, or repair

## Important Constraints

- Do not reintroduce `TailscaleDevice` as a core resource.
- Do not couple DNS record generation to device discovery.
- Keep split-DNS automation out of the hot path for per-service reconcile.
- Make drift visible in status so operators can tell when the tailnet is misconfigured.

## Operational Notes

- The nameserver endpoint may change during migration or failover.
- The operator needs enough Tailscale admin access to inspect and update restricted nameserver configuration.
- Split-DNS should be repairable by API or job flow, not only by manual admin portal work.
- Product success depends on clients actually resolving the zone through the authoritative nameserver.

## Testing Priorities

- fetch current split-DNS configuration from Tailscale
- detect drift against desired nameserver configuration
- repair configuration safely
- validate that internal clients resolve through the intended nameserver after repair

## Summary

The Tailscale domain should be narrowed to split-DNS bootstrap and repair. Device discovery and device CRDs are legacy concerns and should not shape the new architecture.


