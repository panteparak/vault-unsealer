# Vault Auto-unseal Operator - Module Architecture Documentation

## Overview

This document provides detailed documentation of each module's responsibility, logic, and interaction patterns within the Vault Auto-unseal Operator. The operator follows a clean architecture with clear separation of concerns and well-defined module boundaries.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Vault Auto-unseal Operator                       │
├─────────────────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │     CMD     │  │     API     │  │   INTERNAL  │  │       CONFIG        │ │
│  │   (main.go) │  │ (v1alpha1)  │  │  (modules)  │  │    (manifests)      │ │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────────────┘ │
│         │                │                │                       │         │
│         └────────────────┼────────────────┼───────────────────────┘         │
│                          │                │                                 │
│  ┌─────────────────────────────────────────────────────────────────────────┐ │
│  │                        INTERNAL MODULES                                 │ │
│  │  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────────────────┐ │ │
│  │  │ CONTROLLER  │ │   SECRETS   │ │    VAULT    │ │      LOGGING        │ │ │
│  │  │             │ │   LOADER    │ │   CLIENT    │ │                     │ │ │
│  │  └─────────────┘ └─────────────┘ └─────────────┘ └─────────────────────┘ │ │
│  │  ┌─────────────┐                                                         │ │
│  │  │   METRICS   │                                                         │ │
│  │  │             │                                                         │ │
│  │  └─────────────┘                                                         │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                               │
│  ┌─────────────────────────────────────────────────────────────────────────┐ │
│  │                        SUPPORTING MODULES                               │ │
│  │  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐                       │ │
│  │  │    TEST     │ │    HELM     │ │   DEPLOY    │                       │ │
│  │  │    (e2e)    │ │   CHART     │ │ (production)│                       │ │
│  │  └─────────────┘ └─────────────┘ └─────────────┘                       │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Core Modules

### 1. Main Entry Point (`cmd/main.go`)

**Purpose**: Application bootstrap and dependency injection

**Responsibilities**:
- Initialize the operator runtime and scheme
- Configure logging, metrics, and health checks
- Set up controller manager with proper configuration
- Handle graceful shutdown and leader election
- Register all controllers and webhooks

**Key Components**:
- Controller manager setup with metrics server
- Scheme registration for custom resources
- Signal handling for graceful shutdown
- TLS certificate management for webhooks

**Interaction Pattern**:
```go
main() → setupManager() → registerControllers() → startManager()
```

**Location**: `cmd/main.go:main()`

---

### 2. API Module (`api/v1alpha1/`)

#### 2.1 VaultUnsealer Types (`vaultunsealer_types.go`)

**Purpose**: Define the Kubernetes Custom Resource Definition (CRD) schema

**Responsibilities**:
- Define `VaultUnsealerSpec` structure for user configuration
- Define `VaultUnsealerStatus` structure for operational state
- Provide JSON/YAML serialization tags
- Support Kubernetes validation and OpenAPI schema generation

**Key Data Structures**:
```go
type VaultUnsealerSpec struct {
    Vault                VaultConnectionSpec `json:"vault"`
    UnsealKeysSecretRefs []SecretRef         `json:"unsealKeysSecretRefs"`
    Interval             *metav1.Duration    `json:"interval,omitempty"`
    VaultLabelSelector   string              `json:"vaultLabelSelector"`
    Mode                 ModeSpec            `json:"mode"`
    KeyThreshold         int                 `json:"keyThreshold,omitempty"`
}

type VaultUnsealerStatus struct {
    PodsChecked       []string    `json:"podsChecked,omitempty"`
    UnsealedPods      []string    `json:"unsealedPods,omitempty"`
    LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`
    Conditions        []Condition  `json:"conditions,omitempty"`
}
```

**Supporting Types**:
- `SecretRef`: Reference to Kubernetes secrets containing unseal keys
- `VaultConnectionSpec`: Vault cluster connection configuration
- `ModeSpec`: HA vs single-node operation mode
- `Condition`: Kubernetes-style condition reporting

**Location**: `api/v1alpha1/vaultunsealer_types.go`

#### 2.2 Group Version Info (`groupversion_info.go`)

**Purpose**: API group and version metadata

**Responsibilities**:
- Define API group (`ops.autounseal.vault.io`)
- Define API version (`v1alpha1`)
- Register scheme with runtime

**Location**: `api/v1alpha1/groupversion_info.go`

---

### 3. Controller Module (`internal/controller/`)

#### 3.1 VaultUnsealer Controller (`vaultunsealer_controller.go`)

**Purpose**: Core reconciliation logic for VaultUnsealer resources

**Responsibilities**:
- Watch for VaultUnsealer resource changes
- Discover and monitor Vault pods using label selectors
- Load unseal keys from referenced Kubernetes secrets
- Execute unseal operations on sealed Vault pods
- Update resource status and conditions
- Handle finalizers for cleanup
- Emit metrics and structured logs

**Key Functions**:

```go
// Primary reconciliation loop
func (r *VaultUnsealerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)

