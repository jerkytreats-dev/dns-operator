# DNS Reference Architecture Research

## Executive Summary

This document examines the reference DNS service architecture as it relates to porting functionality to a Kubernetes CRD-reconciler pattern using Kubebuilder and controller-runtime. It is a reference-analysis document, while the authoritative implementation plan lives under `docs/plan/`. Target-state terminology in this file is aligned to the current product model: `PublishedService`, `DNSRecord`, `CertificateBundle`, and `TailnetDNSConfig`.

## Architectural Decisions

The following decisions have been made for the `dns-operator` implementation:

1. **One CRD per DNS Record** - Each lower-level authoritative DNS record will be represented as a separate `DNSRecord` resource
2. **Product-Facing Publishing Resource** - `PublishedService` is the primary user-facing resource for internal publishing, while `DNSRecord` remains the lower-level primitive
3. **Focused Controllers with Clear Ownership** - Reconciliation is split across DNS publication, certificate management, HTTPS runtime rendering, and split-DNS repair concerns
4. **ConfigMap Zone Management** - Authoritative zone data will be rendered into Kubernetes `ConfigMap` resources rather than managed through direct filesystem writes
5. **Shared SAN Certificate Management** - `CertificateBundle` will manage shared explicit SAN coverage for published HTTPS hosts
6. **Split-DNS Bootstrap and Repair** - `TailnetDNSConfig` will manage Tailscale split-DNS bootstrap and repair, not device discovery
7. **API Convenience Layer Preserved** - The HTTP API remains a convenience layer over durable CRDs rather than the primary source of truth

## Architecture Overview

### Current Architecture Pattern

The reference DNS service follows a **monolithic API-driven architecture**:

```
┌─────────────────────────────────────────────────────────────┐
│                    Unified Container                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │   API    │  │ CoreDNS  │  │  Caddy   │  │  Cert    │   │
│  │ Service  │  │  Server  │  │  Proxy   │  │ Manager  │   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │
│       │             │             │             │            │
│       └─────────────┴─────────────┴─────────────┘            │
│                    Supervisord                              │
└─────────────────────────────────────────────────────────────┘
```

**Key Characteristics:**
- Single unified container running multiple services via supervisord
- REST API as the primary interface for all operations
- File-based state management (zone files, config files, JSON storage)
- Synchronous request-response model
- Direct file system manipulation for DNS zones and proxy configs

### Target Architecture Pattern

The target architecture will follow a **Kubernetes CRD-reconciler pattern**:

```
┌─────────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                        │
│  ┌──────────────┐         ┌──────────────┐                  │
│  │ Published-   │────────▶│ Controllers  │                  │
│  │ Service      │         │ + Reconcilers│                  │
│  └──────────────┘         └──────┬───────┘                  │
│         ▲                        │                           │
│         │                 ┌──────┴──────────┐                │
│  ┌──────┴──────┐          │ Rendered State  │                │
│  │ DNSRecord   │          │ ConfigMaps,     │                │
│  │ Certificate │          │ Secrets, Status │                │
│  │ Bundle,     │          └──────┬──────────┘                │
│  │ TailnetDNS  │                 │                           │
│  └─────────────┘           ┌─────▼─────┐   ┌─────▼─────┐     │
│                            │ CoreDNS   │   │  Caddy    │     │
│                            │ Runtime   │   │ Runtime   │     │
│                            └───────────┘   └───────────┘     │
└─────────────────────────────────────────────────────────────┘
```

**Key Characteristics:**
- Declarative CRD-based resource definitions
- Event-driven reconciliation loops
- Kubernetes-native state management (etcd)
- Controller-runtime for resource watching and reconciliation
- Separation of concerns across multiple focused controllers
- Durable desired state for publishing, DNS, certificates, and split-DNS repair

## Core Components Analysis

### 1. DNS Record Management

#### Current Implementation

**Location:** `internal/dns/record/service.go`, `internal/dns/coredns/manager.go`

