# Tailscale DNS Capability Whitepaper

## Scope

This paper evaluates Tailscale capabilities that matter for operator-owned authoritative DNS inside the tailnet.

The main question is simple.

- Can Tailscale give `dns-operator` a tailnet native nameserver endpoint without relying on MetalLB or a node owned public style IP

## Checked Versions

- Research date: `2026-03-19`
- Running cluster operator image: `tailscale/k8s-operator:v1.94.2`
- Upstream source snapshot: `2534bc3`
- Upstream source snapshot date: `2026-03-18`

## Goal Context

`dns-operator` needs a nameserver endpoint that Tailscale clients can use as the split DNS authority for the internal zone.

That endpoint must satisfy these requirements.

- it must be reachable from tailnet clients
- it must not depend on browser publishing infrastructure
- it must behave like a real DNS server over the wire
- it should ideally avoid coupling to node identity or node external IP ownership

## Executive Conclusion

Tailscale appears to support the required capability.

The strongest fit is Tailscale operator L3 service exposure, not L7 ingress and not the cluster local `DNSConfig` nameserver feature.

The L3 service exposure path creates a Tailscale VIP service with its own virtual IPs and forwards traffic at the network layer to a Kubernetes `Service`.

That model is a strong match for authoritative DNS because it is not HTTP specific and the forwarding rule is destination IP based rather than port specific.

This changes the status of the problem.

- it is no longer mainly a capability discovery problem
- it is now an implementation and validation problem

## Sources Checked

### Source Set A

- `kubectl get deploy -n tailscale` from the live cluster showed the running operator image `tailscale/k8s-operator:v1.94.2`
- `/tmp/tailscale-src/docs/k8s/operator-architecture.md` documented both L3 ingress and L7 ingress
- `/tmp/tailscale-src/internal/client/tailscale/vip_service.go` described the Tailscale VIP service API object
- `/tmp/tailscale-src/tailcfg/tailcfg.go` described VIP services as having a virtual IP pair distinct from node IPs
- `/tmp/tailscale-src/ipn/serve.go` described service config with a dedicated `Tun` mode for L3 forwarding
- `/tmp/tailscale-src/util/linuxfw/iptables_for_svcs.go` and `/tmp/tailscale-src/util/linuxfw/nftables_for_svcs.go` described the Linux forwarding rules used by the Kubernetes operator for ingress services
- `/tmp/tailscale-src/k8s-operator/apis/v1alpha1/types_tsdnsconfig.go` described the cluster local `DNSConfig` nameserver feature

## Capability Findings

### 1. Tailscale operator supports L3 service exposure

The upstream operator architecture doc describes L3 ingress for a Kubernetes `Service`.

- a user marks a `Service` with `tailscale.com/expose: "true"`
- the operator creates an ingress proxy
- tailnet devices access the service through a Tailscale exposed endpoint

This is a network layer path.

It is not described as an HTTP routing feature.

## 2. The exposed service gets its own Tailscale VIP identity

The upstream VIP service client and tailcfg types show that a Tailscale service has its own service name and its own VIP address pair.

That matters because it means the endpoint can be separate from the node identity.

This aligns well with the idea of a dedicated tailnet native DNS authority endpoint.

## 3. L3 ingress is IP based forwarding, not HTTP proxying

The ingress proxy config in the operator stores a mapping from Tailscale service IP to Kubernetes `ClusterIP`.

The Linux firewall layer then installs DNAT rules for that IP mapping.

The key implementation detail is that the ingress DNAT rule matches only destination IP and rewrites to the Kubernetes service IP.

It does not bind to one TCP port and it does not describe an HTTP path model.

That is exactly the shape we want for DNS traffic.

## 4. Tailscale core distinguishes L3 services from TCP serve handlers

The Tailscale serve config model has two distinct ideas.

- `TCP` handlers for explicit TCP ports
- `Tun` mode for L3 forwarding

That distinction matters because DNS should not be forced into a TCP only or HTTP oriented abstraction.

The presence of a dedicated L3 service mode is strong evidence that authoritative DNS can fit this model.

## 5. The existing Tailscale `DNSConfig` feature is not the feature we want

The upstream `DNSConfig` resource deploys a cluster local nameserver for `ts.net` names.

That feature helps cluster workloads resolve Tailscale names.

It does not expose an arbitrary authoritative DNS server from the cluster back to the tailnet.

So `DNSConfig` is adjacent, but not the solution for `dns-operator` authority exposure.

## DNS Specific Analysis

### Why DNS was originally risky

Authoritative DNS needs both `UDP 53` and `TCP 53`.

If a transport path only supports TCP, or only supports UDP, the resulting nameserver is unreliable in real client behavior.

That concern was valid when the possible Tailscale path was still unclear.

## Why the risk is lower now

The L3 ingress path is implemented as destination IP DNAT to a Kubernetes `Service` IP.

That suggests protocol agnostic forwarding at the IP layer rather than application specific interception.

This is materially different from the L7 ingress path, which is explicitly HTTP and HTTPS oriented.

Because the forwarding rule is IP based, the path is a strong match for DNS traffic.

## What is still not fully proven

The source review gives strong confidence, but not final proof, that DNS will work end to end.

The remaining work is focused validation, not architecture guesswork.

The implementation should still prove:

- `UDP 53` resolution through the Tailscale exposed service
- `TCP 53` resolution through the same endpoint
- large DNS answers that require TCP retry or fallback
- stable split DNS behavior when Tailscale clients use that VIP as the restricted nameserver target

## Product Implications

### For authoritative DNS

- MetalLB is not required for a tailnet native nameserver endpoint
- a node external IP is not strictly required
- a Tailscale VIP service is likely sufficient

### For HTTPS publishing

- this research does not remove the current `80` and `443` conflict for the Caddy runtime
- browser publishing remains a separate exposure problem
- the DNS authority path can now be planned separately from the HTTPS path

### For the operator API

The endpoint feature should model a tailnet managed endpoint, not a requested literal IP.

Tailscale allocates the VIP.

The operator should observe and publish the allocated address in status.

## Recommended Architecture Direction

The preferred first implementation is:

- keep `TailnetDNSConfig` focused on split DNS bootstrap and repair
- add `TailnetDNSEndpoint` as the endpoint owning resource
- implement `TailnetDNSEndpoint` on top of Tailscale operator L3 service exposure
- create and own a sibling Tailscale exposed `Service` that mirrors the authoritative DNS service selector and ports
- publish the Tailscale VIP address into endpoint status
- let `TailnetDNSConfig` consume that status through `endpointRef`

## Open Implementation Questions

- what exact readiness rules should define `EndpointReady`
- how should the controller verify DNS reachability without becoming overly stateful

## Final Assessment

Tailscale appears capable of providing a tailnet native authoritative DNS endpoint for `dns-operator`.

The remaining uncertainty is now at the level of implementation details and DNS validation, not at the level of basic platform capability.
