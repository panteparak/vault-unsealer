# Production Deployment

This directory contains production-ready deployment manifests for the Vault Auto-unseal Operator.

## Prerequisites

1. Kubernetes cluster (v1.25+)
2. RBAC enabled
3. Prometheus Operator (optional, for metrics)
4. Docker registry to host the operator image

## Quick Start

### 1. Build and Push the Image

```bash
# Build the operator image
make docker-build IMG=your-registry/vault-unsealer:v1.0.0

# Push to registry
make docker-push IMG=your-registry/vault-unsealer:v1.0.0
```

### 2. Update Image Reference

Edit `kustomization.yaml` and update the image references:

```yaml
images:
- name: vault-unsealer
  newName: your-registry/vault-unsealer  # Replace with your registry
  newTag: v1.0.0
```

### 3. Deploy the CRDs

```bash
# Apply the CRDs first
kubectl apply -f ../../config/crd/bases/
```

### 4. Deploy the Operator

```bash
# Deploy using kustomize
kubectl apply -k .

# Or deploy individual manifests
kubectl apply -f namespace.yaml
kubectl apply -f rbac.yaml
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
kubectl apply -f servicemonitor.yaml  # Only if Prometheus Operator is installed
```

### 5. Verify Installation

```bash
# Check operator status
kubectl get pods -n vault-unsealer-system

# Check logs
kubectl logs -n vault-unsealer-system -l app.kubernetes.io/name=vault-unsealer

# Check metrics endpoint (if accessible)
kubectl port-forward -n vault-unsealer-system svc/vault-unsealer-metrics 8443:8443
curl -k https://localhost:8443/metrics
```

## Configuration

### Security Context

The operator runs with a restrictive security context:
- Non-root user (65532)
- Read-only root filesystem
- No privileged escalation
- All capabilities dropped

### Resources

Default resource limits:
- CPU: 10m request, 500m limit
- Memory: 64Mi request, 128Mi limit

Adjust these based on your workload requirements in `deployment.yaml`.

### High Availability

For production, consider:

1. **Multiple Replicas**: Increase replicas in `deployment.yaml` (leader election handles coordination)
2. **Node Affinity**: Configure node affinity for control plane nodes
3. **Pod Disruption Budget**: Add PDB for availability during cluster maintenance

### Monitoring

The deployment includes:
- Prometheus metrics on port 8080
- Health checks on port 8081
- ServiceMonitor for Prometheus Operator

Metrics include:
- Reconciliation counters and duration
- Unseal attempt success/failure rates
- Pod and key count gauges
- Connection status indicators

### Security Considerations

1. **Network Policies**: Consider implementing network policies to restrict operator traffic
2. **RBAC**: The RBAC permissions follow least-privilege principle
3. **Image Security**: Use distroless or minimal base images
4. **Secrets**: Ensure Vault unseal keys are properly secured in Kubernetes Secrets

## Troubleshooting

### Common Issues

1. **CRD Not Found**
   ```bash
   # Ensure CRDs are installed
   kubectl get crd vaultunsealers.ops.autounseal.vault.io
   ```

2. **RBAC Issues**
   ```bash
   # Check service account permissions
   kubectl auth can-i list pods --as=system:serviceaccount:vault-unsealer-system:vault-unsealer-controller
   ```

3. **Image Pull Issues**
   ```bash
   # Check image and registry access
   kubectl describe pod -n vault-unsealer-system -l app.kubernetes.io/name=vault-unsealer
   ```

### Log Analysis

Check operator logs for detailed error information:

```bash
kubectl logs -n vault-unsealer-system -l app.kubernetes.io/name=vault-unsealer --tail=100 -f
```

### Metrics Debugging

Access metrics directly:

```bash
kubectl port-forward -n vault-unsealer-system deployment/vault-unsealer-controller 8080:8080
curl http://localhost:8080/metrics | grep vault_unsealer
```

## Upgrade

To upgrade the operator:

1. Update image tag in `kustomization.yaml`
2. Apply the changes: `kubectl apply -k .`
3. Monitor rollout: `kubectl rollout status -n vault-unsealer-system deployment/vault-unsealer-controller`

## Cleanup

To remove the operator:

```bash
# Delete the operator resources
kubectl delete -k .

# Delete CRDs (this will also delete all VaultUnsealer resources)
kubectl delete -f ../../config/crd/bases/
```

⚠️ **Warning**: Deleting CRDs will remove all VaultUnsealer custom resources in the cluster.