**Responsibilities:**
- Create/update/delete DNS A records in zone files
- Manage zone file serial numbers
- Coordinate DNS record creation with proxy rules
- Validate and normalize DNS names
- Generate records from persisted zone files

**Key Operations:**
```go
// Service layer orchestrates DNS + Proxy
CreateRecord(req CreateRecordRequest, httpRequest ...*http.Request) (*Record, error)
RemoveRecord(req RemoveRecordRequest) error
ListRecords() ([]Record, error)

// CoreDNS manager handles zone file operations
AddRecord(serviceName, name, ip string) error
RemoveRecord(serviceName, name string) error
ListRecords(serviceName string) ([]Record, error)
```

**State Management:**
- Zone files stored on filesystem (`/etc/coredns/zones/{domain}.zone`)
- Zone file format: Standard DNS zone file with SOA, NS, and A records
- Serial number updates on every record change
- Corefile template-based configuration generation

**CRD Mapping Considerations:**

**Proposed CRD Structure:**
```yaml
apiVersion: dns.jerkytreats.dev/v1alpha1
kind: DNSRecord
metadata:
  name: webapp-a
  namespace: default
spec:
  hostname: webapp.internal.example.test
  type: A
  ttl: 300
  values:
    - 198.51.100.53
  owner:
    name: webapp
    namespace: default
status:
  zoneConfigMapName: zone-internal-jerkytreats-dev
  renderedValues:
    - 198.51.100.53
  conditions:
    - type: Ready
      status: "True"
```

**Reconciliation Logic:**
- Watch DNSRecord CRDs
- On create/update: Update rendered zone data via `ConfigMap`
- On delete: Remove from zone file
- Update status with current state
- Handle conflicts and retries

**Challenges:**
1. **Zone File Management:** Need to coordinate multiple DNSRecord resources writing to the same zone file
2. **Serial Number Management:** Zone serials must be monotonically increasing across all records
3. **Atomic Updates:** Multiple records in same zone need atomic updates
4. **State Reconstruction:** Need to rebuild zone file state from CRD resources on startup

**Solutions (DECIDED):**
- **ConfigMap-based Zone Management:** Store zone files in Kubernetes ConfigMaps
- Use a single reconciler per zone that aggregates all DNSRecord resources for that zone
- Store zone serial in ConfigMap annotation or separate ZoneStatus resource
- Track direct `DNSRecord` ownership and `PublishedService`-derived records explicitly
- `DNSRecord` reconciliation will update the appropriate zone `ConfigMap`

### 2. Certificate Management

#### Current Implementation

**Location:** `internal/certificate/manager.go`

**Responsibilities:**
- Let's Encrypt certificate provisioning via DNS-01 challenges
- Certificate renewal and expiration monitoring
- SAN (Subject Alternative Name) management for multi-domain certificates
- Cloudflare DNS challenge integration
- Certificate storage and TLS configuration

**Key Operations:**
```go
ObtainCertificate(domain string) error
AddDomainToSAN(domain string) error
RemoveDomainFromSAN(domain string) error
ValidateAndUpdateSANDomains() error
StartRenewalLoop(domain string)
```

**State Management:**
- Certificate files stored on filesystem (`/etc/letsencrypt/live/{domain}/`)
- Domain storage in JSON file tracking base domain + SAN domains
- ACME user registration persisted to files
- Certificate info parsed from PEM files

**CRD Mapping Considerations:**

**Proposed CRD Structure:**
```yaml
apiVersion: certificate.jerkytreats.dev/v1alpha1
kind: CertificateBundle
metadata:
  name: internal-shared
spec:
  mode: sharedSAN
  publishedServiceSelector:
    matchLabels:
      jerkytreats.dev/publish-mode: httpsProxy
  additionalDomains:
    - internal.example.test
  issuer:
    provider: letsencrypt
    email: ops@example.com
  challenge:
    type: dns01
    cloudflare:
      apiTokenSecretRef:
        name: cloudflare-credentials
        key: api-token
  secretTemplate:
    name: internal-shared-tls
status:
  state: Ready
  effectiveDomains:
    - app.internal.example.test
    - api.internal.example.test
  certificateSecretRef:
    name: internal-shared-tls
    namespace: default
  expiresAt: "2024-12-31T23:59:59Z"
  conditions:
    - type: Ready
      status: "True"
```

