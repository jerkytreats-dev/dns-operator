# Migration Guide: API Server Command to Operator Runtime

## Overview

This migration changes the process from a legacy HTTP server into an operator-first runtime that can optionally expose a convenience API.

## Migration Goals

- make controller-runtime the center of process lifecycle
- keep probes, metrics, and optional API listeners
- remove device-sync-era startup assumptions
- initialize the new publishing, DNS, cert, and split-DNS controllers together

## Recommended Steps

### Step 1: Make the manager primary

The main binary should start:

- schemes
- shared clients
- reconcilers
- health and readiness probes
- optional convenience API routes

### Step 2: Keep the API as an optional layer

Do not treat the API as deprecated just because the operator exists.

Instead:

- start the API only when configured
- ensure API write paths create durable resources
- share validation and status reporting with controller logic

### Step 3: Remove old background assumptions

Stop designing around:

- device polling loops
- local process supervision
- separate “HTTP mode” versus “controller mode” product concepts

### Step 4: Surface meaningful readiness

Readiness should reflect whether the operator can actually publish services, which may depend on:

- runtime config readiness
- certificate provider configuration
- split-DNS configuration health

## Testing Checklist

- [ ] manager starts with all target reconcilers
- [ ] optional API starts without changing durable-state semantics
- [ ] probes reflect meaningful readiness
- [ ] process shutdown is clean

## Summary

The process should become an operator-first manager that can also host the convenience API. That keeps the fast bootstrap workflow while centering durable state and reconciliation.

