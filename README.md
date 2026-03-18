# dns-operator

`dns-operator` is a Kubernetes operator for internal publishing with an operator-managed authoritative zone and configurable published hostname ownership.

It manages:

- authoritative DNS through `DNSRecord`
- browser-facing internal publishing through `PublishedService`
- shared SAN certificate state through `CertificateBundle`
- Tailscale split-DNS bootstrap and repair through `TailnetDNSConfig`

## Zone Ownership

`PublishedService.spec.hostname` must fall under one of the manager's `--publish-zones`.

- Hostnames under `--authoritative-zone` are rendered into the operator-managed authoritative DNS zone.
- Hostnames under another configured publish zone remain valid for HTTPS runtime and `CertificateBundle` SAN derivation, but they are not projected into the operator-managed DNS zone.

The manager deployment exposes both flags directly so GitOps overlays can patch them:

```yaml
args:
  - --authoritative-zone=internal.example.test
  - --publish-zones=internal.example.test,test.jerkytreats.dev
```

This lets an Argo CD overlay keep internal authoritative DNS scoped to `internal.example.test` while still allowing approved external publish names such as `*.test.jerkytreats.dev`.

## Getting Started

### Prerequisites
- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/dns-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/dns-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### Import Existing Reference Data

The reference migration path is available through the importer CLI:

```sh
go run ./cmd/import-reference \
  --namespace dns-operator-system \
  --config /path/to/export/config.yaml \
  --zone-file /path/to/export/internal.example.test.zone \
  --proxy-rules /path/to/export/proxy_rules.json \
  --certificate-domains /path/to/export/certificate_domains.json \
  --caddyfile /path/to/export/Caddyfile \
  --nameserver-address 100.70.110.111 \
  --output dist/imported-resources.yaml \
  --report dist/import-report.json
```

See `docs/migration/import-reference.md` for details on supported inputs, emitted resources, and safety notes.

### Certificate Retry Behavior

`CertificateBundle` performs a Cloudflare DNS preflight before it asks Let's Encrypt to issue or renew a certificate.

- If `_acme-challenge` TXT propagation is not visible yet, the bundle records `status.lastFailureClass=DNSPreflightFailed` and schedules `status.nextAttemptAt` without consuming an ACME order.
- If the ACME provider rate-limits issuance, the bundle records `status.lastFailureClass=RateLimited` and backs off more aggressively.
- `status.consecutiveFailures` tracks repeated failures of the same class so cooldown windows can grow conservatively.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/dns-operator:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/dns-operator/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v2-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## Contributing

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

