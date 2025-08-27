# Vault Auto-unseal Operator

The Vault Auto-unseal Operator automatically unseals HashiCorp Vault pods in Kubernetes clusters, providing seamless integration and high availability for Vault deployments.

## Table of Contents

- [Overview](#overview)
- [Features](#features)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Deployment](#deployment)
- [Monitoring](#monitoring)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)

## Overview

HashiCorp Vault requires manual unsealing after initialization or restarts, which can be problematic in automated environments. This operator solves that challenge by:

- Automatically detecting sealed Vault pods
- Safely retrieving unseal keys from Kubernetes Secrets
- Performing unsealing operations with configurable thresholds
- Supporting both standalone and High Availability (HA) Vault deployments
- Providing comprehensive monitoring and observability

## Features

### Core Functionality
- ✅ **Event-driven unsealing** - Responds to pod state changes in real-time
- ✅ **Multi-secret support** - Load keys from multiple Kubernetes Secrets across namespaces
- ✅ **Threshold-based unsealing** - Configurable key threshold for security
- ✅ **HA-aware operation** - Support for both single-pod and multi-pod unsealing
- ✅ **TLS support** - Secure connections to Vault with custom CA certificates
- ✅ **Cross-namespace secrets** - Access secrets from different namespaces

### Operations & Reliability
- ✅ **Graceful cleanup** - Proper finalizer handling and metric cleanup
- ✅ **Leader election** - High availability operator deployment
- ✅ **Retry logic** - Exponential backoff for transient failures
- ✅ **Status tracking** - Comprehensive status reporting with conditions

### Observability & Security
- ✅ **Prometheus metrics** - 8 comprehensive metrics for monitoring
- ✅ **Structured logging** - Detailed logging with correlation IDs
- ✅ **RBAC compliance** - Minimal required permissions
- ✅ **Security contexts** - Non-root execution with read-only filesystems

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Kubernetes    │    │ VaultUnsealer   │    │    Vault        │
│     Pods        │    │   Operator      │    │    Pods         │
│                 │    │                 │    │                 │
│  ┌───────────┐  │    │  ┌───────────┐  │    │  ┌───────────┐  │
│  │   Pod     │◄─┼────┼─►│Controller │◄─┼────┼─►│   Sealed  │  │
│  │ Events    │  │    │  │ Reconciler│  │    │  │   Vault   │  │
│  └───────────┘  │    │  └───────────┘  │    │  └───────────┘  │
│                 │    │        │        │    │                 │
│  ┌───────────┐  │    │        ▼        │    │  ┌───────────┐  │
│  │ Secrets   │◄─┼────┼─► Secret Loader │    │  │ Unsealed  │  │
│  │(UnsealKeys│  │    │                 │    │  │   Vault   │  │
│  └───────────┘  │    │  ┌───────────┐  │    │  └───────────┘  │
└─────────────────┘    │  │  Metrics  │  │    └─────────────────┘
                       │  │ Collector │  │
                       │  └───────────┘  │
                       └─────────────────┘
```

The operator continuously monitors Vault pods and automatically unseals them when they become sealed, using unseal keys stored securely in Kubernetes Secrets.

## Quick Start

### 1. Prerequisites

- Kubernetes cluster (v1.25+)
- HashiCorp Vault installed and **initialized** 
- Unseal keys stored in Kubernetes Secrets
- RBAC enabled cluster

### 2. Install the Operator

```bash
# Install CRDs
kubectl apply -f https://raw.githubusercontent.com/your-org/vault-autounseal-operator/main/config/crd/bases/ops.autounseal.vault.io_vaultunsealers.yaml

# Install using Helm
helm repo add vault-unsealer https://your-org.github.io/vault-autounseal-operator
helm install vault-unsealer vault-unsealer/vault-unsealer \
  --namespace vault-unsealer-system \
  --create-namespace \
  --set image.repository=your-registry/vault-unsealer \
  --set image.tag=v1.0.0
```

### 3. Create Unseal Keys Secret

```bash
# Create a secret with unseal keys (example)
kubectl create secret generic vault-unseal-keys \
  --namespace vault \
  --from-literal=keys.json='["key1", "key2", "key3", "key4", "key5"]'
```

### 4. Create VaultUnsealer Resource

```yaml
apiVersion: ops.autounseal.vault.io/v1alpha1
kind: VaultUnsealer
metadata:
  name: vault-unsealer
  namespace: vault
spec:
  vault:
    url: "https://vault.vault.svc:8200"
  unsealKeysSecretRefs:
    - name: vault-unseal-keys
      key: keys.json
  interval: 60s
  vaultLabelSelector: "app.kubernetes.io/name=vault"
  mode:
    ha: true
  keyThreshold: 3
```

```bash
kubectl apply -f vaultunsealer.yaml
```

### 5. Verify Operation

```bash
# Check operator status
kubectl get pods -n vault-unsealer-system

# Check VaultUnsealer resource
kubectl get vaultunsealer vault-unsealer -n vault

# View operator logs
kubectl logs -n vault-unsealer-system -l app.kubernetes.io/name=vault-unsealer
```

## Configuration

### VaultUnsealer Resource Specification

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.vault.url` | string | ✅ | Vault cluster URL |
| `spec.vault.caBundleSecretRef` | object | ❌ | CA certificate secret reference |
| `spec.vault.insecureSkipVerify` | bool | ❌ | Skip TLS verification (dev only) |
| `spec.unsealKeysSecretRefs` | array | ✅ | List of secret references containing unseal keys |
| `spec.interval` | duration | ❌ | Reconciliation interval (default: 60s) |
| `spec.vaultLabelSelector` | string | ✅ | Label selector for Vault pods |
| `spec.mode.ha` | bool | ✅ | Enable HA mode (unseal all pods) |
| `spec.keyThreshold` | int | ❌ | Maximum keys to submit (0 = no limit) |

### Secret Formats

The operator supports two secret formats:

**JSON Array Format:**
```json
["unseal_key_1", "unseal_key_2", "unseal_key_3"]
```

**Newline-Separated Format:**
```
unseal_key_1
unseal_key_2
unseal_key_3
```

### Advanced Configuration Examples

**Multi-Secret Setup:**
```yaml
spec:
  unsealKeysSecretRefs:
    - name: vault-keys-primary
      namespace: vault
      key: keys.json
    - name: vault-keys-backup
      namespace: vault-backup
      key: backup-keys.txt
```

**TLS Configuration:**
```yaml
spec:
  vault:
    url: "https://vault.vault.svc:8200"
    caBundleSecretRef:
      name: vault-ca-bundle
      key: ca.crt
```

**Single-Pod Mode:**
```yaml
spec:
  mode:
    ha: false  # Stop after first successful unseal
```

## Deployment

### Production Deployment

For production environments, use the provided manifests:

```bash
# Using Kustomize
kubectl apply -k deploy/production/

# Using individual manifests
kubectl apply -f deploy/production/namespace.yaml
kubectl apply -f deploy/production/rbac.yaml
kubectl apply -f deploy/production/deployment.yaml
kubectl apply -f deploy/production/service.yaml
```

### High Availability Setup

```yaml
# values-ha.yaml
replicaCount: 2

controller:
  leaderElection: true

podDisruptionBudget:
  enabled: true
  minAvailable: 1

resources:
  limits:
    cpu: 1
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

```bash
helm install vault-unsealer vault-unsealer/vault-unsealer -f values-ha.yaml
```

## Monitoring

### Prometheus Metrics

The operator exposes comprehensive metrics:

| Metric Name | Type | Description |
|-------------|------|-------------|
| `vault_unsealer_reconciliation_total` | Counter | Total reconciliation attempts |
| `vault_unsealer_reconciliation_errors_total` | Counter | Reconciliation errors by type |
| `vault_unsealer_unseal_attempts_total` | Counter | Unseal attempts per pod (success/failed) |
| `vault_unsealer_pods_unsealed` | Gauge | Current number of unsealed pods |
| `vault_unsealer_pods_checked` | Gauge | Number of pods checked |
| `vault_unsealer_unseal_keys_loaded` | Gauge | Number of keys loaded from secrets |
| `vault_unsealer_reconciliation_duration_seconds` | Histogram | Time taken for reconciliation |
| `vault_unsealer_vault_connection_status` | Gauge | Vault connection health (1=healthy, 0=unhealthy) |

### Monitoring Setup

**Enable ServiceMonitor:**
```yaml
controller:
  metrics:
    serviceMonitor:
      enabled: true
      labels:
        release: prometheus
```

**Grafana Dashboard:**
A Grafana dashboard is available in `docs/grafana-dashboard.json` with pre-configured panels for all metrics.

### Alerting Rules

Example Prometheus alerting rules:

```yaml
groups:
- name: vault-unsealer
  rules:
  - alert: VaultUnsealerDown
    expr: up{job="vault-unsealer-metrics"} == 0
    for: 5m
    labels:
      severity: critical
    annotations:
      summary: "Vault unsealer operator is down"
      
  - alert: VaultUnsealFailures
    expr: increase(vault_unsealer_reconciliation_errors_total[5m]) > 0
    for: 2m
    labels:
      severity: warning
    annotations:
      summary: "Vault unsealing failures detected"
```

## Security

### RBAC Permissions

The operator requires minimal permissions:

```yaml
# Core permissions
- apiGroups: ["ops.autounseal.vault.io"]
  resources: ["vaultunsealers", "vaultunsealers/status", "vaultunsealers/finalizers"]
  verbs: ["get", "list", "watch", "update", "patch"]

# Kubernetes resources
- apiGroups: [""]
  resources: ["pods", "secrets", "events"]
  verbs: ["get", "list", "watch", "create", "patch"]
```

### Security Context

The operator runs with a restrictive security context:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop: ["ALL"]
```

### Best Practices

1. **Secret Management**: Store unseal keys in encrypted etcd
2. **Network Policies**: Restrict operator network access
3. **Regular Rotation**: Rotate unseal keys periodically
4. **Monitoring**: Monitor all unsealing activities
5. **Access Control**: Limit access to VaultUnsealer resources

## Troubleshooting

### Common Issues

**1. Operator Not Starting**
```bash
# Check operator logs
kubectl logs -n vault-unsealer-system -l app.kubernetes.io/name=vault-unsealer

# Check RBAC permissions
kubectl auth can-i list pods --as=system:serviceaccount:vault-unsealer-system:vault-unsealer-controller
```

**2. VaultUnsealer Not Working**
```bash
# Check VaultUnsealer status
kubectl describe vaultunsealer vault-unsealer -n vault

# Check if pods match label selector
kubectl get pods -l "app.kubernetes.io/name=vault" -n vault

# Verify secret exists and has correct format
kubectl get secret vault-unseal-keys -n vault -o yaml
```

**3. Vault Connection Issues**
```bash
# Check Vault pod IPs and ports
kubectl get pods -o wide -n vault

# Test network connectivity
kubectl run debug --image=busybox -it --rm -- wget -qO- http://vault-pod-ip:8200/v1/sys/seal-status
```

### Debug Mode

Enable debug logging:

```yaml
controller:
  logLevel: debug
```

### Metric Troubleshooting

```bash
# Access metrics directly
kubectl port-forward -n vault-unsealer-system deployment/vault-unsealer 8080:8080
curl http://localhost:8080/metrics | grep vault_unsealer
```

## Contributing

Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on:

- Setting up development environment
- Running tests
- Submitting pull requests
- Code style and standards

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.