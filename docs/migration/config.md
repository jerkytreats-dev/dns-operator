# Configuration Migration Guide

## Overview

This guide helps you migrate from the current file-based configuration system (using YAML files and Viper) to a Kubernetes-native configuration approach using ConfigMaps, Secrets, and CRD specifications.

## Current System

The current configuration system uses:
- **YAML configuration files** (`config.yaml`)
- **Viper** for configuration loading and management
- **Environment variable overrides** (automatic via Viper)
- **Required key validation** at runtime
- **Hot-reload capability** via `Reload()` method
- **Search paths** for config file discovery
- **Singleton pattern** with thread-safe access

### Current Configuration Structure

The configuration file (`config.yaml`) contains:

```yaml
app:
  name: dns-manager
  version: 1.0.0
  environment: development

server:
  host: 0.0.0.0
  port: 8080
  read_timeout: 5s
  write_timeout: 10s
  idle_timeout: 120s
  tls:
    enabled: false
    port: 443
    cert_file: /etc/letsencrypt/live/example.com/cert.pem
    key_file: /etc/letsencrypt/live/example.com/privkey.pem

dns:
  domain: example.com
  internal:
    enabled: true
    origin: "example.com"
    polling:
      enabled: true
      interval: "1h"
  coredns:
    config_path: /etc/coredns/Corefile
    template_path: /etc/coredns/Corefile.template
    zones_path: /etc/coredns/zones
    restart_timeout: "30s"
    health_check_retries: 5

tailscale:
  api_key: "tskey-api-..."
  tailnet: "example.tailscale.com"
  dns:
    zone: "internal.jerkytreats.dev"
    mode: "repair"

proxy:
  enabled: true
  caddy:
    config_path: /app/configs/Caddyfile
    template_path: /etc/caddy/Caddyfile.template
    port: 8080

logging:
  level: debug
  format: json
  output: stdout

certificate:
  provider: "lego"
  email: "admin@example.com"
  domain: "example.com"
  cloudflare_api_token: "CF_API_TOKEN"
  dns_resolvers:
    - "8.8.8.8:53"
    - "1.1.1.1:53"
  dns_timeout: "10s"
  renewal:
    enabled: true
    renew_before: 720h
    check_interval: 24h
```

## Target System

The Kubernetes-native configuration uses:
- **ConfigMaps** for non-sensitive operator configuration
- **Secrets** for sensitive data (API keys, tokens)
- **CRD Specs** for resource-specific configuration
- **Environment Variables** in Deployment/StatefulSet for deployment-specific overrides
- **ConfigMap Watching** for hot-reload (via controller-runtime)
- **CRD Validation** and webhooks for validation

## Migration Steps

### Step 1: Identify Sensitive vs Non-Sensitive Data

Separate your configuration into two categories:

**Sensitive Data (→ Kubernetes Secrets):**
- `tailscale.api_key`
- `certificate.cloudflare_api_token`
- TLS certificate files (if not using cert-manager)
- Any other API keys or tokens

**Non-Sensitive Data (→ ConfigMaps):**
- Server configuration (host, port, timeouts)
- DNS configuration (domain, paths, timeouts)
- Logging configuration
- Proxy configuration
- Tailscale split-DNS defaults
- Certificate provider settings (excluding tokens)

### Step 2: Create Kubernetes Secrets

Create a Secret for sensitive configuration:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: dns-operator-secrets
  namespace: dns-operator
type: Opaque
stringData:
  tailscale-api-key: "tskey-api-..."
  cloudflare-api-token: "CF_API_TOKEN"
```

**Important:** Use `stringData` for plain text values (Kubernetes will base64 encode them automatically), or use `data` with base64-encoded values.

### Step 3: Create ConfigMap for Non-Sensitive Configuration

Create a ConfigMap containing the YAML configuration (excluding sensitive fields):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: dns-operator-config
  namespace: dns-operator
data:
  config.yaml: |
    app:
      name: dns-manager
      version: 1.0.0
      environment: production

    server:
      host: 0.0.0.0
      port: 8080
      read_timeout: 5s
      write_timeout: 10s
      idle_timeout: 120s
      tls:
        enabled: false
        port: 443

    dns:
      domain: example.com
      internal:
        enabled: true
        origin: "example.com"
        polling:
          enabled: true
          interval: "1h"
      coredns:
        config_path: /etc/coredns/Corefile
        template_path: /etc/coredns/Corefile.template
        zones_path: /etc/coredns/zones
        restart_timeout: "30s"
        health_check_retries: 5

    tailscale:
      dns:
        zone: "internal.jerkytreats.dev"
        mode: "repair"

    proxy:
      enabled: true
      caddy:
        config_path: /app/configs/Caddyfile
        template_path: /etc/caddy/Caddyfile.template
        port: 8080

    logging:
      level: info
      format: json
      output: stdout

    certificate:
      provider: "lego"
      email: "admin@example.com"
      domain: "example.com"
      dns_resolvers:
        - "8.8.8.8:53"
        - "1.1.1.1:53"
      dns_timeout: "10s"
      renewal:
        enabled: true
        renew_before: 720h
        check_interval: 24h
```

