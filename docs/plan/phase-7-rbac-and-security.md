# Phase 7: Security, Secrets, and RBAC

## Goal

Harden the operator and runtime resources before wider rollout.

## Scope

- Define least privilege RBAC for the operator manager.
- Define how all external credentials enter through `Secret` references.
- Harden pod security context for the operator and runtime workloads.
- Review namespace boundaries and cross namespace reference rules.
- Align secret creation order with Argo sync wave expectations.

## Deliverables

- RBAC manifests that match actual controller behavior.
- Secret reference patterns for Tailscale admin access and certificate providers.
- Security context defaults for operator, CoreDNS, and Caddy workloads.
- Clear rules for ownership, finalizers, and garbage collection.
- Secret provider ordering guidance for Argo managed installs.

## Security Priorities

- No privileged mode for the operator manager.
- No raw token fields in any custom resource spec.
- Explicit read and write access only for resources each controller owns.
- Minimal service account scope for runtime components.
- Secret material must exist before the operator deployment starts reconciling provider dependent resources.
- Tailnet-level automation credentials should be scoped and auditable because they can change split-DNS behavior for the whole tailnet.

## Exit Criteria

- Each controller has only the permissions it needs.
- Secret handling is consistent across all resource types.
- Runtime manifests use non root defaults where feasible.
- Security review items are captured and tracked before cutover.
