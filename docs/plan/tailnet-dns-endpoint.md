# Tailnet DNS Endpoint Design

## Goal

Introduce an operator-owned tailnet endpoint for authoritative DNS so split DNS can target a dedicated tailnet identity instead of an infra supplied static address.

## Why This Exists

- `TailnetDNSConfig` currently requires a raw nameserver IP.
- That pushes endpoint lifecycle into infra even though the operator owns the DNS runtime.
- DNS authority and HTTPS publishing are different data planes and should not have to share rollout constraints.
- A k3s cluster can bootstrap authoritative DNS before it solves browser facing `80` and `443` exposure.

## Product Outcome

- The operator creates and owns a dedicated tailnet reachable endpoint for authoritative DNS.
- Tailscale allocates the address.
- `TailnetDNSConfig` points at a resource reference instead of a hardcoded IP when the endpoint is operator managed.
- Split DNS repair remains a separate concern from endpoint lifecycle.
- The first implementation should use Tailscale operator L3 service exposure backed by a VIP service.

## Non Goals

- Do not let users request a specific tailnet IP.
- Do not couple this feature to Caddy or HTTPS publishing.
- Do not make `TailnetDNSConfig` responsible for creating endpoint infrastructure.
- Do not expose cross namespace secret or service references.
- Do not default to a dedicated node identity if a VIP service is sufficient.

## New Resource

Add `TailnetDNSEndpoint` under `api/tailscale/v1alpha1/`.

This resource owns the dedicated tailnet identity that forwards DNS traffic to the in cluster authoritative DNS service.

## Proposed API

```go
type TailnetDNSEndpointAuth struct {
	SecretRef common.SecretKeyReference `json:"secretRef"`
}

type TailnetDNSEndpointService struct {
	Ref common.ObjectReference `json:"ref"`
}

type TailnetDNSEndpointExposure struct {
	// +kubebuilder:validation:Enum=tailscaleVIPService
	Mode string `json:"mode"`

	// +kubebuilder:validation:MinLength=1
	Hostname string `json:"hostname"`
}

type TailnetDNSEndpointSpec struct {
	// +kubebuilder:validation:Pattern=`^internal\.example\.test$`
	Zone string `json:"zone"`

	// +kubebuilder:validation:MinLength=1
	Tailnet string `json:"tailnet"`

	Service TailnetDNSEndpointService `json:"service"`

	Auth TailnetDNSEndpointAuth `json:"auth"`

	Exposure TailnetDNSEndpointExposure `json:"exposure"`
}

type TailnetDNSEndpointStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	ResolvedServiceRef *common.ObjectReference `json:"resolvedServiceRef,omitempty"`

	ExposureServiceRef *common.ObjectReference `json:"exposureServiceRef,omitempty"`

	EndpointHostname string `json:"endpointHostname,omitempty"`

	EndpointDNSName string `json:"endpointDNSName,omitempty"`

	EndpointAddress string `json:"endpointAddress,omitempty"`

	LastAppliedAt *metav1.Time `json:"lastAppliedAt,omitempty"`

	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

## Condition Contract

Add one shared condition constant for this feature:

- `EndpointReady`

Expected conditions on `TailnetDNSEndpoint`:

- `InputValid`
- `ReferencesResolved`
- `CredentialsReady`
- `EndpointReady`
- `Ready`

Meaning:

- `InputValid` says the spec is structurally acceptable.
- `ReferencesResolved` says the target `Service` exists and is usable.
- `CredentialsReady` says the Tailscale admin secret was resolved.
- `EndpointReady` says the tailnet endpoint exists and has a usable address.
- `Ready` is true only when the endpoint can be used by split DNS automation.

## TailnetDNSConfig Change

Extend `TailnetNameserver` so it can reference an endpoint resource.

```go
type TailnetNameserver struct {
	Address string `json:"address,omitempty"`

	EndpointRef *common.ObjectReference `json:"endpointRef,omitempty"`
}
```

Validation rule:

- exactly one of `address` or `endpointRef` must be set

This keeps the current static address path working while allowing the operator managed path.

## Example Resources

```yaml
apiVersion: tailscale.jerkytreats.dev/v1alpha1
kind: TailnetDNSEndpoint
metadata:
  name: internal-authority
  namespace: dns-operator-system
spec:
  zone: internal.example.test
  tailnet: example.ts.net
  service:
    ref:
      name: dns-operator-authoritative-dns
  auth:
    secretRef:
      name: tailscale-admin-credentials
      key: api-key
  exposure:
    mode: tailscaleVIPService
    hostname: internal-authority
```

```yaml
apiVersion: tailscale.jerkytreats.dev/v1alpha1
kind: TailnetDNSConfig
metadata:
  name: internal-zone
  namespace: dns-operator-system
