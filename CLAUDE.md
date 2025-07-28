# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AIBrix is a cloud-native Kubernetes operator for managing Large Language Model (LLM) inference infrastructure. It provides essential building blocks for constructing scalable GenAI inference systems with enterprise-grade features.

## Development Commands

### Core Development

```bash
# Generate CRDs and RBAC manifests
make manifests

# Generate DeepCopy implementations
make generate

# Format and lint Go code
make fmt
make vet
make lint
make lint-fix

# Run tests
make test                  # Unit tests with coverage
make test-race-condition   # Tests with race detection
make test-integration      # Integration tests
make test-e2e             # End-to-end tests

# Build controller binary
make build
```

### Docker Images

```bash
# Build all images
make docker-build-all

# Build specific images
make docker-build-controller-manager
make docker-build-gateway-plugins
make docker-build-runtime
make docker-build-metadata-service
make docker-build-kvcache-watcher

# Push images
make docker-push-all
```

### Kubernetes Deployment

```bash
# Install CRDs and dependencies
make install

# Deploy controllers
make deploy

# Uninstall
make uninstall
make undeploy
```

### Local Development with Kind

```bash
# Full installation in Kind cluster
make dev-install-in-kind

# Clean up from Kind
make dev-uninstall-from-kind

# Set up port forwarding for local access
make dev-port-forward
make dev-stop-port-forward
```

### Python Development

For Python components in `python/aibrix/`:

```bash
# Install dependencies with Poetry
cd python/aibrix
poetry install

# Run Python linters
poetry run mypy .
poetry run ruff check .
poetry run ruff format .

# Run Python tests
poetry run pytest
```

## Architecture

### Custom Resources (CRDs)

The project defines multiple API groups under `aibrix.ai`:

1. **autoscaling.aibrix.ai/v1alpha1**
   - `PodAutoscaler`: Custom autoscaling for LLM workloads

2. **model.aibrix.ai/v1alpha1**
   - `ModelAdapter`: Manages LoRA adapters

3. **orchestration.aibrix.ai/v1alpha1**
   - `KVCache`: Distributed KV cache management
   - `RayClusterFleet/ReplicaSet`: Multi-node inference
   - `StormService/RoleSet`: Service orchestration

### Key Components

1. **Controllers** (`cmd/controllers/`): Kubernetes operators managing custom resources
2. **Gateway Plugins** (`pkg/plugins/gateway/`): LLM-aware routing algorithms including:
   - Least busy/latency routing
   - Prefix cache aware routing
   - SLO-based routing
   - Prefill-decode disaggregation support
3. **AI Runtime** (`python/aibrix/`): Python runtime for model management and GPU optimization
4. **Metadata Service**: Centralized metadata management

### Routing Algorithms

The gateway implements sophisticated routing strategies:
- Virtual Token Counter (VTC) for load estimation
- Support for heterogeneous GPU clusters
- Prefix caching optimization
- SLO-aware request routing

## Testing Strategy

- **Unit Tests**: Standard Go testing with comprehensive mocks
- **Integration Tests**: Using Ginkgo/Gomega with envtest
- **E2E Tests**: Full cluster testing with Kind
- **Python Tests**: Using pytest for runtime components

Always verify tests pass before submitting changes:
```bash
make test
make lint
cd python/aibrix && poetry run pytest
```

## Key Design Patterns

1. **Controller Pattern**: Uses controller-runtime for reconciliation loops
2. **Feature Gates**: Controllers can be enabled/disabled individually
3. **Leader Election**: Supports HA deployments
4. **Observability**: Prometheus metrics and structured logging throughout
5. **Error Handling**: Wrapped errors with context for debugging

## LLM-Specific Features

- KV cache management with offloading capabilities
- Dynamic LoRA adapter loading
- Prefix caching for performance optimization
- GPU heterogeneity support
- Hardware failure detection
- Tokenizer abstraction layer supporting multiple backends

## Working with the Codebase

1. Follow existing code patterns - check neighboring files for conventions
2. Use the established error wrapping pattern for better debugging
3. Add Prometheus metrics for new features
4. Write table-driven tests for comprehensive coverage
5. Update CRD documentation when modifying APIs
6. Ensure backward compatibility for API changes