**Reconciliation Logic:**
- Watch `CertificateBundle` resources
- Watch published HTTPS hosts when building the effective SAN set
- On create/update: Trigger ACME certificate request/renewal
- Monitor certificate expiration and auto-renew
- Store certificates in Kubernetes `Secret` resources
- Publish certificate references for CoreDNS and Caddy runtime consumption
- Handle rate limiting and retries

**Challenges:**
1. **SAN Management:** Multiple published HTTPS hosts may need the same certificate
2. **Certificate Sharing:** Need to determine how shared SAN membership is derived and reviewed
3. **Rate Limiting:** Let's Encrypt has strict rate limits
4. **Secret Management:** Certificates stored as Kubernetes `Secret` resources

**Solutions (DECIDED):**
- **Derived Shared SAN Management:** `CertificateBundle` will derive effective SAN membership from desired published HTTPS hosts plus any explicit additional domains
- Shared SAN state remains visible and reviewable through `CertificateBundle` spec and status
- Use exponential backoff and rate limit awareness
- Store certificates in `Secret` resources with deterministic naming and labels for discovery
- Debounce renewal and issuance work when SAN membership changes

### 3. HTTPS Publishing Runtime

#### Current Implementation

**Location:** `internal/proxy/manager.go`

**Responsibilities:**
- Caddy reverse proxy rule management
- Template-based Caddyfile generation
- Proxy rule persistence to JSON storage
- Automatic proxy setup for DNS records with ports
- Caddy configuration reload via supervisord

**Key Operations:**
```go
AddRule(proxyRule *ProxyRule) error
RemoveRule(hostname string) error
ListRules() []*ProxyRule
RestoreFromStorage() error
```

**State Management:**
- Proxy rules stored in JSON file (`data/proxy-rules.json`)
- Caddyfile generated from template with all active rules
- Configuration reloaded via supervisord commands

**Target Resource Model:**
```yaml
apiVersion: publish.jerkytreats.dev/v1alpha1
kind: PublishedService
metadata:
  name: webapp
spec:
  hostname: app.internal.example.test
  publishMode: httpsProxy
  backend:
    address: 192.0.2.10
    port: 8080
    protocol: http
  tls:
    mode: sharedSAN
  auth:
    mode: none
status:
  dnsRecordRef:
    name: app-a
    namespace: default
  certificateBundleRef:
    name: internal-shared
    namespace: default
  renderedConfigMapName: caddy-config
  conditions:
    - type: RuntimeReady
      status: "True"
```

**Reconciliation Logic:**
- Watch `PublishedService` resources with `publishMode=httpsProxy`
- Generate Caddy configuration from the effective publishing state
- Update Caddy `ConfigMap` with rendered config
- Trigger Caddy reload (via admin API or sidecar container)
- Handle rule conflicts and validation

**Challenges:**
1. **Caddy Integration:** Need to run Caddy as a separate pod or sidecar
2. **Config Reload:** Caddy admin API or file watching mechanism
3. **Runtime Coordination:** Multiple published services need to be combined into one stable runtime config
4. **State Persistence:** Desired runtime state needs to survive pod restarts without local files

**Solutions (DECIDED):**
- **ConfigMap Caddyfile:** Store generated Caddy config in a Kubernetes `ConfigMap`
- Run Caddy as a deployment or equivalent operator-managed runtime component
- Use a file-based reload path or admin API that is observable and recoverable
- A `PublishedService`-focused controller renders one effective Caddy config from all active published HTTPS hosts
- Remove local proxy rule persistence from the target design

### 4. Tailscale Integration

#### Current Implementation

**Location:** `internal/tailscale/client.go`, `internal/tailscale/sync/manager.go`

