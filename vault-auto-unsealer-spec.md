# Vault Auto-Unsealing Operator — Complete Specification

## Overview

This operator automatically **unseals HashiCorp Vault pods** in Kubernetes.

**Key principles:**
-   **Event-driven**: Watches Pods, VaultUnsealer CRs, and referenced Secrets.
-   **Multi-secret support**: Supports multiple Secrets holding unseal key shares.
-   **Threshold-based**: Submits only `keyThreshold` keys per Vault pod.
-   **HA-aware**: Can unseal all Vault pods in HA mode or stop after the first unseal.
-   **Resilient**: Uses exponential backoff for retries on transient errors.
-   **Observable**: Exposes Prometheus metrics for monitoring.
-   **Secure**: Designed to run with a restrictive security context.

---

## 1. Prerequisites

-   Kubernetes cluster (v1.25+ recommended).
-   HashiCorp Vault installed and **initialized**. The operator only unseals; it does not initialize Vault.
-   Unseal key shares stored in one or more Kubernetes Secrets.
-   Prometheus Operator (optional, for metrics scraping).

---

## 2. CRD Spec

### Example YAML

```yaml
apiVersion: [ops.example.com/v1alpha1](https://ops.example.com/v1alpha1)
kind: VaultUnsealer
metadata:
  name: example-vault-unsealer
  namespace: vault
spec:
  # Connection details for the Vault cluster
  vault:
    url: "[https://vault.vault.svc:8200](https://vault.vault.svc:8200)"
    # Optional: Reference to a secret containing the Vault CA bundle
    caBundleSecretRef:
      name: vault-ca-secret
      key: ca.crt
  
  # References to secrets containing unseal keys
  unsealKeysSecretRefs:
    - name: vault-unseal-keys-a
      namespace: vault
      key: keys.json
    - name: vault-unseal-keys-b
      key: key.txt

  # Periodic sync interval for safety checks
  interval: 60s
  
  # Label selector to find Vault pods
  vaultLabelSelector: "app.kubernetes.io/name=vault"

  # Unsealing strategy
  mode:
    ha: true
  keyThreshold: 3
```

### Go Type Definition

```go
// SecretRef is a reference to a key in a Kubernetes Secret.
type SecretRef struct {
    Name      string `json:"name"`
    Namespace string `json:"namespace,omitempty"`
    Key       string `json:"key"`
}

// VaultConnectionSpec defines how to connect to the Vault cluster.
type VaultConnectionSpec struct {
    URL                string     `json:"url"`
    CABundleSecretRef *SecretRef `json:"caBundleSecretRef,omitempty"`
    InsecureSkipVerify bool       `json:"insecureSkipVerify,omitempty"`
}

// ModeSpec defines the unsealing strategy.
type ModeSpec struct {
    HA bool `json:"ha"`
}

// VaultUnsealerSpec defines the desired state of VaultUnsealer.
type VaultUnsealerSpec struct {
    Vault                VaultConnectionSpec `json:"vault"`
    UnsealKeysSecretRefs []SecretRef         `json:"unsealKeysSecretRefs"`
    Interval             metav1.Duration     `json:"interval,omitempty"`
    VaultLabelSelector   string              `json:"vaultLabelSelector"`
    Mode                 ModeSpec            `json:"mode"`
    KeyThreshold         int                 `json:"keyThreshold,omitempty"`
}

// Condition represents the state of a resource.
type Condition struct {
    Type    string `json:"type"`
    Status  string `json:"status"`
    Reason  string `json:"reason,omitempty"`
    Message string `json:"message,omitempty"`
}

// VaultUnsealerStatus defines the observed state of VaultUnsealer.
type VaultUnsealerStatus struct {
    PodsChecked       []string    `json:"podsChecked,omitempty"`
    UnsealedPods      []string    `json:"unsealedPods,omitempty"`
    Conditions        []Condition `json:"conditions,omitempty"`
    LastReconcileTime metav1.Time `json:"lastReconcileTime,omitempty"`
}
```

