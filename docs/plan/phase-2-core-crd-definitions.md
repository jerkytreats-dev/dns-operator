# Phase 2: API and Resource Model

## Goal

Define a CRD-first contract that matches the real product: publish internal services under `internal.jerkytreats.dev`, keep browser behavior correct under HTTPS/HSTS, and preserve an API convenience layer for fast bootstrap.

## Scope

- Create the first version of `PublishedService`, `DNSRecord`, `CertificateBundle`, and `TailnetDNSConfig`.
- Define shared condition names, observed generation handling, and status message conventions.
- Model references between resources with explicit typed references.
- Keep the API surface aligned with a future bootstrap HTTP API.
- Move all secret material behind `Secret` references.

## Version Strategy

- Start with `v1alpha1` for all operator owned APIs.
- Keep wire formats simple and additive.
- Avoid speculative fields that do not serve internal publishing.
- Prefer one product-facing resource and a small set of supporting primitives.

## Shared API Conventions

### Metadata

- All resources are namespaced unless there is a strong reason otherwise.
- `metadata.name` should be stable and human meaningful.
- Labels should support zone-level grouping, runtime ownership, and migration provenance.
- Annotations should be reserved for operator internals, migration notes, pause controls, and API-origin metadata.

### Status

Each resource should expose:

- `observedGeneration`
- `conditions`
- a small set of resource-specific status fields that explain effective state

### Common Condition Types

The following condition names should be reused where they fit:

- `Ready`
- `InputValid`
- `ReferencesResolved`
- `CredentialsReady`
- `DNSReady`
- `CertificateReady`
- `RuntimeReady`
- `SplitDNSReady`
- `Accepted`

### Reference Rules

- References should use explicit `name` and optional `namespace` fields.
- Cross namespace references should be opt in and documented per kind.
- Secret references should point to both secret name and key where needed.
- Controllers should surface broken references in conditions rather than logs only.

## Resource Design Priorities

### `PublishedService`

- This is the primary user-facing resource for internal publishing.
- It represents one hostname and the desired way that hostname should be served.
- It should be sufficient for the common workflow used by humans and agents.

**Proposed spec shape**

- `hostname` as the full internal FQDN, including nested names when needed
- `publishMode` as `httpsProxy` or `dnsOnly`
- `backend.address`
- `backend.port`
- `backend.protocol`
- `backend.transport.insecureSkipVerify`
- `tls.mode` as `sharedSAN` or `disabled`
- `auth.mode` as `none` for the first slice

**Proposed status shape**

- `observedGeneration`
- `hostname`
- `url`
- `dnsRecordRef`
- `certificateBundleRef`
- `renderedConfigMapName`
- `conditions`

**Validation goals**

- require a valid hostname within the managed internal zone
- support nested labels
- require backend fields when `publishMode` is `httpsProxy`
- require exactly one supported TLS mode for HTTPS publishing

### `DNSRecord`

- This is the lower-level authoritative DNS primitive.
- It supports manual records, migration imports, and challenge records.
- It is not the primary publishing interface for end users.

**Proposed spec shape**

- `hostname` as the full FQDN inside the managed zone
- `type` as `A`, `AAAA`, `CNAME`, or `TXT`
- `ttl` as an integer with a sane default
- `values` as one or more literal record values
- `owner` as optional reference back to a `PublishedService`

**Proposed status shape**

- `observedGeneration`
- `zoneConfigMapName`
- `renderedValues`
- `conditions`

**Validation goals**

- enforce record type enum
- enforce valid full hostname format, including nested labels
- require at least one value
- bound `ttl` to a sensible minimum

### `CertificateBundle`

- This manages the shared SAN certificate set used by published HTTPS hosts.
- The normal model is one shared bundle for the internal publishing surface.
- SAN membership is derived from desired published hosts and any explicit extra domains.

**Proposed spec shape**

- `mode` as `sharedSAN`
- `publishedServiceSelector`
- `additionalDomains`
- `issuer.provider` as `letsencrypt` or `letsencrypt-staging`
- `issuer.email`
- `challenge.type` as `dns01`
- `challenge.cloudflare.apiTokenSecretRef.name`
- `challenge.cloudflare.apiTokenSecretRef.key`
- `secretTemplate.name`
- `renewBefore`

**Proposed status shape**

- `observedGeneration`
- `state`
- `effectiveDomains`
- `certificateSecretRef`
- `expiresAt`
- `lastIssuedAt`
- `conditions`

**Validation goals**

- require a supported issuer and challenge type
- require secret refs for DNS provider credentials
- ensure target secret naming is deterministic
- keep the bundle compatible with nested internal hostnames

### `TailnetDNSConfig`

- This captures desired Tailscale split-DNS state for the internal zone.
- It exists so the system can bootstrap and repair split-DNS safely.
- It should not be part of the hot path for every published service reconcile.

**Proposed spec shape**

- `zone` as the managed internal zone
- `nameserver.address` as the current authoritative nameserver endpoint
- `tailnet` as the Tailscale tailnet identifier
- `auth.secretRef.name`
- `auth.secretRef.key`
- `behavior.mode` as `bootstrapAndRepair`

**Proposed status shape**

- `observedGeneration`
- `configuredNameserver`
- `lastAppliedAt`
- `driftDetected`
- `conditions`

**Validation goals**

- require the managed internal zone
- require a nameserver address reachable from the tailnet
- require API credentials for split-DNS management

## Deliverables

