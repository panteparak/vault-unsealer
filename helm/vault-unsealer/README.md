# Vault Auto-unseal Operator Helm Chart

This Helm chart deploys the Vault Auto-unseal Operator on Kubernetes.

## Prerequisites

- Kubernetes 1.25+
- Helm 3.8+
- HashiCorp Vault cluster (initialized)

## Installing the Chart

### Install CRDs First

```bash
# Install the CRDs
kubectl apply -f https://raw.githubusercontent.com/your-org/vault-autounseal-operator/main/config/crd/bases/ops.autounseal.vault.io_vaultunsealers.yaml
```

### Install the Operator

```bash
# Add the Helm repository (if published)
helm repo add vault-unsealer https://your-org.github.io/vault-autounseal-operator
helm repo update

# Install the chart
helm install vault-unsealer vault-unsealer/vault-unsealer \
  --namespace vault-unsealer-system \
  --create-namespace \
  --set image.repository=your-registry/vault-unsealer \
  --set image.tag=v1.0.0
```

### Install from Source

```bash
# Clone the repository
git clone https://github.com/your-org/vault-autounseal-operator.git
cd vault-autounseal-operator

# Install the chart
helm install vault-unsealer helm/vault-unsealer \
  --namespace vault-unsealer-system \
  --create-namespace \
  --set image.repository=your-registry/vault-unsealer \
  --set image.tag=v1.0.0
```

## Configuration

The following table lists the configurable parameters and their default values.

### Global Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of operator replicas | `1` |
| `nameOverride` | Override the name of the chart | `""` |
| `fullnameOverride` | Override the full name of the chart | `""` |

### Image Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Operator image repository | `your-registry/vault-unsealer` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `image.tag` | Image tag | `v1.0.0` |
| `imagePullSecrets` | Image pull secrets | `[]` |

### Controller Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.logLevel` | Log level (debug, info, warn, error) | `info` |
| `controller.leaderElection` | Enable leader election | `true` |
| `controller.metrics.enabled` | Enable metrics endpoint | `true` |
| `controller.metrics.port` | Metrics port | `8080` |
| `controller.health.port` | Health check port | `8081` |

### Security Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podSecurityContext.runAsNonRoot` | Run as non-root user | `true` |
| `podSecurityContext.runAsUser` | User ID to run as | `65532` |
| `securityContext.allowPrivilegeEscalation` | Allow privilege escalation | `false` |
| `securityContext.readOnlyRootFilesystem` | Read-only root filesystem | `true` |

### Resource Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `128Mi` |
| `resources.requests.cpu` | CPU request | `10m` |
| `resources.requests.memory` | Memory request | `64Mi` |

### RBAC Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create service account | `true` |
| `serviceAccount.annotations` | Service account annotations | `{}` |
| `rbac.create` | Create RBAC resources | `true` |
| `rbac.additionalRules` | Additional RBAC rules | `[]` |

### Monitoring Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.metrics.serviceMonitor.enabled` | Create ServiceMonitor for Prometheus | `false` |
| `controller.metrics.serviceMonitor.namespace` | ServiceMonitor namespace | `""` |
| `controller.metrics.serviceMonitor.labels` | ServiceMonitor labels | `{}` |

## Usage Examples

### Basic Configuration

```yaml
# values.yaml
image:
  repository: your-registry/vault-unsealer
  tag: v1.0.0

controller:
  logLevel: info
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

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: app.kubernetes.io/name
            operator: In
            values:
            - vault-unsealer
        topologyKey: kubernetes.io/hostname
```

### With Prometheus Monitoring

```yaml
# values-monitoring.yaml
controller:
  metrics:
    enabled: true
    serviceMonitor:
      enabled: true
      labels:
        release: prometheus
```

### Production Configuration

```yaml
# values-production.yaml
image:
  repository: your-registry/vault-unsealer
  tag: v1.0.0
  pullPolicy: Always

replicaCount: 2

controller:
  logLevel: warn
  leaderElection: true
  metrics:
    enabled: true
    serviceMonitor:
      enabled: true
      labels:
        app: prometheus

resources:
  limits:
    cpu: 1
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi

podDisruptionBudget:
  enabled: true
  minAvailable: 1

nodeSelector:
  node-role.kubernetes.io/control-plane: ""

tolerations:
- key: node-role.kubernetes.io/control-plane
  operator: Exists
  effect: NoSchedule
```

## Creating a VaultUnsealer Resource

After installing the operator, create a VaultUnsealer resource:

```yaml
apiVersion: ops.autounseal.vault.io/v1alpha1
kind: VaultUnsealer
metadata:
  name: vault-unsealer
  namespace: vault
spec:
  vault:
    url: "https://vault.vault.svc:8200"
    caBundleSecretRef:
      name: vault-ca-secret
      key: ca.crt
  unsealKeysSecretRefs:
    - name: vault-unseal-keys
      key: keys.json
  interval: 60s
  vaultLabelSelector: "app.kubernetes.io/name=vault"
  mode:
    ha: true
  keyThreshold: 3
```

## Monitoring

The operator exposes the following metrics:

- `vault_unsealer_reconciliation_total` - Total reconciliation attempts
- `vault_unsealer_reconciliation_errors_total` - Reconciliation errors
- `vault_unsealer_unseal_attempts_total` - Unseal attempts per pod
- `vault_unsealer_pods_unsealed` - Number of pods unsealed
- `vault_unsealer_pods_checked` - Number of pods checked
- `vault_unsealer_unseal_keys_loaded` - Number of keys loaded
- `vault_unsealer_reconciliation_duration_seconds` - Reconciliation duration
- `vault_unsealer_vault_connection_status` - Vault connection status

## Troubleshooting

### Check Operator Status

```bash
kubectl get pods -n vault-unsealer-system
kubectl logs -n vault-unsealer-system -l app.kubernetes.io/name=vault-unsealer
```

### Check VaultUnsealer Resources

```bash
kubectl get vaultunsealers -A
kubectl describe vaultunsealer vault-unsealer -n vault
```

### Access Metrics

```bash
kubectl port-forward -n vault-unsealer-system svc/vault-unsealer-metrics 8080:8080
curl http://localhost:8080/metrics
```

## Uninstalling

```bash
# Delete VaultUnsealer resources first
kubectl delete vaultunsealers --all -A

# Uninstall the Helm release
helm uninstall vault-unsealer -n vault-unsealer-system

# Optionally delete the CRDs (this will remove all VaultUnsealer resources)
kubectl delete crd vaultunsealers.ops.autounseal.vault.io

# Delete the namespace
kubectl delete namespace vault-unsealer-system
```

## Contributing

Please see the main repository for contribution guidelines.