---

## 2. Operator Behavior

1. **Watch Events**
   - Pods matching `vaultLabelSelector`
   - VaultUnsealer CRs
   - Referenced Secrets

2. **Reconciliation Logic**
   - For each matching Pod:
     - Skip if not ready or no IP
     - Check `/v1/sys/seal-status`
     - If sealed:
       - Load keys from all Secrets
       - Deduplicate keys
       - Submit up to `keyThreshold`
       - Stop early if HA=false and one pod unsealed
   - Update CR status: `PodsChecked`, `UnsealedPods`, `Conditions`, `LastReconcileTime`
   - Requeue after `interval` for safety

3. **Error Handling**
   - Missing Secrets → `KeysMissing` condition
   - Vault API failure → `VaultAPIFailure` condition, retry
   - Pod unreachable → `PodUnavailable` condition

---

## 3. Secrets

- Multi-secret supported
- JSON array or newline-separated keys
- Base64 encoded in Kubernetes Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: vault-unseal-keys-a
  namespace: vault
type: Opaque
data:
  keys.json: WyJ1bnNlYWxfa2V5XzEiLCJ1bnNlYWxfa2V5XzIiLCJ1bnNlYWxfa2V5XzMiXQ==
```

---

## 4. RBAC & Deployment

### ServiceAccount / Role / RoleBinding

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: vault-unsealer-sa
  namespace: vault
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: vault-unsealer-role
  namespace: vault
rules:
  - apiGroups: [""]
    resources: ["pods", "secrets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["ops.example.com"]
    resources: ["vaultunsealers"]
    verbs: ["get", "list", "watch", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: vault-unsealer-rb
  namespace: vault
subjects:
  - kind: ServiceAccount
    name: vault-unsealer-sa
roleRef:
  kind: Role
  name: vault-unsealer-role
  apiGroup: rbac.authorization.k8s.io
```

### Deployment Example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vault-unsealer
  namespace: vault
spec:
  replicas: 1
  selector:
    matchLabels:
      app: vault-unsealer
  template:
    metadata:
      labels:
        app: vault-unsealer
    spec:
      serviceAccountName: vault-unsealer-sa
      containers:
        - name: operator
          image: my-registry/vault-unsealer:latest
          env:
            - name: LOG_LEVEL
              value: "info"
```

---

## 5. Unit Tests

- **Targets**:
  - CR and Secret parsing
  - Threshold logic & key deduplication
  - Early stop on successful unseal
  - Error handling and condition updates
- **Tools**: `envtest` + `httptest` for mock Vault endpoints
- **Assertions**: CR status fields, requeue logic, correct number of unseal calls

---

## 6. E2E Tests

- **Environment**:
  - Spin up k3s via `testcontainers-go`
  - Install Vault Helm chart with `values.yaml`
- **Flow**:
  1. Deploy k3s container
  2. Helm install Vault HA, auto-unseal disabled
  3. Create Secrets with test keys
  4. Deploy operator
  5. Apply VaultUnsealer CR
  6. Poll CR status and Vault `/v1/sys/seal-status`
  7. Assert all pods are unsealed (HA=true) or first pod (HA=false)
- **Cleanup**: helm uninstall + stop k3s container

---

## 7. Helm `values.yaml` (for tests)

```yaml
server:
  ha:
    enabled: true
    replicas: 3
  standalone:
    enabled: false
  dataStorage:
    enabled: false
  haStorage:
    enabled: false
  bootstrapExpect: 3
ui:
  enabled: true
service:
  type: ClusterIP
inject:
  enabled: false
```

---

## 8. CI / Makefile

```make
.PHONY: test unit e2e build image deploy

test: unit e2e

unit:
    go test ./... -short

e2e:
    go test ./test/e2e -v -run TestE2E --timeout 30m

build:
    go build ./cmd/controller-manager

image:
    docker build -t my-registry/vault-unsealer:dev .

deploy:
    kubectl apply -f deploy/operator-deployment.yaml