**Responsibilities:**
- Tailscale API client for device discovery
- Device IP resolution (100.64.x.x range)
- Automatic DNS record sync for Tailscale devices
- Device annotation and metadata management
- Polling-based device synchronization

**Key Operations:**
```go
ListDevices() ([]Device, error)
GetCurrentDeviceIP() (string, error)
GetDeviceByIP(ip string) (*Device, error)
EnsureInternalZone() error
RefreshDeviceIPs() error
```

**State Management:**
- Device data persisted in JSON file (`data/devices.json`)
- IP cache in memory for change detection
- Device annotations stored separately from Tailscale API data

**CRD Mapping Considerations:**

**Proposed CRD Structure:**
```yaml
apiVersion: tailscale.jerkytreats.dev/v1alpha1
kind: TailnetDNSConfig
metadata:
  name: internal-zone
spec:
  zone: internal.example.test
  nameserver:
    address: 198.51.100.53
  tailnet: example.ts.net
  auth:
    secretRef:
      name: tailnet-admin-credentials
      key: api-key
  behavior:
    mode: bootstrapAndRepair
status:
  configuredNameserver: 198.51.100.53
  driftDetected: false
  lastAppliedAt: "2024-01-01T12:00:00Z"
  conditions:
    - type: SplitDNSReady
      status: "True"
```

**Reconciliation Logic:**
- Watch `TailnetDNSConfig` resources
- Reconcile Tailscale restricted nameserver configuration for the managed internal zone
- Detect drift between desired and effective split-DNS state
- Update status with configured nameserver, last apply time, and drift state
- Provide a rerunnable bootstrap and repair path

**Challenges:**
1. **API Rate Limiting:** Tailscale API has rate limits
2. **Bootstrap vs Repair:** Split-DNS must work for first install and later endpoint drift
3. **Failure Recovery:** Manual break-glass recovery must exist when automation fails
4. **Desired State Ownership:** The authoritative nameserver endpoint must come from durable state, not portal notes

**Solutions (DECIDED):**
- **`TailnetDNSConfig` owns split-DNS intent** - no device discovery or automatic DNS record creation in the target design
- Keep split-DNS changes outside the hot path for per-service publishing reconciles
- Use controller-runtime requeue behavior for drift detection and repair loops
- Implement exponential backoff for rate limit handling
- Use status subresource to track configured nameserver, drift, and repair results
- Preserve a manual recovery path if Tailscale automation is unavailable

### 5. Configuration Management

#### Current Implementation

**Location:** `internal/config/config.go`

**Responsibilities:**
- YAML configuration file loading via Viper
- Environment variable support
- Required key validation
- Configuration hot-reload capability
- Default value management

**Key Operations:**
```go
GetString(key string) string
GetInt(key string) int
GetBool(key string) bool
CheckRequiredKeys() error
Reload() error
```

**State Management:**
- Single YAML config file (`config.yaml`)
- Viper-based configuration with environment variable overrides
- Thread-safe singleton pattern

**CRD Mapping Considerations:**

**Kubernetes-Native Configuration:**
- Use ConfigMaps for non-sensitive configuration
- Use Secrets for sensitive data (API keys, tokens)
- Use CRD spec for resource-specific configuration
- Use operator-level ConfigMap for global settings

**Migration Strategy:**
- Convert YAML config to ConfigMap
- Move secrets to Kubernetes Secrets
- Embed resource-specific config in CRD specs
- Use controller-runtime's client for ConfigMap/Secret access

### 6. Persistence Layer

#### Current Implementation

**Location:** `internal/persistence/file.go`

**Responsibilities:**
- File-based storage with atomic writes
- Backup creation and management
- Thread-safe read/write operations
- Recovery from backup on corruption

**State Management:**
- JSON files for device storage (`data/devices.json`)
- JSON files for proxy rules (`data/proxy-rules.json`)
- Backup files with timestamps
- Atomic write operations via temp files

**CRD Mapping Considerations:**

