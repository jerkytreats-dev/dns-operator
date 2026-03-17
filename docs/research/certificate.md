# Certificate Domain Research

## Executive Summary

The certificate domain must support browser-safe HTTPS for internal hosts under `internal.example.test`. Because wildcard certificates are not the target model, shared explicit SAN management becomes a core product feature rather than an implementation detail.

## Product Role

Certificates must satisfy:

- real browser validation for internal hosts
- support for nested hostnames
- explicit SAN coverage for all published HTTPS hosts
- stable attachment into the HTTPS runtime

## Resource Direction

Use a shared `CertificateBundle` concept rather than a per-host wildcard mindset.

The bundle should represent:

- the issuer and challenge method
- the durable certificate secret
- the intended SAN set
- the observed effective SAN set

## Architectural Direction

### Current pattern

The legacy app:

- manages certificates locally
- tracks SAN domains in JSON
- mutates the SAN set automatically from current records
- renews in background loops

### Target pattern

The operator should:

- derive desired SAN membership from published internal hosts
- debounce SAN changes to avoid churn
- obtain and renew certificates through controller logic
- store resulting material in Kubernetes Secrets
- report readiness and expiration in status

## Important Constraints

- Do not assume wildcard certificates.
- Support nested names such as `bar.foo.internal.example.test`.
- Avoid uncontrolled SAN mutation that can thrash issuance.
- Ensure runtime consumers can tell when a hostname is not yet covered.

## SAN Strategy Notes

Recommended early behavior:

- derive candidate SANs from `PublishedService`
- allow explicit additional SANs when needed
- preserve imported or manually curated SAN bundles carefully
- prefer stable bundle membership over highly reactive add/remove behavior

## Operational Implications

- Cloudflare DNS-01 remains relevant.
- ACME account state and issued cert material belong in Secrets.
- Certificate status should clearly show covered names, expiry, and last issuance result.
- Publishing readiness should depend on both runtime config and SAN coverage.

## Testing Priorities

- SAN set derivation from multiple published services
- nested hostname coverage
- debounce behavior for SAN changes
- renewal scheduling and expiry handling
- browser-facing validation with the generated certificate attached to the runtime

## Summary

The certificate domain should be reframed around a shared SAN bundle that serves published internal hosts. The hard product requirement is browser-safe HTTPS, so SAN coverage and runtime attachment matter more than mimicking the old local certificate manager behavior.

