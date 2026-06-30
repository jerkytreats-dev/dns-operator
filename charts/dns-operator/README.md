# dns-operator Helm Chart

This chart installs the `dns-operator` control plane, CoreDNS runtime, Caddy runtime, and optional bootstrap resources.

## Intent

Use this chart as the primary Argo integration surface.

Keep real domain, tailnet, service type, image tag, and secret names in an infra owned values file.

## Main Values

- `image.repository`
- `image.tag`
- `operator.authoritativeZone`
- `operator.publishZones`
- `runtime.coredns.service.type`
- `runtime.caddy.service.type`
- `runtime.caddy.resources`
- `secrets.tailscaleAdmin.mode`
- `secrets.tailscaleAdmin.name`
- `secrets.tailscaleAdmin.key`
- `secrets.tailscaleAdmin.clientIDKey`
- `secrets.tailscaleAdmin.clientSecretKey`
- `secrets.tailscaleAdmin.scopes`
- `secrets.cloudflare.name`
- `secrets.cloudflare.key`
- `tailnet.name`

## Large WebSocket Payloads

The Caddy runtime proxies WebSocket upgrades as streaming connections. The generated Caddyfile does not impose a WebSocket message-size limit, so large payload failures are usually caused by Caddy pod memory headroom, the upstream application, or a load balancer in front of the runtime.

The chart defaults give the Caddy runtime a `1Gi` memory limit. For larger payloads or multiple concurrent large WebSocket sends, override `runtime.caddy.resources` in your environment values file:

```yaml
runtime:
  caddy:
    resources:
      requests:
        cpu: 50m
        memory: 256Mi
      limits:
        cpu: 500m
        memory: 2Gi
```

## Optional Bootstrap

The chart can also create:

- `TailnetDNSEndpoint`
- `TailnetDNSConfig`
- `CertificateBundle`

Enable only the bootstrap resources you want Argo to own.

Tailscale bootstrap auth defaults to the legacy API-token shape:

```yaml
secrets:
  tailscaleAdmin:
    mode: apiToken
    name: tailscale-admin-credentials
    key: api-key
```

Use OAuth client credentials by switching the mode and pointing both keys at the same Kubernetes `Secret`:

```yaml
secrets:
  tailscaleAdmin:
    mode: oauthClientCredentials
    name: tailscale-admin-credentials
    clientIDKey: client-id
    clientSecretKey: client-secret
    scopes:
      - dns
```

## Example Values

A generic tailnet bootstrap example lives in `charts/dns-operator/values-tailnet-bootstrap-example.yaml`.

## OCI Publish Target

The intended OCI location is `oci://ghcr.io/jerkytreats-dev/charts/dns-operator`.

The release workflow packages the chart, sets the chart version from the git tag, and pushes it to GHCR.

## Argo Use

Argo should consume the released chart and an infra owned values file.

Keep real domain, tailnet, image tag, and secret names out of the chart defaults.

## Render

```sh
helm template dns-operator charts/dns-operator
```

## Validate

```sh
helm lint charts/dns-operator
```