**Kubernetes-Native Persistence:**
- CRDs stored in etcd (Kubernetes-native)
- No need for file-based persistence
- Use CRD status subresource for computed state
- Use finalizers for cleanup coordination

**Migration Benefits:**
- Automatic persistence via etcd
- Built-in versioning and history
- No manual backup/restore needed
- Distributed and highly available

## Data Flow Analysis

### Current Flow: API Request → DNS Record Creation

```
1. HTTP POST /add-record
   ↓
2. RecordHandler.AddRecord()
   ↓
3. RecordService.CreateRecord()
   ├─→ Validator.ValidateCreateRequest()
   ├─→ DNSManager.AddRecord() → Write zone file
   ├─→ ProxyManager.AddRule() → Generate Caddyfile
   └─→ CertificateManager.AddDomainToSAN() → Trigger cert renewal
   ↓
4. HTTP 201 Created
```

### Target Flow: CRD Creation → Reconciliation

```
1. kubectl apply -f publishedservice.yaml
   ↓
2. Kubernetes API Server → etcd
   ↓
3. PublishedService Controller (Watch)
   ├─→ Reconcile() triggered
   ├─→ Validate PublishedService spec
   ├─→ Project authoritative DNS intent
   ├─→ Update rendered Caddy config
   └─→ Update PublishedService status
   ↓
4. Status reflects current state
```

## State Management Comparison

### Current: File-Based State

**Pros:**
- Simple and direct
- Easy to inspect and debug
- No external dependencies

**Cons:**
- Not distributed
- Manual backup/restore
- Race conditions possible
- No built-in versioning
- Difficult to coordinate across instances

### Target: CRD-Based State

**Pros:**
- Distributed and highly available (etcd)
- Automatic versioning and history
- Built-in conflict resolution
- Kubernetes-native
- Easy to query and filter

**Cons:**
- Requires Kubernetes cluster
- etcd dependency
- More complex setup
- Learning curve for CRD patterns

## Key Migration Considerations

### 1. Resource Granularity

**Decision Point:** How to model DNS records as CRDs?

**Options:**
- **One CRD per DNS record:** Fine-grained, matches current API model
- **One CRD per zone:** Coarse-grained, manages all records in zone
- **Hybrid:** DNSRecord CRD with zone-level coordination

**DECISION:** One CRD per DNS record (DNSRecord) with zone-level coordination via ConfigMap aggregation.

### 2. Controller Architecture

**Decision Point:** Single controller vs multiple controllers?

**Options:**
- **Monolithic Controller:** One controller handles DNS, Proxy, Certificates
- **Separate Controllers:** DNS publication, HTTPS runtime, certificate, and split-DNS repair controllers
- **Hybrid:** Core controller with helper controllers

**DECISION:** Separate controllers with clear boundaries:
- **DNSRecordController:** Manages authoritative DNS records and zone `ConfigMap` output
- **CertificateBundleController:** Manages shared SAN certificate issuance, renewal, and secret publication
- **PublishedServiceController:** Manages HTTPS publishing intent and rendered Caddy config
- **TailnetDNSConfigController:** Manages Tailscale split-DNS bootstrap and repair

### 3. Zone File Management

**Challenge:** Multiple DNSRecord resources need to write to the same zone file.

**Solutions:**
1. **Zone-level Controller:** One controller per zone that aggregates all DNSRecords
2. **ConfigMap-based:** Store zone file in ConfigMap, reconcile all records together
3. **Finalizers:** Use finalizers to coordinate zone file updates

**DECISION:** ConfigMap-based approach with zone-level aggregation in DNSRecordController.
- Each zone has a corresponding ConfigMap (e.g., `zone-{domain}`)
- DNSRecordController watches all DNSRecords and aggregates them by zone
- Zone ConfigMap is updated atomically with all records for that zone
- CoreDNS mounts the ConfigMap as a volume for zone file access

### 4. Certificate Sharing

**Challenge:** Multiple DNSRecords may need the same certificate.