**Note:** Sensitive fields (`tailscale.api_key`, `certificate.cloudflare_api_token`) are excluded from the ConfigMap.

### Step 4: Update Deployment/StatefulSet

Mount the ConfigMap and Secret in your operator Deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dns-operator
  namespace: dns-operator
spec:
  template:
    spec:
      containers:
      - name: operator
        image: dns-operator:latest
        volumeMounts:
        - name: config
          mountPath: /etc/dns-operator/config.yaml
          subPath: config.yaml
        - name: secrets
          mountPath: /etc/dns-operator/secrets
          readOnly: true
        env:
        # Reference secrets as environment variables
        - name: TAILSCALE_API_KEY
          valueFrom:
            secretKeyRef:
              name: dns-operator-secrets
              key: tailscale-api-key
        - name: CLOUDFLARE_API_TOKEN
          valueFrom:
            secretKeyRef:
              name: dns-operator-secrets
              key: cloudflare-api-token
        # Override config values via environment variables (if needed)
        - name: DNS_DOMAIN
          value: "example.com"
      volumes:
      - name: config
        configMap:
          name: dns-operator-config
      - name: secrets
        secret:
          secretName: dns-operator-secrets
```

### Step 5: Update Operator Code

The operator code needs to be updated to:

1. **Load from ConfigMap instead of file:**
   - Use controller-runtime client to read ConfigMap
   - Parse YAML from ConfigMap data
   - Watch ConfigMap for changes

2. **Load secrets from Kubernetes Secrets:**
   - Use controller-runtime client to read Secrets
   - Watch Secrets for changes
   - Map secret keys to configuration keys

3. **Handle environment variable overrides:**
   - Check environment variables first (highest precedence)
   - Fall back to ConfigMap/Secret values
   - Maintain backward compatibility during transition

### Step 6: Update Configuration Access Pattern

**Before (Viper-based):**
```go
import "internal/config"

apiKey := config.GetString("tailscale.api_key")
domain := config.GetString("dns.domain")
```

**After (Kubernetes-native):**
```go
import (
    "sigs.k8s.io/controller-runtime/pkg/client"
    corev1 "k8s.io/api/core/v1"
)

// In controller setup
configMap := &corev1.ConfigMap{}
err := r.Client.Get(ctx, types.NamespacedName{
    Name:      "dns-operator-config",
    Namespace: "dns-operator",
}, configMap)

secret := &corev1.Secret{}
err := r.Client.Get(ctx, types.NamespacedName{
    Name:      "dns-operator-secrets",
    Namespace: "dns-operator",
}, secret)

// Access values
apiKey := string(secret.Data["tailscale-api-key"])
configYAML := configMap.Data["config.yaml"]
// Parse YAML and access values
```

### Step 7: Implement ConfigMap Watching

Watch ConfigMaps for changes to enable hot-reload:

```go
// In controller setup
err := ctrl.NewControllerManagedBy(mgr).
    For(&corev1.ConfigMap{}).
    Named("dns-operator-config").
    Complete(r)

// In Reconcile
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) error {
    if req.Name == "dns-operator-config" && req.Namespace == "dns-operator" {
        // Reload configuration
        return r.reloadConfig(ctx)
    }
    // ... rest of reconciliation
}
```

### Step 8: Move Resource-Specific Config to CRDs

Configuration specific to DNS records should move to CRD specs:

**Before:**
- Configuration in global `config.yaml`
- Applied to all resources

**After:**
- Resource-specific configuration in CRD `.spec`
- Global defaults in operator ConfigMap
- CRD validation via OpenAPI schema

Example CRD spec:
```yaml
apiVersion: dns.example.com/v1
kind: DNSRecord
metadata:
  name: example-record
spec:
  domain: example.com
  type: A
  ttl: 300
  # Resource-specific config
  certificate:
    enabled: true
    autoRenew: true
```

## Configuration Key Mapping

### Environment Variable Overrides

The operator should support environment variable overrides with the following mapping:

| Config Key | Environment Variable | Example |
|------------|---------------------|---------|
| `tailscale.api_key` | `TAILSCALE_API_KEY` | `tskey-api-...` |
| `tailscale.tailnet` | `TAILSCALE_TAILNET` | `example.tailscale.com` |
| `dns.domain` | `DNS_DOMAIN` | `example.com` |
| `certificate.cloudflare_api_token` | `CLOUDFLARE_API_TOKEN` | `CF_API_TOKEN` |
| `logging.level` | `LOG_LEVEL` | `info` |

### Precedence Order

Configuration values should be resolved in this order (highest to lowest precedence):

1. **Environment Variables** (highest)
2. **CRD Spec** (for resource-specific config)
3. **ConfigMap** (operator-level defaults)
4. **Secret** (for sensitive defaults)
5. **Hard-coded defaults** (lowest)

## Validation Changes

### Before: Runtime Validation

```go
// Modules register required keys
config.RegisterRequiredKey("tailscale.api_key")
config.RegisterRequiredKey("tailscale.tailnet")

