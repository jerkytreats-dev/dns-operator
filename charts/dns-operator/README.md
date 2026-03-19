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
- `secrets.tailscaleAdmin.name`
- `secrets.tailscaleAdmin.key`
- `secrets.cloudflare.name`
- `secrets.cloudflare.key`
- `tailnet.name`

## Optional Bootstrap

The chart can also create:

- `TailnetDNSEndpoint`
- `TailnetDNSConfig`
- `CertificateBundle`

Enable only the bootstrap resources you want Argo to own.

## Example Values

A generic tailnet bootstrap example lives in `charts/dns-operator/values-tailnet-bootstrap-example.yaml`.

## OCI Publish Target

The intended OCI location is `oci://ghcr.io/jerkytreats/charts/dns-operator`.

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
