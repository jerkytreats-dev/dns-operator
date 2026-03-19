# Research Documentation Summary

## Overview

The `docs/research/` directory should be read through the lens of the product, not through the lens of one-for-one migration of the legacy app.

The product is:

- an authoritative nameserver for `internal.example.test`
- reachable only inside the Tailscale network via split-DNS
- able to publish browser-facing internal services over valid HTTPS
- able to support nested hostnames such as `bar.foo.internal.example.test`
- based on shared explicit SAN certificate management rather than wildcard assumptions
- centered on CRD-first durable state with a convenience API for fast bootstrap

## Core Domains

### 1. **API Domain**
- **File**: `api.md`
- **Purpose**: Convenience control surface for fast bootstrap by humans and agents
- **Key Features**: Creates or updates durable resources without requiring direct CRD authoring
- **Direction**: Keep the API as a first-class interface, not as deprecated compatibility

### 2. **Certificate Domain**
- **File**: `certificate.md`
- **Purpose**: Shared SAN certificate lifecycle for published internal hosts
- **Key Features**: ACME DNS-01 via Cloudflare, effective SAN set derivation, renewal and debounce behavior
- **Direction**: Model certificate ownership around a shared bundle, because wildcard is not the target model

### 3. **DNS Domain**
- **File**: `dns.md`
- **Purpose**: Authoritative DNS and zone rendering for `internal.example.test`
- **Key Features**: Full-hostname records, nested labels, zone aggregation, CoreDNS integration
- **Direction**: Keep `DNSRecord` as the lower-level primitive while projecting `PublishedService` into DNS

### 4. **Proxy Domain**
- **File**: `proxy.md`
- **Purpose**: HTTPS publishing runtime for internal services
- **Key Features**: Caddyfile generation, backend transport controls, HTTP-to-HTTPS redirects
- **Direction**: Drive runtime config from published services rather than a separate proxy-only user resource

### 5. **Tailscale Domain**
- **File**: `tailscale.md`
- **Purpose**: Split-DNS bootstrap and repair for the internal zone
- **Key Features**: Restricted nameserver configuration, drift detection, admin API credentials
- **Direction**: Device discovery is not part of the v1 product direction

### 5a. **Tailscale DNS Capabilities**
- **File**: `tailscale-dns-capabilities.md`
- **Purpose**: Validate whether Tailscale can provide a tailnet native authoritative DNS endpoint
- **Key Features**: L3 service exposure, Tailscale VIP services, DNS transport fit, implementation implications
- **Direction**: Prefer Tailscale L3 service exposure for authoritative DNS reachability and keep it separate from HTTPS publishing

## Infrastructure Domains

### 6. **Configuration**
- **File**: `config.md`
- **Purpose**: Operator and runtime configuration
- **Key Features**: ConfigMap defaults, Secret-backed credentials, runtime wiring
- **Direction**: Keep Kubernetes-native config and remove legacy file assumptions from the target design

### 7. **Logging**
- **File**: `logging.md`
- **Purpose**: Structured logging for controllers, runtime, and API
- **Key Features**: Context-rich logs for publish, cert, and split-DNS flows
- **Direction**: Keep structured logging and improve product-oriented context

### 8. **Persistence**
- **File**: `persistence.md`
- **Purpose**: Historical reference for file-based storage
- **Key Features**: Understand what must be migrated from JSON and rendered files
- **Direction**: Replace application-managed persistence with Kubernetes state and generated artifacts

### 9. **Healthcheck**
- **File**: `healthcheck.md`
- **Purpose**: Health and readiness reporting
- **Key Features**: Manager probes, runtime checks, operational visibility
- **Direction**: Success should correlate to end-user browser and DNS experience, not just process liveness

### 10. **Firewall**
- **File**: `firewall.md`
- **Purpose**: Historical reference only
- **Key Features**: Legacy iptables/ipset management
- **Direction**: Not part of the v1 product roadmap

## Command Domains

### 11. **API Server Command**
- **File**: `cmd-api.md`
- **Purpose**: Entry point and lifecycle for the operator plus convenience API
- **Key Features**: Manager lifecycle, optional API surface, health endpoints
- **Direction**: Kubernetes manager is still primary; API is layered on for bootstrap convenience

### 12. **OpenAPI Generation**
- **File**: `cmd-generate-openapi.md`
- **Purpose**: Documentation generation for the convenience API and resource contracts
- **Key Features**: Build-time schema generation and documentation alignment
- **Direction**: Align generated docs with the new product-first API surface

## Supporting Domains

### 13. **Documentation**
- **File**: `docs.md`
- **Purpose**: Serve useful docs for the convenience API and product contract
- **Key Features**: API docs, examples, and operational guidance
- **Direction**: Keep docs useful; do not assume all HTTP-facing documentation should disappear

### 14. **Validation**
- **File**: `validation.md`
- **Purpose**: DNS and domain validation utilities
- **Key Features**: FQDN validation, nested hostname support, reusable validation logic
- **Direction**: Reuse across CRDs, API validation, and controller logic

## Common Migration Patterns

### 1. **CRD-Based Durable State**
- CRDs remain the source of truth
- Generated runtime artifacts should be derived from CRD state
- API requests should create the same durable resources as direct CRD workflows

### 2. **Controller Reconciliation**
- Controllers own authoritative DNS, shared SAN cert state, and publishing runtime artifacts
- Split-DNS automation should be handled by bootstrap and repair logic, not every publish reconcile

### 3. **ConfigMap/Secret Storage**
- ConfigMaps for operator and runtime defaults
- Secrets for certificate providers, Tailscale admin access, and other sensitive inputs

### 4. **Validation**
- Use schema, API validation, and controller checks together
- Validate full hostnames, nested labels, and product-level publishing constraints

### 5. **Status and Operational Feedback**
- Status must explain whether DNS, HTTPS, cert, and split-DNS are actually ready
- Browser success is a more meaningful acceptance signal than resource creation alone

## Key Takeaways

- The internal publishing product is the design center.
- Device discovery is no longer a product requirement.
- Shared explicit SAN management is a core feature because nested hostnames are required.
- Split-DNS is required for v1, but only as bootstrap and repair automation.
- The convenience API remains important because humans and agents need a fast bootstrap path.
