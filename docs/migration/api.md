# API Domain Migration Guide

## Overview

This migration is not about deleting the API. It is about changing the API from an imperative runtime mutator into a convenience layer that writes the same durable resources used by direct Kubernetes workflows.

## Migration Summary

**From:** handler-oriented REST API that writes local state  
**To:** convenience API that creates `PublishedService`, `DNSRecord`, `CertificateBundle`, and split-DNS management resources

## Target Behavior

The API should:

- accept publish-oriented requests
- create or update durable resources
- return resource references and readiness information
- avoid becoming a second source of truth

## Recommended Migration Steps

### Step 1: Reframe the API contract

Replace record-centric endpoints with publish-centric endpoints.

Preferred operations:

- create or update `PublishedService`
- list published services
- inspect publish readiness
- trigger or inspect split-DNS repair

### Step 2: Preserve API convenience

Do not force every caller to author CRDs manually.

Instead:

- map API requests to the CRD model
- reuse the same validation rules as the CRDs
- store all durable intent in Kubernetes resources

### Step 3: Align validation

Validation should be shared across:

- request decoding
- CRD schema and webhooks
- controller business rules

This avoids drift between API-created resources and Git-managed resources.

### Step 4: Return durable status

API responses should return:

- resource name and namespace
- effective hostname
- current readiness conditions
- any blocking certificate, DNS, or split-DNS issue

### Step 5: Deprecate low-value legacy routes

Legacy routes built around device discovery or direct proxy rule mutation should be removed or rewritten around the new product model.

## Migration Checklist

- [ ] inventory current API endpoints and classify them as keep, rewrite, or remove
- [ ] define publish-oriented request and response shapes
- [ ] map API write paths to `PublishedService` and related resources
- [ ] align API validation with CRD validation
- [ ] update API docs and examples
- [ ] add end-to-end tests that verify API calls converge to browser-ready internal services

## Rollback Plan

- keep the old API surface behind a feature flag only if needed during the cutover
- preserve compatibility only for endpoints that still map cleanly to the new resource model
- avoid dual sources of truth

## Summary

The API remains part of the product. The migration goal is to make it a clean convenience layer over durable operator-managed resources, centered on internal publishing rather than raw subsystem mutation.

