# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a **Vault Auto-unseal Operator** - a Kubernetes operator that automatically unseals HashiCorp Vault pods. The operator is event-driven and monitors Vault pods to automatically provide unseal keys when needed.

**Key Architecture:**
- **Event-driven**: Watches Pods, VaultUnsealer CRs, and referenced Secrets
- **Multi-secret support**: Can load unseal keys from multiple Kubernetes Secrets  
- **Threshold-based**: Submits only the required number of keys per pod
- **HA-aware**: Handles both single-node and HA Vault deployments
- **Production-ready**: Includes metrics, structured logging, and distroless container support

## Essential Commands

### Development Commands
```bash
# Generate CRDs from Go types (always run after API changes)
make manifests

# Build the operator binary
make build

# Run unit tests (excludes E2E tests)
make test

# Run a single test package
go test ./internal/secrets/ -v

# Run specific test function
go test ./internal/controller/ -run TestVaultUnsealerController -v

# Lint code
make lint

# Format code
make fmt

# Generate deep copy methods (run after changing API types)
make generate
```

### E2E Testing Commands
```bash
# Run comprehensive E2E tests using testcontainers with k3s
go test ./test/e2e/ -run TestK3sE2EBasic -v

# Run CRD generation tests
go test ./test/e2e/ -run TestCRDGeneration -v

# Run all E2E tests
go test ./test/e2e/ -v
```

### Container & Deployment Commands
```bash
# Build standard Docker image
make docker-build IMG=vault-autounseal-operator:latest

# Build distroless image for production security
docker build -t vault-autounseal-operator:distroless -f Dockerfile .

# Build multi-architecture distroless image
./scripts/build-distroless.sh latest

# Install CRDs to cluster
make install

# Deploy operator to cluster
make deploy IMG=vault-autounseal-operator:latest

# Apply sample VaultUnsealer resource
kubectl apply -f config/samples/
```

## Code Architecture

### Module Structure and Responsibilities

**`api/v1alpha1/`** - Kubernetes API definitions
- `vaultunsealer_types.go`: Complete CRD specification with VaultUnsealerSpec and Status
- Defines SecretRef, VaultConnectionSpec, ModeSpec types
- Generated files handle serialization/deserialization

**`internal/controller/`** - Core operator logic
- `vaultunsealer_controller.go`: Main reconciliation loop with comprehensive error handling
- Watches Pods, VaultUnsealer CRs, and Secrets for event-driven operation
- Implements finalizer handling for graceful cleanup
- Updates status conditions and manages requeue logic

**`internal/vault/`** - Vault API client wrapper  
- `client.go`: HTTP client for Vault seal/unseal operations with TLS support
- Handles `/v1/sys/seal-status` and `/v1/sys/unseal` endpoints
- Includes retry logic and proper error handling

**`internal/secrets/`** - Multi-format secret loading
- `loader.go`: Loads and parses unseal keys from Kubernetes Secrets
- Supports JSON arrays and newline-separated text formats
- Implements key deduplication and threshold selection
- Handles cross-namespace secret access

**`internal/metrics/`** - Prometheus monitoring
- `metrics.go`: 8 comprehensive metrics for observability
- Tracks reconciliations, unseal attempts, errors, and performance
- Integrates with controller-runtime metrics framework

**`internal/logging/`** - Structured logging
- `logger.go`: Correlation ID support and structured log helpers
- Consistent logging patterns across all components

**`test/e2e/`** - End-to-end testing
- `basic_e2e_test.go`: Comprehensive testcontainers-based testing with k3s
- `crd_generator_test.go`: Programmatic CRD generation validation
- Tests CRD installation, resource operations, and secrets loading

### Key Integration Points

**Controller ” Vault Client**: Controller calls vault client for seal status checks and unseal operations

**Controller ” Secrets Loader**: Controller uses secrets loader to fetch and parse unseal keys from multiple sources

**Controller ” Metrics**: All operations update Prometheus metrics for observability

**Secrets Loader ” Kubernetes API**: Direct integration with Kubernetes client to fetch secrets across namespaces

## Important Implementation Details

### CRD Generation
- Uses operator-sdk/controller-gen for build-time CRD generation
- CRDs are generated from Go struct annotations in `api/v1alpha1/`
- Always run `make manifests` after changing API types

### Event-Driven Architecture
- Controller watches three resource types: Pods, VaultUnsealer CRs, and Secrets
- Uses controller-runtime's source.Kind for efficient event filtering
- Implements proper requeue logic for periodic safety checks

### Secret Handling
- Supports multiple secret formats: JSON arrays and newline-separated text
- Implements key deduplication across multiple secrets
- Respects keyThreshold for security (only submits required number of keys)
- Handles cross-namespace secret references

### Error Handling & Status Management
- Uses Kubernetes condition patterns for status reporting
- Implements exponential backoff for transient failures
- Proper finalizer handling prevents resource leaks
- Structured error conditions: KeysMissing, VaultAPIFailure, PodUnavailable

### Production Deployment
- Distroless container images for minimal security surface
- Multi-architecture builds (AMD64/ARM64) 
- Helm chart available in `helm/vault-unsealer/`
- RBAC manifests in `config/rbac/`
- Production deployment manifests in `deploy/production/`

### Testing Strategy
- Unit tests use envtest for Kubernetes API simulation
- E2E tests use testcontainers with real k3s cluster
- CRD validation tests ensure proper schema generation
- Comprehensive test coverage for all core components

## Development Workflow

1. **API Changes**: Modify `api/v1alpha1/vaultunsealer_types.go` ’ Run `make manifests generate`
2. **Controller Logic**: Update `internal/controller/vaultunsealer_controller.go` ’ Run tests
3. **Testing**: Add unit tests in respective `_test.go` files, update E2E tests for integration scenarios
4. **Documentation**: Update context files in `claude-code-context/` for significant changes

## Special Files

- `vault-auto-unsealer-spec.md`: Complete operator specification with ASCII flowcharts
- `scripts/build-distroless.sh`: Multi-architecture container build script
- `Dockerfile.distroless`: Production-ready distroless container definition
- `claude-code-context/`: Progressive documentation of implementation phases