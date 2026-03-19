# Tailnet DNS Endpoint Implementation Plan

## Objective

Implement `TailnetDNSEndpoint` as an operator owned path that gives authoritative DNS a dedicated tailnet reachable endpoint without coupling it to browser facing HTTPS exposure.

The first implementation should target Tailscale operator L3 service exposure and publish the allocated VIP address into resource status.

## Planned Outcome

At the end of this work the operator should be able to:

- reconcile a new `TailnetDNSEndpoint` resource
- create and own a sibling Tailscale exposed `Service`
- observe the Tailscale allocated VIP hostname and IP from Kubernetes `Service` status
- publish that endpoint into `TailnetDNSEndpoint.status`
- let `TailnetDNSConfig` consume the endpoint through `spec.nameserver.endpointRef`
- keep authoritative DNS reachability separate from HTTPS runtime exposure

## Implementation Principles

- keep controller code thin and move business rules into `internal/tailnetdns/`
- keep the primary authoritative DNS `Service` free of Tailscale specific mutation
- use a sibling `Service` as the Tailscale exposure object
- preserve backward compatibility for static nameserver addresses
- ship in small slices with validation at each slice boundary

## Scope

This plan covers:

- API and CRD changes
- domain package design
- controller behavior
- status and event contracts
- tests and validation
- deploy examples and rollout guidance

This plan does not cover:

- a second exposure provider beyond `tailscaleVIPService`
- active packet level reachability probes in the first merge
- browser facing HTTPS runtime changes

## File Level Delivery Map

### API

- `api/tailscale/v1alpha1/tailnetdnsendpoint_types.go`
- `api/tailscale/v1alpha1/tailnetdnsconfig_types.go`
- `api/tailscale/v1alpha1/zz_generated.deepcopy.go`

### Controllers

- `internal/controller/tailscale/tailnetdnsendpoint_controller.go`
- `internal/controller/tailscale/tailnetdnsconfig_controller.go`
- controller setup wiring in `cmd/main.go`

### Domain

- `internal/tailnetdns/endpoint.go`
- `internal/tailnetdns/provider.go`
- `internal/tailnetdns/tailscale_vip_service.go`
- `internal/tailnetdns/status.go`

### Tests

- `api/tailscale/v1alpha1/tailnetdnsendpoint_types_test.go`
- `internal/tailnetdns/endpoint_test.go`
- `internal/tailnetdns/tailscale_vip_service_test.go`
- `internal/controller/tailscale/tailnetdnsendpoint_controller_test.go`
- `internal/controller/tailscale/tailnetdnsconfig_controller_test.go`

### Deploy And Docs

- `config/crd/bases/`
- `config/rbac/`
- `deploy/argocd/README.md`
- `docs/plan/tailnet-dns-endpoint.md`
- `docs/research/tailscale-dns-capabilities.md`

## Owned Resources

For each `TailnetDNSEndpoint`, the controller should manage one sibling `Service` in the same namespace.

Suggested name:

- `<endpoint-name>-tailscale`

Suggested shape:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: internal-authority-tailscale
  namespace: dns-operator-system
  annotations:
    tailscale.com/expose: "true"
    tailscale.com/hostname: internal-authority
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: dns-operator
    app.kubernetes.io/component: coredns
  ports:
    - name: dns-tcp
      port: 53
      protocol: TCP
      targetPort: dns-tcp
    - name: dns-udp
      port: 53
      protocol: UDP
      targetPort: dns-udp
