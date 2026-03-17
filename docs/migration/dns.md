# DNS Domain Migration Guide

## Overview

This migration turns the DNS layer into an authoritative internal zone pipeline for `internal.jerkytreats.dev`.

## Migration Goals

- make Kubernetes resources the source of truth
- support full hostnames and nested names
- render authoritative zone data for CoreDNS
- derive service records from `PublishedService`
- keep `DNSRecord` for lower-level authoritative entries

## Recommended Migration Steps

### Step 1: Redefine `DNSRecord`

Use a lower-level record model centered on:

- `hostname`
- `type`
- `ttl`
- `values`
- optional owner reference

Do not keep:

- `TailscaleDevice` references
- embedded proxy settings
- split domain/name fields that make nested hostnames awkward

### Step 2: Add `PublishedService` projection

The authoritative DNS output should be built from:

- direct `DNSRecord` objects
- generated records owned by `PublishedService`

This lets the product-facing resource drive the main workflow while preserving a low-level escape hatch.

### Step 3: Render the zone deterministically

The controller should:

- collect all effective records for `internal.jerkytreats.dev`
- sort deterministically
- generate the full zone artifact
- update the runtime only when content actually changes

### Step 4: Migrate existing zone data

When converting the legacy zone:

- preserve NS and other authoritative records
- convert service hostnames into `PublishedService` where appropriate
- convert exceptional/manual entries into direct `DNSRecord`
- verify nested names survive the conversion unchanged

### Step 5: Align validation

Validation must accept:

- nested hostnames
- mixed-case legacy inputs that normalize safely
- full hostnames under `internal.jerkytreats.dev`

Avoid legacy regex patterns that reject valid nested records.

## Testing Checklist

- [ ] `PublishedService` generates expected authoritative records
- [ ] direct `DNSRecord` resources render correctly
- [ ] nested hostnames render correctly
- [ ] conflicts between direct and generated records are surfaced clearly
- [ ] CoreDNS serves the rendered zone correctly
- [ ] no-op reconciles do not trigger unnecessary runtime churn

## Rollback Plan

- preserve the exported legacy zone before cutover
- keep the old authoritative server available until the new zone answers correctly
- rollback by restoring the previous nameserver target if the rendered zone is wrong

## Summary

The DNS migration is not just a storage move from files to ConfigMaps. It is the move to authoritative, product-oriented internal DNS where `PublishedService` drives the main experience and `DNSRecord` remains the lower-level primitive.
