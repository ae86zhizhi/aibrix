# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

AIBrix is an open-source cloud-native solution for deploying, managing, and scaling large language model (LLM) inference infrastructure. It's built on Kubernetes and uses the operator pattern with custom resources.

## Common Development Commands

### Building and Testing

```bash
# Run all tests
make test

# Run integration tests  
make test-integration

# Run e2e tests
make test-e2e

# Lint code
make lint

# Fix linting issues
make lint-fix

# Build all Docker images
make docker-build-all

# Build specific component
make docker-build-controller-manager
make docker-build-gateway-plugins
make docker-build-runtime
make docker-build-metadata-service
```

### Local Development with Kind

```bash
# Install AIBrix in Kind cluster (builds images and loads them)
make dev-install-in-kind

# Uninstall from Kind
make dev-uninstall-from-kind

# Set up port forwarding for local access
make dev-port-forward

# Stop port forwarding
make dev-stop-port-forward
```

### Code Generation

```bash
# Generate CRDs, DeepCopy methods, and RBAC manifests
make generate

# Update client code
make update-codegen

# Verify generated code is up to date
make verify-codegen
```

## Architecture Overview

### Core Components

1. **Controller Manager** (`cmd/controllers/main.go`)
   - Manages all Kubernetes controllers
   - Handles custom resources: PodAutoscaler, ModelAdapter, KVCache, StormService, RayCluster

2. **Gateway Plugins** (`pkg/plugins/gateway/`)
   - Envoy external processor for LLM-aware routing
   - Implements various routing algorithms (round-robin, least-latency, PD-routing)

3. **Runtime Service** (`python/aibrix/`)
   - FastAPI application for model management
   - Handles model downloading, metrics collection, batch processing

4. **Metadata Service** (`cmd/metadata/`)
   - Redis-backed service for model metadata management

### Custom Resources

- `autoscaling.aibrix.ai/v1alpha1`:
  - `PodAutoscaler` - LLM-specific autoscaling with HPA/KPA/APA algorithms
  
- `model.aibrix.ai/v1alpha1`:
  - `ModelAdapter` - Manages LoRA adapter loading/unloading

- `orchestration.aibrix.ai/v1alpha1`:
  - `KVCache` - Distributed KV cache management
  - `StormService` - Advanced service deployment with rollout strategies
  - `RayClusterFleet`, `RayClusterReplicaSet` - Distributed inference clusters

### Project Structure

```
/api              - Kubernetes API definitions
/cmd              - Entry points for services
/pkg              - Core Go packages
  /controller     - All Kubernetes controllers
  /plugins        - Gateway plugins
  /metrics        - Metrics processing
/python           - Python runtime and KV cache
/config           - Kubernetes deployment configs
  /crd            - Custom Resource Definitions
  /overlays       - Environment-specific configs
/build/container  - Dockerfiles
```

### Configuration Patterns

- Uses Kustomize for deployment configuration
- Environment-specific overlays in `/config/overlays/`
- Feature flags controlled via controller manager args
- Runtime configuration through ConfigMaps and environment variables

### Testing Approach

- Unit tests: Standard Go testing with mocks
- Integration tests: Uses envtest for controller testing
- E2E tests: Full cluster testing with Kind
- Python tests: Located in `/python/tests/`

### Key Dependencies

- Kubernetes controller-runtime for operator framework
- Envoy Gateway for API gateway functionality
- Redis for metadata storage
- Ray for distributed inference
- vLLM for LLM inference engine

### Development Workflow

1. Controllers reconcile custom resources in `/pkg/controller/`
2. Gateway plugins process requests in `/pkg/plugins/gateway/`
3. Runtime manages model lifecycle in `/python/aibrix/`
4. All components expose Prometheus metrics
5. Uses structured logging throughout