**Solutions:**
1. **CertificateBundle resource:** Shared explicit SAN resource derived from published HTTPS hosts
2. **Automatic Pooling:** Controller automatically groups domains into certificates
3. **Manual Assignment:** Users explicitly manage bundle membership

**DECISION:** `CertificateBundle` with derived shared SAN management.
- `CertificateBundle` derives effective SAN membership from published HTTPS hosts and any explicit additional domains
- The effective SAN set remains visible through resource status
- Renewal is triggered from durable desired state changes with rate-limit-aware backoff

### 5. HTTPS Runtime Coordination

**Challenge:** Multiple published HTTPS hosts need to be combined into a single effective Caddy config.

**Solutions:**
1. **Aggregating Controller:** Single controller that watches all published HTTPS services
2. **ConfigMap Generation:** Generate Caddy config `ConfigMap` from all effective published services
3. **Caddy Admin API:** Use Caddy's dynamic config API

**DECISION:** ConfigMap-based Caddy config generation from `PublishedService`.
- `PublishedServiceController` watches all publishable services
- Generates one effective Caddy config from all active published HTTPS hosts
- Stores generated config in a `ConfigMap` such as `caddy-config`
- Caddy runtime mounts that config and reloads through a defined, observable path

### 6. Tailscale Integration

**Challenge:** How to model Tailscale split-DNS ownership and repair behavior.

**Solutions:**
1. **Bootstrap Job:** One-shot automation for initial split-DNS setup
2. **Controller:** `TailnetDNSConfig` resource with reconcile and repair behavior
3. **Hybrid:** Controller plus explicit rerun path for repairs
4. **Manual Only:** Keep split-DNS configuration outside the product

**DECISION:** `TailnetDNSConfig` with bootstrap and repair automation.
- `TailnetDNSConfigController` manages restricted nameserver state for the internal zone
- The target design does not create device resources or generate DNS records from device inventory
- Split-DNS drift is surfaced through resource status and repairable through rerunnable automation
- Users manage authoritative DNS through `PublishedService` and `DNSRecord`, not through device sync

## API Endpoint Mapping

### Current REST API → CRD Operations

| REST Endpoint | Method | CRD Equivalent |
|--------------|--------|---------------|
| `/add-record` | POST | API creates or updates `PublishedService` or `DNSRecord` |
| `/list-records` | GET | API reads `PublishedService` and `DNSRecord` resources |
| `/remove-record` | DELETE | API deletes or disables `PublishedService` or `DNSRecord` |
| `/health` | GET | Controller health endpoint or Kubernetes probes |

**Note:** The REST API can still be provided as a convenience layer on top of CRDs using an API server or webhook.

## Testing Strategy

### Current Testing Approach

- Unit tests for individual components
- Integration tests with file system mocks
- Manual testing via API endpoints

### Target Testing Approach

- Unit tests with controller-runtime fake client
- Integration tests with testenv (controller-runtime test environment)
- End-to-end tests with kind/minikube clusters
- CRD validation tests
- Webhook validation tests

## Migration Phases

### Phase 1: Foundation and Control Loop
- Establish the controller-runtime project, install shape, and shared status conventions
- Define baseline deployment wiring for the operator and runtime dependencies

### Phase 2: API and Resource Model
- Create `PublishedService`, `DNSRecord`, `CertificateBundle`, and `TailnetDNSConfig`
- Define validation, references, conditions, and example manifests
- Preserve the API convenience layer over durable CRDs

### Phase 3: Authoritative Internal DNS Slice
- Implement the `DNSRecord` reconciler
- Project `PublishedService` hostnames into authoritative DNS output
- Render authoritative zone data into `ConfigMap` resources

### Phase 4: Split-DNS Bootstrap and Repair
- Implement `TailnetDNSConfig` reconcile or equivalent repair automation path
- Manage Tailscale restricted nameserver configuration for `internal.example.test`
- Detect and repair split-DNS drift safely

