# Documentation Domain Research

## Executive Summary

Documentation is still useful in the new architecture because the convenience API remains part of the product. The old assumption that all HTTP-facing documentation should be removed is no longer correct.

## Product Role

Documentation should help humans and agents understand:

- how to publish an internal service quickly
- what resource fields matter for `PublishedService`, `CertificateBundle`, `DNSRecord`, and split-DNS management
- how to tell whether DNS, certificate, and runtime readiness are actually complete

## Recommended Documentation Surfaces

- API reference for the convenience API
- examples for publish flows
- CRD reference and status semantics
- operational docs for split-DNS repair and certificate troubleshooting

## Design Direction

- Keep generated API docs if the convenience API exists.
- Keep CRD documentation and examples aligned with the same product contract.
- Prefer documentation organized by user workflow rather than by legacy subsystem.

## Important Notes

- Browser-facing success should be documented as an end-to-end outcome.
- Split-DNS repair should be documented because it is part of the product boundary.
- “Use `kubectl explain`” is helpful but not sufficient for human-friendly bootstrap workflows.

## Testing Priorities

- generated API docs match the actual convenience API
- examples reflect the current resource model
- operational docs cover certificate and split-DNS failure modes

## Summary

Documentation remains a real product surface. The new goal is to align API docs, CRD docs, and operator guidance around the internal publishing workflow rather than removing the HTTP docs layer entirely.