// Handles cleanup when resource is being deleted
func (r *VaultUnsealerReconciler) handleFinalizer(ctx context.Context, vaultUnsealer *opsv1alpha1.VaultUnsealer) error

// Main unsealing logic
func (r *VaultUnsealerReconciler) unsealPods(ctx context.Context, vaultUnsealer *opsv1alpha1.VaultUnsealer,
    pods []corev1.Pod, unsealKeys []string, reconciledID string) ([]string, []string, error)

// Updates resource status with current state
func (r *VaultUnsealerReconciler) updateStatus(ctx context.Context, vaultUnsealer *opsv1alpha1.VaultUnsealer,
    podsChecked, unsealedPods []string, err error, reconciledID string) error
```

**Reconciliation Flow**:
1. **Resource Retrieval**: Fetch VaultUnsealer resource
2. **Finalizer Management**: Handle deletion if needed
3. **Pod Discovery**: Find Vault pods using label selector
4. **Key Loading**: Load unseal keys from secrets
5. **Unseal Operations**: Process each pod for unsealing
6. **Status Updates**: Update resource status and conditions
7. **Metrics Recording**: Update Prometheus metrics
8. **Requeue Logic**: Schedule next reconciliation

**Error Handling**:
- Transient errors trigger exponential backoff requeue
- Permanent errors are reported in status conditions
- All errors are logged with structured context
- Metrics track error types and frequencies

**Location**: `internal/controller/vaultunsealer_controller.go`

---

### 4. Secrets Module (`internal/secrets/`)

#### 4.1 Secrets Loader (`loader.go`)

**Purpose**: Load and process unseal keys from Kubernetes secrets

**Responsibilities**:
- Support multiple secret formats (JSON arrays, newline-separated text)
- Handle cross-namespace secret references
- Implement key deduplication logic
- Enforce key threshold limits
- Provide consistent error handling

**Key Functions**:

```go
// Main entry point for loading keys
func (l *Loader) LoadUnsealKeys(ctx context.Context, namespace string,
    secretRefs []opsv1alpha1.SecretRef, keyThreshold int) ([]string, error)

// Load keys from individual secret
func (l *Loader) loadKeysFromSecret(ctx context.Context, namespace string,
    secretRef opsv1alpha1.SecretRef) ([]string, error)

// Parse different key formats
func (l *Loader) parseKeys(data []byte) ([]string, error)
```

**Supported Formats**:
1. **JSON Array**: `["key1", "key2", "key3"]`
2. **Newline-separated**: `key1\nkey2\nkey3`

**Processing Logic**:
1. Iterate through all secret references
2. Resolve namespace (use default if not specified)
3. Fetch secret from Kubernetes API
4. Extract and parse key data
5. Deduplicate keys across all secrets
6. Apply threshold limit if specified
7. Return final key list

**Error Scenarios**:
- Secret not found or access denied
- Invalid JSON format in secret data
- Empty or missing keys
- Threshold cannot be satisfied

**Location**: `internal/secrets/loader.go`

#### 4.2 Loader Tests (`loader_test.go`)

**Purpose**: Comprehensive unit tests for secrets loading logic

**Test Coverage**:
- JSON and text format parsing
- Key deduplication across multiple secrets
- Threshold enforcement
- Cross-namespace access
- Error handling for missing/invalid secrets
- Edge cases and malformed data

**Location**: `internal/secrets/loader_test.go`

---

### 5. Vault Client Module (`internal/vault/`)

#### 5.1 Vault Client (`client.go`)

**Purpose**: Interface with HashiCorp Vault API for unsealing operations

**Responsibilities**:
- Create secure connections to Vault servers
- Check seal status of Vault instances
- Execute unseal operations with provided keys
- Handle TLS configuration and certificate validation
- Provide structured error responses

**Key Components**:

```go
type Client struct {
    client *api.Client
}