```

---

## 9. Logging & Events

- Structured logs (INFO / ERROR)
- Record:
  - Pod name
  - Keys used
  - Failures
- Emit Kubernetes Events for CR changes and unseal attempts

---

## 10. Notes

- Vault must be **initialized** (`vault operator init`) before CR is applied.
- Secrets may span multiple namespaces.
- Operator permissions: watch Pods, read Secrets, update CRs.
- Event-driven design minimizes API polling.
- Optional multi-cluster support via **context7 MCP**.
- Recommended secret rotation procedure: update Secret → operator triggers unseal if necessary.

---

## 11. Operator Reconciliation Flow (ASCII)

```text
                   ┌─────────────────────────┐
                   │ VaultUnsealer CR created │
                   └────────────┬───────────┘
                                │
                                ▼
                  ┌─────────────────────────┐
                  │ Watch Pods & Secrets     │
                  │ matching selectors       │
                  └────────────┬───────────┘
                               │
                               ▼
                 ┌───────────────────────────┐
                 │ Pod Ready?                 │
                 ├───────────────┬───────────┤
                 │ Yes           │ No        │
                 ▼               ▼
     ┌─────────────────┐   ┌───────────────┐
     │ Check Vault API  │   │ Requeue / wait │
     │ /sys/seal-status │   └───────────────┘
     └─────────┬────────┘
               │
               ▼
       ┌───────────────────┐
       │ Is Pod sealed?    │
       ├─────────┬─────────┤
       │ Yes     │ No      │
       ▼         ▼
┌───────────────────┐   ┌──────────────────┐
│ Load keys from     │   │ Update CR status │
│ Secrets           │   │ Pod already unsealed
└─────────┬─────────┘   └──────────────────┘
          │
          ▼
 ┌───────────────────────┐
 │ Deduplicate & select   │
 │ up to keyThreshold     │
 └─────────┬─────────────┘
           │
           ▼
 ┌───────────────────────┐
 │ Submit keys to Vault   │
 │ /sys/unseal            │
 └─────────┬─────────────┘
           │
           ▼
 ┌───────────────────────┐
 │ Is Pod unsealed now?   │
 ├─────────┬─────────────┤
 │ Yes     │ No          │
 ▼         ▼
┌──────────────┐   ┌───────────────┐
│ Update CR    │   │ Retry / set   │
│ UnsealedPods │   │ condition     │
└──────────────┘   └───────────────┘
           │
           ▼
 ┌─────────────────────┐
 │ HA mode enabled?    │
 ├─────────┬───────────┤
 │ Yes     │ No        │
 ▼         ▼
Loop next pod  Stop early
```

---

## 12. Secret Handling Flow (ASCII)

```text
       ┌───────────────────────────┐
       │ Start secret loading      │
       └─────────────┬────────────┘
                     │
                     ▼
       ┌───────────────────────────┐
       │ Iterate over SecretRefs    │
       └─────────────┬────────────┘
                     │
                     ▼
       ┌───────────────────────────┐
       │ Fetch Secret from K8s     │
       │ namespace/key              │
       └─────────────┬────────────┘
                     │
                     ▼
       ┌───────────────────────────┐
       │ Decode Base64 / parse JSON│
       └─────────────┬────────────┘
                     │
                     ▼
       ┌───────────────────────────┐
       │ Append to keys list       │
       └─────────────┬────────────┘
                     │
                     ▼
       ┌───────────────────────────┐
       │ Deduplicate keys          │
       │ Select first N = threshold│
       └─────────────┬────────────┘
                     │
                     ▼
           ┌───────────────────┐
           │ Return key list    │
           └───────────────────┘

```
This Markdown is now fully complete and Claude Code-ready, including:
	•	CRD + Status
	•	Operator logic and error handling
	•	Secrets handling and threshold logic
	•	Unit & e2e tests with k3s + Helm
	•	RBAC and Deployment manifests
	•	CI/Makefile notes
	•	Helm values for Vault test deployment
	•	ASCII flowcharts for operator and secrets flow