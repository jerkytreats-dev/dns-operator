# Tailscale Migration Guide

## Overview

This migration narrows Tailscale integration to split-DNS ownership for `internal.jerkytreats.dev`.

## What Changes

- remove device discovery from the target architecture
- remove `TailscaleDevice` as a core resource
- add operator-managed split-DNS bootstrap and repair
- make the authoritative nameserver endpoint manageable through Tailscale admin automation

## Recommended Migration Steps

### Step 1: Inventory current split-DNS state

Capture:

- the currently configured restricted nameserver entry
- the internal zone mapping
- the tailnet and credential material needed to update it

### Step 2: Introduce a split-DNS resource

Use a `TailnetDNSConfig` style resource to express:

- zone
- nameserver endpoint
- tailnet identity
- auth secret reference
- desired behavior such as observe, bootstrap, or repair

### Step 3: Move Tailscale logic out of the hot path

Do not tie split-DNS changes to every `PublishedService` reconcile.

Instead:

- run explicit bootstrap when first installing
- run repair when the authoritative nameserver endpoint drifts
- surface drift and repair status clearly

### Step 4: Remove device-oriented code paths

Delete or stop migrating:

- device sync flows
- device HTTP endpoints
- `devices.json` as a source of truth
- DNS record generation from Tailscale device inventory

### Step 5: Validate client behavior

Success means clients on the tailnet:

- resolve `internal.jerkytreats.dev` through the intended authoritative nameserver
- do not resolve that zone outside the tailnet

## Testing Checklist

- [ ] current split-DNS state can be read from Tailscale
- [ ] desired nameserver target can be written safely
- [ ] drift is surfaced in status
- [ ] repair updates the tailnet configuration
- [ ] tailnet clients resolve the zone correctly after repair

## Rollback Plan

- preserve the previous nameserver target before updating split-DNS
- rollback by restoring the old restricted nameserver configuration if resolution fails

## Summary

The Tailscale migration is no longer about device CRDs. It is about making split-DNS for the internal zone manageable, repairable, and aligned with the authoritative nameserver that backs the product.