// Connection with TLS support
func NewClient(vaultURL string, tlsConfig *TLSConfig) (*Client, error)

// Check if Vault instance is sealed
func (c *Client) GetSealStatus(ctx context.Context) (*SealStatus, error)

// Perform unseal operation with key
func (c *Client) Unseal(ctx context.Context, key string) (*UnsealResponse, error)
```

**TLS Configuration**:
- Support for custom CA certificates
- Option for insecure skip verify (development only)
- Proper certificate chain validation

**Status Structures**:
```go
type SealStatus struct {
    Sealed      bool   `json:"sealed"`
    T           int    `json:"t"`          // Threshold
    N           int    `json:"n"`          // Total shares
    Progress    int    `json:"progress"`   // Current unseal progress
    Nonce       string `json:"nonce"`      // Unseal nonce
    Version     string `json:"version"`    // Vault version
    BuildDate   string `json:"build_date"` // Build information
    Migration   bool   `json:"migration"`  // Migration status
    ClusterName string `json:"cluster_name"`
    ClusterID   string `json:"cluster_id"`
}
```

**Error Handling**:
- Network connectivity issues
- Authentication failures
- Invalid unseal keys
- Vault API errors

**Location**: `internal/vault/client.go`

---

### 6. Metrics Module (`internal/metrics/`)

#### 6.1 Prometheus Metrics (`metrics.go`)

**Purpose**: Provide comprehensive observability through Prometheus metrics

**Responsibilities**:
- Track reconciliation attempts and errors
- Monitor unseal operations per pod
- Record operational metrics (duration, counts, status)
- Support metric labeling for multi-dimensional analysis

**Defined Metrics**:

```go
// 1. Reconciliation tracking
ReconciliationTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "vault_unsealer_reconciliation_total",
        Help: "Total number of reconciliation attempts",
    },
    []string{"vaultunsealer", "namespace"},
)

// 2. Error tracking
ReconciliationErrors = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "vault_unsealer_reconciliation_errors_total",
        Help: "Total number of reconciliation errors",
    },
    []string{"vaultunsealer", "namespace", "error_type"},
)

// 3. Unseal operation tracking
UnsealAttempts = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "vault_unsealer_unseal_attempts_total",
        Help: "Total number of unseal attempts",
    },
    []string{"vaultunsealer", "namespace", "pod", "status"},
)

// 4. Current pod states
PodsUnsealed = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
        Name: "vault_unsealer_pods_unsealed",
        Help: "Number of currently unsealed pods",
    },
    []string{"vaultunsealer", "namespace"},
)

// 5. Pod processing tracking
PodsChecked = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
        Name: "vault_unsealer_pods_checked",
        Help: "Number of pods checked in last reconciliation",
    },
    []string{"vaultunsealer", "namespace"},
)

// 6. Key management
UnsealKeysLoaded = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
        Name: "vault_unsealer_unseal_keys_loaded",
        Help: "Number of unseal keys successfully loaded",
    },
    []string{"vaultunsealer", "namespace"},
)

// 7. Performance tracking
ReconciliationDuration = prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name: "vault_unsealer_reconciliation_duration_seconds",
        Help: "Duration of reconciliation operations",
        Buckets: prometheus.DefBuckets,
    },
    []string{"vaultunsealer", "namespace"},
)