spec:
  zone: internal.example.test
  tailnet: example.ts.net
  nameserver:
    endpointRef:
      name: internal-authority
  auth:
    secretRef:
      name: tailscale-admin-credentials
      key: api-key
  behavior:
    mode: bootstrapAndRepair
```

## Controller Boundaries

### TailnetDNSEndpoint Reconciler

Location:

- `internal/controller/tailscale/tailnetdnsendpoint_controller.go`

Responsibilities:

- validate in namespace secret and service references
- fetch the target `Service`
- resolve the Tailscale admin secret
- call the domain layer to ensure a sibling Tailscale exposed `Service` exists
- patch status and emit events

Non responsibilities:

- do not patch split DNS settings
- do not render DNS zone data
- do not manage Caddy runtime state

### Tailnet DNS Domain

Location:

- `internal/tailnetdns/endpoint.go`
- `internal/tailnetdns/provider.go`

Responsibilities:

- define the contract for provisioning a dedicated tailnet endpoint through Tailscale L3 service exposure
- resolve the effective service target from Kubernetes service state
- render and own a sibling exposure `Service` that mirrors the DNS ports and selector of the target service
- normalize endpoint status returned to the controller
- hide provider specific behavior behind an interface

Suggested interface:

```go
type EndpointProvider interface {
	EnsureDNSEndpoint(ctx context.Context, input EnsureEndpointInput) (EnsureEndpointResult, error)
}
```

### TailnetDNSConfig Reconciler

Location:

- `internal/controller/tailscale/tailnetdnsconfig_controller.go`

Responsibilities after the change:

- continue to own split DNS bootstrap and repair
- resolve `spec.nameserver.endpointRef` to `status.endpointAddress`
- call the existing split DNS domain logic with the resolved address

Non responsibilities after the change:

- do not create dedicated endpoints
- do not infer service wiring

## Reconcile Flow

### Endpoint Flow

1. read `TailnetDNSEndpoint`
2. validate `zone`, `tailnet`, `service.ref`, and `auth.secretRef`
3. fetch the target `Service`
4. resolve credentials from the local namespace
5. call the endpoint provider
6. publish `status.endpointAddress`
7. set `EndpointReady=True` when the address is usable

### Split DNS Flow

1. read `TailnetDNSConfig`
2. resolve `nameserver.address` directly or through `nameserver.endpointRef`
3. call `EnsureSplitDNS`
4. set `SplitDNSReady=True` when Tailscale matches desired state

## Service Selection Rule

The first version should use an explicit single `Service` reference.

Why:

- authoritative DNS should have one clear owner
- the split DNS control path should not depend on label selection ambiguity
- infra can still patch the referenced service name by overlay

## Provider Strategy

Research now gives a preferred provider direction.

- Upstream Tailscale operator L3 service exposure creates a Tailscale VIP service for a Kubernetes `Service`.
- The ingress proxy forwards traffic at the network layer through destination IP DNAT to the Kubernetes `ClusterIP`.
- Tailscale core models VIP services with `Tun` mode L3 forwarding, distinct from TCP serve handlers.

This is a strong fit for authoritative DNS and should be the first implementation path.

The remaining requirement is focused end to end validation for both DNS transports:

- UDP `53`
- TCP `53`

The design no longer needs to assume an HTTP oriented ingress path.

The chosen ownership shape is a sibling `Service` dedicated to Tailscale exposure while still targeting the same CoreDNS backend.

This keeps Tailscale specific annotations and status on an operator owned object and leaves the primary authoritative DNS service free for infra managed exposure choices.

The unrelated upstream `DNSConfig` nameserver feature is not the right primitive here because it serves cluster local `ts.net` resolution instead of exposing an authoritative zone server to the tailnet.

## Backward Compatibility

- existing `TailnetDNSConfig.spec.nameserver.address` remains valid
- migration can move users from static address to `endpointRef` one namespace at a time
- no change is required for `DNSRecord`, `PublishedService`, or `CertificateBundle`

## Rollout Plan

1. add the new API types and shared condition constant
2. add a fake endpoint provider and controller unit tests
3. extend `TailnetDNSConfig` to accept `endpointRef`
4. resolve endpoint status address in the split DNS reconciler
5. implement the real provider using Tailscale L3 service exposure and verify DNS over both UDP and TCP
6. add an Argo sample that uses `TailnetDNSEndpoint` plus `TailnetDNSConfig`

The concrete implementation plan is in [Tailnet DNS endpoint implementation plan](tailnet-dns-endpoint-implementation.md).

## Open Questions

- how `EndpointReady` should combine VIP allocation state with DNS reachability checks
- whether `EndpointDNSName` should be the primary human facing status field over `EndpointHostname`
