# API Server Command Research

## Executive Summary

The entrypoint should evolve from a monolithic legacy binary into an operator-first process that can also expose a convenience API. The key correction is that the API is still wanted, but it should sit on top of durable operator-managed state rather than replace it.

## Target Shape

The process should own:

- controller-runtime manager lifecycle
- reconcilers for DNS, certificates, publishing runtime, and split-DNS config
- health and readiness endpoints
- an optional HTTP API for fast bootstrap and status inspection

## Design Direction

### Do keep

- single process lifecycle coordination when convenient
- health endpoints
- clear startup sequencing for dependencies and credentials

### Do not keep

- device-sync startup assumptions
- local process supervision assumptions
- the belief that the API server must disappear in Kubernetes

## Recommended Startup Order

1. load operator configuration and secret references
2. initialize manager and schemes
3. initialize external clients needed by reconcilers
4. register reconcilers
5. optionally register convenience API routes
6. start probes, metrics, and API listeners
7. start the manager

## Operational Notes

- The API should share validation and domain logic with the CRD path as much as possible.
- Readiness should reflect whether the system can actually publish services, not just whether the process is running.
- Tailscale split-DNS credentials and certificate provider credentials should be checked early and surfaced clearly.

## Testing Priorities

- manager startup with all reconcilers registered
- optional API startup without diverging from CRD behavior
- graceful shutdown
- dependency initialization failures surfacing useful errors

## Summary

The entrypoint should become an operator-first manager with an optional convenience API layered on top. The change is not “remove HTTP entirely”; it is “move durable state and reconciliation into the center, then keep the API for bootstrap.”