// 8. Vault connectivity
VaultConnectionStatus = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
        Name: "vault_unsealer_vault_connection_status",
        Help: "Status of Vault API connections (1=success, 0=failure)",
    },
    []string{"vaultunsealer", "namespace", "vault_url"},
)
```

**Initialization**:
- Metrics registered with controller-runtime registry
- Available on `/metrics` endpoint
- Compatible with Prometheus scraping

**Usage Pattern**:
```go
metrics.ReconciliationTotal.WithLabelValues(
    vaultUnsealer.Name,
    vaultUnsealer.Namespace,
).Inc()
```

**Location**: `internal/metrics/metrics.go`

---

### 7. Logging Module (`internal/logging/`)

#### 7.1 Structured Logger (`logger.go`)

**Purpose**: Provide structured logging helpers for consistent log formatting

**Responsibilities**:
- Add contextual information to log entries
- Support resource-specific logging contexts
- Maintain correlation across operations
- Follow Kubernetes logging best practices

**Helper Functions**:

```go
// Add VaultUnsealer resource context
func WithVaultUnsealer(logger logr.Logger, vu *opsv1alpha1.VaultUnsealer) logr.Logger {
    return logger.WithValues(
        "vaultunsealer", vu.Name,
        "namespace", vu.Namespace,
        "generation", vu.Generation,
        "resourceVersion", vu.ResourceVersion,
    )
}

// Add Pod context
func WithPod(logger logr.Logger, pod *corev1.Pod) logr.Logger {
    return logger.WithValues(
        "pod", pod.Name,
        "namespace", pod.Namespace,
        "podIP", pod.Status.PodIP,
        "phase", pod.Status.Phase,
    )
}

// Add Secret reference context
func WithSecret(logger logr.Logger, secretRef opsv1alpha1.SecretRef, namespace string) logr.Logger

// Add Vault connection context
func WithVaultClient(logger logr.Logger, vaultURL string, podName string) logr.Logger

// Add reconciliation correlation ID
func WithReconcileID(logger logr.Logger, reconcileID string) logr.Logger

// Add operation timing context
func WithDuration(logger logr.Logger, operation string, duration time.Duration) logr.Logger
```

**Log Levels**:
- **Debug**: Detailed operational flow
- **Info**: Normal operational events
- **Warn**: Non-fatal issues that may require attention
- **Error**: Fatal errors requiring immediate action

**Usage Pattern**:
```go
logger := logging.WithVaultUnsealer(
    logging.WithReconcileID(ctrl.Log, reconcileID),
    vaultUnsealer,
)
logger.Info("Starting reconciliation")
```

**Location**: `internal/logging/logger.go`

---

### 8. Testing Modules (`test/`)

#### 8.1 E2E Test Framework (`test/e2e/`)

**Purpose**: End-to-end testing using real Kubernetes clusters

**Components**:

##### `basic_e2e_test.go`
- **Testcontainers Integration**: Uses k3s container for testing
- **Kubeconfig Management**: Handles Docker exec output parsing
- **CRD Installation**: Dynamic CRD installation in test clusters
- **Comprehensive Validation**: Tests all major functionality

**Test Flow**:
1. Start k3s container with testcontainers
2. Extract and clean kubeconfig from container
3. Set up Kubernetes clients
4. Install VaultUnsealer CRDs
5. Test basic Kubernetes operations
6. Test VaultUnsealer resource CRUD
7. Test secrets loading with multiple formats
8. Clean up test environment

##### `vault_unsealer_e2e_test.go`
- **Ginkgo/Gomega Framework**: BDD-style testing
- **Focused Test Scenarios**: Specific business logic validation
- **Mock Vault Integration**: Simulated Vault scenarios

##### `e2e_testcontainers_test.go`
- **Advanced Test Scenarios**: Complex integration patterns
- **Performance Testing**: Load and stress testing capabilities

**Location**: `test/e2e/`

#### 8.2 Test Utilities (`test/utils/`)

**Purpose**: Shared testing utilities and helpers

**Location**: `test/utils/utils.go`

---

### 9. Configuration Modules (`config/`)

#### 9.1 CRD Definitions (`config/crd/`)
- Generated Kubernetes CRD manifests
- OpenAPI schema validation
- Subresource definitions (status)

#### 9.2 RBAC Configuration (`config/rbac/`)
- Service accounts and role bindings
- Minimal privilege access controls
- Leader election permissions

#### 9.3 Deployment Manifests (`config/manager/`)
- Manager deployment configuration
- Resource limits and requests
- Security contexts

#### 9.4 Sample Resources (`config/samples/`)
- Example VaultUnsealer configurations
- Test secret configurations
- Documentation examples

---

### 10. Deployment Modules

#### 10.1 Production Deployment (`deploy/production/`)

**Purpose**: Production-ready Kubernetes manifests

**Components**:
- `namespace.yaml`: Dedicated namespace
- `rbac.yaml`: Complete RBAC configuration
- `deployment.yaml`: Secure operator deployment
- `service.yaml`: Metrics service exposure
- `servicemonitor.yaml`: Prometheus integration

**Security Features**:
- Non-root execution (user 65532)
- Read-only root filesystem
- Dropped capabilities
- Resource limits enforcement

#### 10.2 Helm Chart (`helm/vault-unsealer/`)

**Purpose**: Parameterized deployment packaging

**Structure**:
- `Chart.yaml`: Helm chart metadata
- `values.yaml`: Default configuration values
- `templates/`: Kubernetes manifest templates
- `README.md`: Installation and configuration guide

**Key Features**:
- Configurable resource limits
- Multiple deployment modes
- TLS certificate management
- Monitoring integration

---

## Module Interaction Patterns

### 1. Request Flow

```
User Creates VaultUnsealer → Controller Reconcile → Secrets Loader → Vault Client → Status Update
                                    ↓                    ↓              ↓             ↓
                            Pod Discovery    →  Key Loading  → Unseal Ops → Metrics Update
                                    ↓                    ↓              ↓             ↓
                            Label Selection  → Deduplication → TLS Verify →  Log Events