// Validate at startup
if err := config.CheckRequiredKeys(); err != nil {
    log.Fatal(err)
}
```

### After: CRD Validation + Webhooks

1. **CRD OpenAPI Schema Validation:**
```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
spec:
  validation:
    openAPIV3Schema:
      properties:
        spec:
          required:
            - domain
            - type
          properties:
            domain:
              type: string
              pattern: '^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$'
```

2. **Validating Webhook for Complex Validation:**
```go
func (v *Validator) ValidateCreate(ctx context.Context, obj runtime.Object) error {
    record := obj.(*DNSRecord)
    // Complex validation logic
    if record.Spec.Domain == "" {
        return fmt.Errorf("domain is required")
    }
    return nil
}
```

3. **Operator Startup Validation:**
```go
// Validate operator-level config at startup
func validateOperatorConfig(configMap *corev1.ConfigMap) error {
    // Check required fields
    if configMap.Data["config.yaml"] == "" {
        return fmt.Errorf("config.yaml missing from ConfigMap")
    }
    return nil
}
```

## Hot Reload Changes

### Before: Manual Reload

```go
// Reload configuration from disk
if err := config.Reload(); err != nil {
    log.Error(err)
}
```

### After: ConfigMap Watching

```go
// ConfigMap changes trigger reconciliation
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) error {
    if req.Name == "dns-operator-config" {
        // Reload configuration from ConfigMap
        configMap := &corev1.ConfigMap{}
        if err := r.Get(ctx, req.NamespacedName, configMap); err != nil {
            return err
        }
        
        // Update cached configuration
        if err := r.updateConfig(configMap); err != nil {
            return err
        }
        
        // Trigger reconciliation of affected resources
        return r.reconcileAffectedResources(ctx)
    }
    // ... rest of reconciliation
}
```

## Testing Migration

### Test Configuration Loading

1. **Create test ConfigMap and Secret:**
```bash
kubectl create configmap dns-operator-config-test \
  --from-file=config.yaml=test-config.yaml \
  -n dns-operator

kubectl create secret generic dns-operator-secrets-test \
  --from-literal=tailscale-api-key=test-key \
  -n dns-operator
```

2. **Verify configuration is loaded correctly:**
```bash
kubectl logs -n dns-operator deployment/dns-operator | grep "Configuration loaded"
```

3. **Test hot-reload:**
```bash
# Update ConfigMap
kubectl patch configmap dns-operator-config-test \
  -n dns-operator \
  --type merge \
  -p '{"data":{"config.yaml":"...updated config..."}}'

# Verify operator picks up changes
kubectl logs -n dns-operator deployment/dns-operator -f
```

## Rollback Plan

If you need to rollback to the file-based configuration:

1. **Keep file-based config as fallback:**
   - Check for mounted config file first
   - Fall back to ConfigMap if file doesn't exist
   - Support both during transition period

2. **Maintain backward compatibility:**
   - Support `--config-file` flag for explicit file path
   - Support ConfigMap mounting as file
   - Environment variables work with both approaches

## Common Issues and Solutions

### Issue: ConfigMap not updating

**Solution:** Ensure ConfigMap is being watched and reconciliation is triggered:
```go
// Watch ConfigMaps
err := ctrl.NewControllerManagedBy(mgr).
    For(&corev1.ConfigMap{}).
    Named("dns-operator-config").
    Complete(r)
```

### Issue: Secret values not accessible

**Solution:** Ensure RBAC permissions are set:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: dns-operator-config
rules:
- apiGroups: [""]
  resources: ["configmaps", "secrets"]
  verbs: ["get", "list", "watch"]
```

### Issue: Environment variable precedence

**Solution:** Check environment variables first, then ConfigMap/Secret:
```go
func getConfigValue(key string) string {
    // 1. Check environment variable
    if envVal := os.Getenv(keyToEnvVar(key)); envVal != "" {
        return envVal
    }
    // 2. Check ConfigMap
    if configMapVal := getFromConfigMap(key); configMapVal != "" {
        return configMapVal
    }
    // 3. Check Secret
    if secretVal := getFromSecret(key); secretVal != "" {
        return secretVal
    }
    // 4. Return default
    return getDefault(key)
}
```

## Summary

The migration from file-based configuration to Kubernetes-native configuration involves:

1. ✅ **Separate sensitive and non-sensitive data** → Secrets and ConfigMaps
2. ✅ **Create Kubernetes resources** → ConfigMap and Secret manifests
3. ✅ **Update Deployment** → Mount ConfigMap and Secret volumes
4. ✅ **Update operator code** → Use controller-runtime client
5. ✅ **Implement watching** → Watch ConfigMaps for hot-reload
6. ✅ **Move validation** → CRD schema and webhooks
7. ✅ **Test thoroughly** → Verify configuration loading and hot-reload

This migration provides better integration with Kubernetes, improved security for sensitive data, and native hot-reload capabilities through ConfigMap watching.

