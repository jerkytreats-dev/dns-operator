# Docs Style Policy

Date: 2026-03-16
Status: active

## Architecture And Workflow Diagrams

- Prefer Mermaid diagrams when a diagram materially improves clarity.
- Keep diagrams near the related plan, spec, or migration section.
- Use labels that match domain terms used in the product model and code.

## Product Language

- Prefer product-facing terms such as `PublishedService`, `DNSRecord`, `CertificateBundle`, and `TailnetDNSConfig` when describing the target design.
- Avoid reviving outdated design terms such as `ProxyRule` and `TailscaleDevice` except when explicitly describing legacy behavior or migration history.

## Migration And Plan Docs

- Make success criteria outcome-based, not just resource-based.
- When documenting implementation order, call out ownership boundaries and rollback implications.
- Keep browser-facing HTTPS and internal-only resolution requirements explicit where relevant.