```

### 2. Error Propagation

```
Vault API Error → Vault Client → Controller → Status Condition → User Visibility
                                     ↓              ↓                ↓
Secret Missing → Secrets Loader → Controller → Metrics Update → Alert Manager
                                     ↓              ↓                ↓
Pod Not Found → Controller → Status Update → Log Warning → Admin Notification
```

### 3. Observability Flow

```
Operation → Structured Logs → Log Aggregation
    ↓           ↓
Metrics → Prometheus → Grafana Dashboard
    ↓           ↓
Events → Kubernetes → kubectl/UI
```

## Design Principles

### 1. **Separation of Concerns**
Each module has a single, well-defined responsibility:
- Controller handles Kubernetes reconciliation
- Secrets loader manages key retrieval and processing
- Vault client encapsulates Vault API operations
- Metrics module provides observability
- Logging module ensures consistent structured logging

### 2. **Dependency Injection**
- Dependencies injected through constructors
- Interfaces used for testability
- Clean module boundaries with minimal coupling

### 3. **Error Handling**
- Consistent error propagation patterns
- Structured error types with context
- Transient vs permanent error classification
- Comprehensive error logging and metrics

### 4. **Observability First**
- Metrics for every significant operation
- Structured logging with correlation IDs
- Status conditions for user visibility
- Event emission for audit trails

### 5. **Security by Design**
- Minimal RBAC permissions
- Secure defaults in all configurations
- TLS support for external communications
- No secrets in logs or metrics

### 6. **Production Readiness**
- Comprehensive testing coverage
- Resource limits and health checks
- Graceful shutdown and cleanup
- Multiple deployment options

## Conclusion

The Vault Auto-unseal Operator follows a modular architecture with clear separation of concerns, comprehensive error handling, and production-ready observability. Each module is designed to be testable, maintainable, and follows Kubernetes operator best practices.

The architecture enables:
- **Scalability**: Event-driven processing with efficient resource utilization
- **Reliability**: Comprehensive error handling and retry logic
- **Observability**: Full metrics and logging coverage
- **Security**: Minimal privileges and secure communications
- **Maintainability**: Clean module boundaries and comprehensive testing