- Generated CRD manifests with OpenAPI validation.
- Example manifests for each resource kind.
- Shared API conventions for labels, annotations, and status conditions.
- A short design note for how the bootstrap API maps into durable resources.

## Concrete Type Sketches

The following shapes are the preferred baseline for implementation planning.

### Shared Types

```go
type ObjectReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type SecretKeyReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Key       string `json:"key"`
}
```

### `PublishedService`

```go
type PublishBackendTransport struct {
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

type PublishBackend struct {
	Address   string                   `json:"address"`
	Port      int32                    `json:"port,omitempty"`
	Protocol  string                   `json:"protocol,omitempty"`
	Transport *PublishBackendTransport `json:"transport,omitempty"`
}

type PublishTLS struct {
	Mode string `json:"mode,omitempty"`
}

type PublishAuth struct {
	Mode string `json:"mode,omitempty"`
}

type PublishedServiceSpec struct {
	Hostname    string         `json:"hostname"`
	PublishMode string         `json:"publishMode"`
	Backend     PublishBackend `json:"backend,omitempty"`
	TLS         *PublishTLS    `json:"tls,omitempty"`
	Auth        *PublishAuth   `json:"auth,omitempty"`
}

type PublishedServiceStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Hostname           string             `json:"hostname,omitempty"`
	URL                string             `json:"url,omitempty"`
	DNSRecordRef       *ObjectReference   `json:"dnsRecordRef,omitempty"`
	CertificateBundleRef *ObjectReference `json:"certificateBundleRef,omitempty"`
	RenderedConfigMapName string          `json:"renderedConfigMapName,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}
```

### `DNSRecord`

```go
type DNSRecordOwner struct {
	PublishedServiceRef *ObjectReference `json:"publishedServiceRef,omitempty"`
}

type DNSRecordSpec struct {
	Hostname string         `json:"hostname"`
	Type     string         `json:"type"`
	TTL      int32          `json:"ttl,omitempty"`
	Values   []string       `json:"values"`
	Owner    *DNSRecordOwner `json:"owner,omitempty"`
}

type DNSRecordStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	ZoneConfigMapName  string             `json:"zoneConfigMapName,omitempty"`
	RenderedValues     []string           `json:"renderedValues,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}
```

### `CertificateBundle`

```go
type ServiceSelector struct {
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

type BundleIssuer struct {
	Provider string `json:"provider"`
	Email    string `json:"email"`
}

type BundleChallenge struct {
	Type string `json:"type"`
	Cloudflare struct {
		APITokenSecretRef SecretKeyReference `json:"apiTokenSecretRef"`
	} `json:"cloudflare"`
}

type BundleSecretTemplate struct {
	Name string `json:"name"`
}

type CertificateBundleSpec struct {
	Mode                  string               `json:"mode"`
	PublishedServiceSelector *ServiceSelector  `json:"publishedServiceSelector,omitempty"`
	AdditionalDomains     []string             `json:"additionalDomains,omitempty"`
	Issuer                BundleIssuer         `json:"issuer"`
	Challenge             BundleChallenge      `json:"challenge"`
	SecretTemplate        BundleSecretTemplate `json:"secretTemplate"`
	RenewBefore           metav1.Duration      `json:"renewBefore,omitempty"`
}

type CertificateBundleStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	State              string             `json:"state,omitempty"`
	EffectiveDomains   []string           `json:"effectiveDomains,omitempty"`
	CertificateSecretRef *ObjectReference `json:"certificateSecretRef,omitempty"`
	ExpiresAt          *metav1.Time       `json:"expiresAt,omitempty"`
	LastIssuedAt       *metav1.Time       `json:"lastIssuedAt,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}
```

### `TailnetDNSConfig`

```go
type TailnetDNSAuth struct {
	SecretRef SecretKeyReference `json:"secretRef"`
}

type TailnetNameserver struct {
	Address string `json:"address"`
}

type TailnetBehavior struct {
	Mode string `json:"mode"`
}

type TailnetDNSConfigSpec struct {
	Zone       string            `json:"zone"`
	Tailnet    string            `json:"tailnet"`
	Nameserver TailnetNameserver `json:"nameserver"`
	Auth       TailnetDNSAuth    `json:"auth"`
	Behavior   TailnetBehavior   `json:"behavior"`
}

type TailnetDNSConfigStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	ConfiguredNameserver string           `json:"configuredNameserver,omitempty"`
	LastAppliedAt      *metav1.Time       `json:"lastAppliedAt,omitempty"`
	DriftDetected      bool               `json:"driftDetected,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}
```

## Sample Manifests

Concrete examples live in [Phase 2 sample manifests](phase-2-sample-manifests.md).

## Key Decisions

- `PublishedService` is the primary user-facing resource.
- `DNSRecord` remains available for lower-level DNS ownership, migration, and challenge records.
- No device discovery API is part of v1.
- No raw API keys or tokens live in spec fields.
- Status should prefer conditions over ad hoc booleans.
- Namespaced references should be explicit when cross namespace use is allowed.
- The API should be shaped so a convenience HTTP layer can create the same durable state without translation debt.
- Shared SAN certificate membership is a core feature, not just a migration tactic.

## Exit Criteria

- CRDs install cleanly and reject obviously invalid input.
- Example resources pass schema validation.
- Shared status conventions are documented and used across the new resource set.
- The API shape matches the product workflow closely enough to avoid later schema churn.
