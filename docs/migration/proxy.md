# Proxy Domain Migration Guide

## Overview

This migration turns the proxy layer into the HTTPS publishing runtime for internal services.

## Migration Goals

- remove `ProxyRule` as the main user-facing resource
- derive runtime config from `PublishedService`
- preserve backend transport features from the legacy Caddy config
- deploy Caddy as operator-managed runtime infrastructure

## Recommended Migration Steps

### Step 1: Replace `ProxyRule` with `PublishedService`

The user contract should move into `PublishedService` fields such as:

- `hostname`
- `publishMode`
- `backend.address`
- `backend.port`
- `backend.protocol`
- optional backend transport flags such as `insecureSkipVerify`

### Step 2: Generate runtime config

The controller should collect all publishable services and generate:

- Caddyfile or equivalent runtime config
- site blocks keyed by exact hostname
- certificate secret attachment info
- redirect behavior for HTTP to HTTPS

### Step 3: Preserve critical legacy features

During migration, carry forward:

- upstream HTTPS support
- `tls_insecure_skip_verify` style backend transport when explicitly required
- deterministic config ordering

### Step 4: Migrate existing proxy rules

Convert each legacy proxy rule into either:

- a `PublishedService`, if it represents a browser-facing internal app
- a direct `DNSRecord`, if it was only serving as an addressable helper record

### Step 5: Validate runtime behavior

Success is not “Caddyfile rendered.” Success is:

- hostname resolves internally
- certificate covers the hostname
- browser loads over HTTPS

## Testing Checklist

- [ ] each migrated service renders a runtime site entry
- [ ] upstream transport flags are preserved
- [ ] certificate secrets are attached correctly
- [ ] HTTP to HTTPS redirects work
- [ ] browser access succeeds for migrated services

## Rollback Plan

- retain the legacy Caddy config until generated runtime output is validated
- rollback by restoring the previous runtime config and previous authoritative DNS target if needed

## Summary

The proxy migration should not create a new `ProxyRule`-centric operator. It should build a publishing runtime derived from `PublishedService`, with the generated Caddy config treated as implementation output.

