# Phase 2 Sample Manifests

These examples show the intended resource shape for the first operator API revision.

## Shared Secrets

### Tailscale admin credentials for split-DNS automation

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: tailscale-admin-credentials
  namespace: dns-operator-system
type: Opaque
stringData:
  api-key: tskey-api-example
```

### Cloudflare API token

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cloudflare-credentials
  namespace: dns-operator-system
type: Opaque
stringData:
  api-token: example-token
```

## `PublishedService`

```yaml
apiVersion: publish.jerkytreats.dev/v1alpha1
kind: PublishedService
metadata:
  name: grafana
  namespace: dns-operator-system
  labels:
    publish.jerkytreats.dev/certificate-bundle: internal-shared
spec:
  hostname: grafana.internal.jerkytreats.dev
  publishMode: httpsProxy
  backend:
    address: 100.70.110.111
    port: 80
    protocol: http
  tls:
    mode: sharedSAN
  auth:
    mode: none
```

## `PublishedService` with nested hostname

```yaml
apiVersion: publish.jerkytreats.dev/v1alpha1
kind: PublishedService
metadata:
  name: api-chat
  namespace: dns-operator-system
  labels:
    publish.jerkytreats.dev/certificate-bundle: internal-shared
spec:
  hostname: api.chat.internal.jerkytreats.dev
  publishMode: httpsProxy
  backend:
    address: 100.70.110.111
    port: 8000
    protocol: http
  tls:
    mode: sharedSAN
  auth:
    mode: none
```

## `PublishedService` with HTTPS backend transport override

```yaml
apiVersion: publish.jerkytreats.dev/v1alpha1
kind: PublishedService
metadata:
  name: sunshine
  namespace: dns-operator-system
  labels:
    publish.jerkytreats.dev/certificate-bundle: internal-shared
spec:
  hostname: sunshine.internal.jerkytreats.dev
  publishMode: httpsProxy
  backend:
    address: 100.70.110.111
    port: 47990
    protocol: https
    transport:
      insecureSkipVerify: true
  tls:
    mode: sharedSAN
  auth:
    mode: none
```

## `DNSRecord` for manual authoritative record management

```yaml
apiVersion: dns.jerkytreats.dev/v1alpha1
kind: DNSRecord
metadata:
  name: ns-record
  namespace: dns-operator-system
spec:
  hostname: ns.internal.jerkytreats.dev
  type: A
  ttl: 300
  values:
    - 100.72.130.7
```

## `CertificateBundle`

```yaml
apiVersion: certificate.jerkytreats.dev/v1alpha1
kind: CertificateBundle
metadata:
  name: internal-shared
  namespace: dns-operator-system
spec:
  mode: sharedSAN
  publishedServiceSelector:
    matchLabels:
      publish.jerkytreats.dev/certificate-bundle: internal-shared
  issuer:
    provider: letsencrypt-staging
    email: admin@example.com
  challenge:
    type: dns01
    cloudflare:
      apiTokenSecretRef:
        name: cloudflare-credentials
        key: api-token
  secretTemplate:
    name: internal-jerkytreats-dev-shared-tls
  renewBefore: 720h
```

## `TailnetDNSConfig`

```yaml
apiVersion: tailscale.jerkytreats.dev/v1alpha1
kind: TailnetDNSConfig
metadata:
  name: internal-zone
  namespace: dns-operator-system
spec:
  zone: internal.jerkytreats.dev
  tailnet: tail1cfaab.ts.net
  nameserver:
    address: 100.72.130.7
  auth:
    secretRef:
      name: tailscale-admin-credentials
      key: api-key
  behavior:
    mode: bootstrapAndRepair
```

## Notes

- These examples assume one operator namespace for the first revision.
- `PublishedService` is the primary interface for humans, agents, and the convenience API.
- `DNSRecord` remains available for lower-level authoritative DNS control and migration imports.
- Shared SAN membership is derived from desired published hosts through `CertificateBundle`.
- Split-DNS automation is durable state, but should run as bootstrap and repair work rather than part of every service reconcile.
