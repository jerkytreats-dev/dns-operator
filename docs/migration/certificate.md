# Certificate Domain Migration Guide

## Overview

This migration moves certificate management from a local manager into operator-managed shared SAN bundles.

## Migration Goals

- replace file-based cert storage with Secrets
- replace JSON SAN tracking with a `CertificateBundle` resource
- derive SAN membership from published hosts
- preserve browser-safe HTTPS for internal domains

## Recommended Migration Steps

### Step 1: Replace `Certificate` with `CertificateBundle`

The new resource should describe:

- issuer and challenge settings
- secret template or target secret
- desired SAN selection rules
- observed cert state and expiry

### Step 2: Build SAN membership from published services

The controller should derive candidate SANs from:

- `PublishedService` hostnames
- explicit additional domains when needed

Early migration should prefer stable SAN sets over aggressive automatic churn.

### Step 3: Migrate existing certificate state

Preserve:

- existing ACME account data
- existing certificate material
- currently covered SAN names

Import them into Kubernetes Secrets and bundle status without losing recovery context.

### Step 4: Keep DNS-01 behavior

Preserve the Cloudflare DNS-01 workflow, including cleanup and rate-limit handling, but move scheduling into reconciliation logic.

### Step 5: Validate runtime attachment

The migration is only complete when:

- the bundle covers the intended hostnames
- the runtime is serving the corresponding cert
- browsers accept the internal site over HTTPS

## Testing Checklist

- [ ] bundle SAN membership matches intended published hosts
- [ ] imported cert material is readable from Secrets
- [ ] renewals are scheduled correctly
- [ ] rate-limit behavior is surfaced in status
- [ ] browsers accept the generated cert for migrated hosts

## Rollback Plan

- preserve the previous certificate files and account state during import
- keep the old runtime path available until the new secret-backed runtime is verified
- rollback by reattaching the previous certificate material if SAN coverage is wrong

## Summary

The certificate migration should focus on shared explicit SAN bundles that support published internal hosts. The success criterion is browser-valid HTTPS, not just successful issuance.