```

The controller should derive selector and DNS ports from the referenced service.

The controller should reject unsupported target services instead of inventing missing selector or port data.

## Detailed Delivery Phases

## Phase 1 API Surface

### Deliverables

- add `TailnetDNSEndpoint` spec and status types
- extend `TailnetDNSConfig` with `nameserver.endpointRef`
- add validation markers and print columns if useful
- generate CRD and deepcopy output

### Detailed Tasks

- create `TailnetDNSEndpointSpec`
- create `TailnetDNSEndpointStatus`
- add `ExposureServiceRef` to status
- define `tailscaleVIPService` as the only supported exposure mode
- update `TailnetNameserver` to support either `address` or `endpointRef`
- add validation that exactly one nameserver source is set
- add comments that explain the operator contract in product terms

### API Decisions

- `spec.service.ref.name` references the authoritative DNS service to mirror
- `spec.exposure.mode` supports only `tailscaleVIPService` in the first release
- `spec.exposure.hostname` becomes the desired Tailscale hostname annotation on the sibling service
- `spec.auth.secretRef` stays on the resource for future provider parity, even if the first implementation relies on cluster level Tailscale operator deployment
- `status.resolvedServiceRef` points at the user supplied target service
- `status.exposureServiceRef` points at the managed sibling service

### Exit Criteria

- CRD generation succeeds
- schema validates the expected good and bad examples
- existing `TailnetDNSConfig` manifests with `nameserver.address` remain valid

## Phase 2 Shared Contracts And Domain Inputs

### Deliverables

- condition constants for endpoint resources
- input and result structs for the domain layer
- helper functions to read target service shape and sibling service status

### Detailed Tasks

- define condition names such as `InputValid`, `ReferencesResolved`, `CredentialsReady`, `EndpointReady`, and `Ready`
- add a domain input type that contains zone, tailnet, desired hostname, target service metadata, selector, and ports
- add a domain result type that contains exposure service reference, endpoint hostname, endpoint DNS name, endpoint address, and condition friendly status details
- add helpers for extracting `TCP 53` and `UDP 53` from a service spec
- add helpers for reading VIP hostname and IP from `Service.status.loadBalancer.ingress`

### Exit Criteria

- domain package compiles without controller dependencies
- helper tests cover empty status, hostname only status, IP only status, and combined status

## Phase 3 Provider Implementation

### Deliverables

- provider interface in `internal/tailnetdns/provider.go`
- real Tailscale VIP service provider in `internal/tailnetdns/tailscale_vip_service.go`
- sibling service renderer in the domain layer

### Detailed Tasks

- define an `EndpointProvider` interface that accepts a domain input and returns a domain result
- implement a renderer for the sibling service metadata, annotations, selector, ports, and owner references
- copy the selector and only the DNS ports from the referenced target service
- always render the sibling service as `ClusterIP`
- set `tailscale.com/expose: "true"`
- set `tailscale.com/hostname` from `spec.exposure.hostname`
- read the resulting VIP data from sibling service status
- return a domain result that the controller can write directly into status

### Important Rules

- never mutate the referenced authoritative DNS service
- never infer missing DNS ports
- never expose non DNS ports on the sibling service
- keep the provider focused on desired state and observed state translation

### Exit Criteria

- provider unit tests prove the sibling service renderer mirrors selector and DNS ports correctly
- provider unit tests prove unsupported target service shapes are rejected with stable errors

## Phase 4 `TailnetDNSEndpoint` Controller

### Deliverables

- reconciler in `internal/controller/tailscale/tailnetdnsendpoint_controller.go`
- manager registration and RBAC updates
- service ownership and watch setup

### Detailed Tasks

- scaffold the reconciler with namespace scoped secret and service reads
- validate the user input early and set `InputValid`
- resolve the target service and set `ReferencesResolved`
- verify the auth secret reference exists and set `CredentialsReady`
- call the domain provider with normalized input
- apply the rendered sibling service through server side apply or controlled create and patch
- set owner reference on the sibling service
- publish `status.resolvedServiceRef`
- publish `status.exposureServiceRef`
- publish endpoint hostname, DNS name, and address
- set `EndpointReady` and `Ready`
- emit events for created sibling service, pending VIP allocation, and endpoint ready transitions

### Watches And Indexes

- watch `TailnetDNSEndpoint`
- watch owned sibling services
- index `TailnetDNSEndpoint.spec.service.ref.name`
- watch target services and enqueue matching endpoint resources through the field index

### Failure Handling

- if the target service disappears, set `ReferencesResolved=False`
- if the sibling service loses annotations, reconcile them back
- if VIP data is absent, keep `EndpointReady=False` with a waiting reason
- if the endpoint loses VIP status after being ready, clear readiness and emit an event

### Exit Criteria

- controller tests cover create, update, delete, target service drift, and delayed VIP allocation
- RBAC includes the minimum required verbs for services, secrets, and status updates

## Phase 5 `TailnetDNSConfig` Integration

### Deliverables

- resolver logic that accepts either a direct address or an endpoint reference
- clear status and event behavior when the referenced endpoint is not ready

### Detailed Tasks

- add a small resolver helper in `internal/tailnetdns/endpoint.go` or a nearby file
- if `nameserver.address` is set, keep current behavior unchanged
- if `nameserver.endpointRef` is set, read the `TailnetDNSEndpoint`
- require `Ready=True`
- require non empty `status.endpointAddress`
- pass the resolved address into existing split DNS ensure logic
- surface a specific condition reason when the endpoint exists but is not ready

### Compatibility Rules

- static address users should see no behavior change
- endpoint reference users should fail clearly if the endpoint is missing or unready
- no migration should be required for existing `DNSRecord`, `PublishedService`, or `CertificateBundle` resources

### Exit Criteria

- unit tests cover direct address, missing endpoint, unready endpoint, and ready endpoint cases

## Phase 6 Observability And Status

### Deliverables

- stable condition reasons
- readable events
- status population rules that avoid partial ambiguity

### Status Contract

`EndpointReady` should mean all of these are true:

- the sibling service exists
- the sibling service matches desired annotations and DNS ports
- sibling service status reports a usable VIP address

`Ready` should mean all of these are true:

- `InputValid=True`
- `ReferencesResolved=True`
- `EndpointReady=True`

### Event Contract

Suggested event reasons:

- `TargetServiceResolved`
- `ExposureServiceApplied`
- `EndpointPending`
- `EndpointReady`
- `EndpointLost`
- `EndpointReferenceInvalid`

### Metrics Follow Up

Metrics are optional for the first merge, but the controller should be structured so future counters and gauges can be added without changing core reconciliation.

### Exit Criteria

- status fields are deterministic under repeated reconcile loops
- event reasons are stable enough for docs and operator runbooks

## Phase 7 Tests And Validation

### Unit Tests

- API validation tests for nameserver source rules
- domain tests for target service contract enforcement
- renderer tests for sibling service annotations, selector, and port copying
- controller tests for status transitions and drift repair
- split DNS integration tests for endpoint reference resolution

### Characterization Tests

- prove no behavior change for `TailnetDNSConfig` that uses a direct address
- prove only DNS ports are mirrored from the target service
- prove a headless target service is rejected

### End To End Validation

The first end to end validation should prove all of these:

- `dig @<vip> host.internal.example.test`
- `dig +tcp @<vip> host.internal.example.test`
- a response large enough to require TCP fallback
- `TailnetDNSConfig` can use the VIP address for split DNS bootstrap
- deleting the endpoint removes the sibling service and clears readiness

### Exit Criteria

- focused Go test targets pass
- docs describe the validation commands and expected outcomes

## Phase 8 Deploy Surface And Examples

### Deliverables

- sample manifests that show `TailnetDNSEndpoint` plus `TailnetDNSConfig`
- any CRD and RBAC generated output committed
- deploy guidance that explains secret expectations and the lack of topology values in repo defaults

### Detailed Tasks

- add an example endpoint resource to deploy docs
- update Argo guidance to show the new endpoint as optional but recommended for tailnet native DNS
- keep zone and hostname examples generic and non identifying

### Exit Criteria

- a reader can follow the docs to create the endpoint and wire split DNS to it

## Phase 9 Rollout Strategy

### Merge Order

The safest merge order is:

1. API and schema changes
2. fake provider plus unit tests
3. endpoint controller with sibling service management
4. split DNS endpoint resolution
5. docs and end to end validation artifacts

### Migration Story

For clusters already using `TailnetDNSConfig.spec.nameserver.address`:

1. create `TailnetDNSEndpoint`
2. wait for `Ready=True`
3. patch `TailnetDNSConfig` to use `nameserver.endpointRef`
4. verify split DNS repair picks up the VIP address
5. remove infra managed static address handling if no longer needed

### Rollback Story

If endpoint based resolution misbehaves:

1. patch `TailnetDNSConfig` back to `nameserver.address`
2. verify split DNS returns to the static address
3. delete the `TailnetDNSEndpoint`
4. confirm the sibling service is garbage collected

## Concrete Work Breakdown

### Slice 1 API And Generated Artifacts

- add types
- run generators
- add schema tests

### Slice 2 Domain And Fake Provider

- add provider contract
- add renderer helpers
- add tests

### Slice 3 Controller And Watches

- add reconciler
- add service watch and field index
- add events and conditions

### Slice 4 Split DNS Integration

- add endpoint reference resolver
- update existing reconciler tests

### Slice 5 Docs And Validation

- add examples
- add validation commands
- document rollout and rollback

## Detailed Acceptance Criteria

The feature is complete when all of these are true:

- a `TailnetDNSEndpoint` can reference the authoritative DNS service in its namespace
- the operator creates a sibling Tailscale exposed `Service`
- the endpoint resource reports the sibling service reference in status
- the endpoint resource reports the allocated VIP address in status
- `TailnetDNSConfig` can resolve `endpointRef` into a usable address
- existing direct address users continue to work unchanged
- focused tests cover the new controller and domain logic
- docs explain implementation, rollout, rollback, and validation

## Deferred Work

These items should stay out of the first merge unless implementation reality forces them in:

- active DNS packet probes from the controller
- additional endpoint provider types
- endpoint level ACL automation
- cross namespace service references
- changes to HTTPS publish exposure