### Phase 5: Shared SAN Certificate Management
- Implement the `CertificateBundle` reconciler
- Derive SAN membership from published HTTPS hosts
- Model issuance, renewal, and secret publication

### Phase 6: HTTPS Publishing Runtime
- Implement the `PublishedService` reconciler
- Render Caddy configuration into operator-owned runtime resources
- Define reload, deployment, and runtime verification behavior

### Phase 7+: Security, Observability, Testing, and Cutover
- Land RBAC, secret flow hardening, and validation coverage
- Add observability, migration tooling, release flow, and cutover validation

## Open Questions

1. **Zone Rendering Shape:** Should the operator store rendered zone text only, or also keep a more structured intermediate representation for debugging and diffing?

2. **Caddy Runtime Shape:** Should runtime reload use file watching, the admin API, or both for safe recovery?

3. **Namespace Boundaries:** Which resources, if any, should support cross-namespace references in the first cluster target?

4. **Backup/Recovery Guidance:** What operational guidance should accompany etcd-backed CRD state and cutover rollback?

## Summary of Architectural Decisions

The following key decisions have been finalized for the dns-operator implementation:

| Decision Area | Decision | Rationale |
|--------------|----------|-----------|
| **DNS Record Granularity** | One CRD per authoritative DNS record | Fine-grained control, preserves a low-level escape hatch, enables per-record management |
| **Primary Publishing Resource** | `PublishedService` | Keeps the common workflow product-oriented instead of proxy-rule-oriented |
| **Controller Architecture** | Separate controllers with clear boundaries | Separation of concerns, independent scaling, easier testing and maintenance |
| **Zone Management** | ConfigMap-based rendered zone data | Kubernetes-native, atomic updates, version control, easy to mount in CoreDNS |
| **Certificate Management** | `CertificateBundle` with shared explicit SANs | Matches current shared-cert behavior without relying on wildcard assumptions |
| **HTTPS Runtime** | ConfigMap-based rendered Caddy config from `PublishedService` | Kubernetes-native runtime output with stable aggregation |
| **Tailscale Integration** | `TailnetDNSConfig` bootstrap and repair | Explicit ownership of split-DNS without device discovery in the core model |
| **API Surface** | Convenience API over CRDs | Preserves fast bootstrap while keeping CRDs as the source of truth |

### Controller Responsibilities

- **DNSRecordController:** Watches `DNSRecord` resources, aggregates by zone, and updates authoritative zone `ConfigMap` output
- **CertificateBundleController:** Watches `CertificateBundle` resources and published HTTPS hosts, manages shared SAN state, and publishes certificate secrets
- **PublishedServiceController:** Watches `PublishedService` resources, projects DNS intent, and renders Caddy runtime config
- **TailnetDNSConfigController:** Watches `TailnetDNSConfig` resources and repairs restricted nameserver state for the internal zone

## Conclusion

The reference DNS service provides a solid foundation for understanding the domain logic and requirements. The migration to a CRD-Reconciler pattern will require:

1. **Architectural Changes:**
   - Move from API-driven to declarative CRD model
   - Replace file-based state with Kubernetes resources
   - Implement event-driven reconciliation loops

2. **Component Separation:**
   - Split the monolithic service into focused controllers
   - Keep clear boundaries between authoritative DNS, certificate management, HTTPS runtime, and split-DNS repair
   - Use Kubernetes primitives (`ConfigMap`, `Secret`, status) for desired and rendered state

3. **State Management:**
   - Leverage etcd for distributed state
   - Use CRD status subresources for computed state
   - Implement finalizers for cleanup coordination

4. **Integration Points:**
   - CoreDNS integration via rendered zone `ConfigMap` resources
   - Caddy integration via rendered runtime config and observable reload behavior
   - Tailscale API integration for split-DNS bootstrap and repair with proper rate limiting

The reference implementation provides excellent domain knowledge and business logic that can be directly ported to the reconciler pattern, with the main changes being in how state is managed and how operations are triggered (API calls → CRD watches).
