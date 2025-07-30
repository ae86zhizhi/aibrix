======================================================
End-to-End Testing Guide for KV Event Synchronization
======================================================

This guide provides comprehensive instructions for running and debugging E2E tests for the KV cache event synchronization system.

Overview
--------

The E2E tests validate the complete KV event synchronization flow in a real Kubernetes environment, including:

- vLLM pods with ZMQ event publishing
- Event flow from vLLM → ZMQ → Sync Indexer → Router
- Multi-pod and multi-model scenarios
- Pod lifecycle events
- Large-scale deployments

Prerequisites
-------------

Local Development
~~~~~~~~~~~~~~~~~

1. **Go 1.23+** with ZMQ support
2. **Docker** for building images
3. **Kind** or **K3s** for local Kubernetes cluster
4. **kubectl** configured to access the cluster
5. **ZMQ libraries**: ``libzmq3-dev`` (Ubuntu/Debian) or ``zeromq`` (macOS)

Installation
~~~~~~~~~~~~

.. code-block:: bash

   # Install ZMQ (Ubuntu/Debian)
   sudo apt-get update
   sudo apt-get install -y libzmq3-dev pkg-config

   # Install ZMQ (macOS)
   brew install zeromq pkg-config

   # Install Kind
   curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.26.0/kind-linux-amd64
   chmod +x ./kind
   sudo mv ./kind /usr/local/bin/kind

Test Setup
----------

Create Kind Cluster
~~~~~~~~~~~~~~~~~~~

.. code-block:: bash

   kind create cluster --config development/vllm/kind-config.yaml --name aibrix-e2e

Build and Load Images
~~~~~~~~~~~~~~~~~~~~~

.. code-block:: bash

   # Build all AIBrix images
   make docker-build-all

   # Load images into Kind
   kind load docker-image aibrix/controller-manager:nightly --name aibrix-e2e
   kind load docker-image aibrix/gateway-plugins:nightly --name aibrix-e2e
   kind load docker-image aibrix/vllm-mock:nightly --name aibrix-e2e

Deploy AIBrix Components
~~~~~~~~~~~~~~~~~~~~~~~~

.. code-block:: bash

   # Install CRDs and dependencies
   kubectl apply -k config/dependency --server-side
   kubectl apply -k config/test

   # Wait for controllers to be ready
   kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=controller-manager \
     -n aibrix-system --timeout=300s

Running E2E Tests
-----------------

Run All E2E Tests
~~~~~~~~~~~~~~~~~

.. code-block:: bash

   go test -v ./test/e2e/kv_sync_e2e_test.go -timeout 30m

Run Specific Test
~~~~~~~~~~~~~~~~~

.. code-block:: bash

   # Single pod test
   go test -v ./test/e2e/kv_sync_e2e_test.go -run TestKVSyncE2EHappyPath

   # Multi-pod test
   go test -v ./test/e2e/kv_sync_e2e_test.go -run TestKVSyncE2EMultiPod

   # Pod lifecycle test
   go test -v ./test/e2e/kv_sync_e2e_test.go -run TestKVSyncE2EPodLifecycle

   # Multi-model test
   go test -v ./test/e2e/kv_sync_e2e_test.go -run TestKVSyncE2EMultiModel

   # Large scale test (resource intensive)
   go test -v ./test/e2e/kv_sync_e2e_test.go -run TestKVSyncE2ELargeScale

Skip Large Scale Tests
~~~~~~~~~~~~~~~~~~~~~~

.. code-block:: bash

   go test -v ./test/e2e/kv_sync_e2e_test.go -short

Test Scenarios
--------------

Happy Path E2E
~~~~~~~~~~~~~~

``TestKVSyncE2EHappyPath`` validates the basic flow:

- Deploys a single vLLM pod with KV events enabled
- Validates ZMQ connection establishment
- Sends test events and verifies processing
- Checks sync indexer contains the events

Multi-Pod Scenarios
~~~~~~~~~~~~~~~~~~~

``TestKVSyncE2EMultiPod`` tests multiple pods:

- Deploys 3 vLLM pods
- Sends events from each pod
- Verifies all events are processed correctly
- Validates event isolation per pod

Pod Lifecycle
~~~~~~~~~~~~~

``TestKVSyncE2EPodLifecycle`` tests pod lifecycle events:

- Tests pod creation with immediate event publishing
- Scales deployment up (1→3 pods)
- Deletes a pod and verifies recovery
- Scales down (3→1 pods)
- Ensures events persist through lifecycle changes

Multi-Model
~~~~~~~~~~~

``TestKVSyncE2EMultiModel`` tests multiple models:

- Deploys multiple models in separate namespaces
- Validates model-specific event isolation
- Tests cross-model routing decisions
- Verifies no event leakage between models

Large Scale
~~~~~~~~~~~

``TestKVSyncE2ELargeScale`` tests at scale:

- Tests with 10, 50, and 100 pods
- Measures event processing throughput
- Validates system behavior under load
- Checks performance metrics

Debugging
---------

Check Pod Status
~~~~~~~~~~~~~~~~

.. code-block:: bash

   # List all test pods
   kubectl get pods -n kv-sync-test

   # Check pod logs
   kubectl logs -n kv-sync-test <pod-name>

   # Check ZMQ port connectivity
   kubectl exec -n kv-sync-test <pod-name> -- nc -zv localhost 5557

Verify Event Manager
~~~~~~~~~~~~~~~~~~~~

.. code-block:: bash

   # Check controller logs
   kubectl logs -n aibrix-system -l app.kubernetes.io/name=controller-manager

   # Check event manager status
   kubectl get pods -n aibrix-system -o wide

Debug ZMQ Connections
~~~~~~~~~~~~~~~~~~~~~

.. code-block:: bash

   # Port forward to test ZMQ directly
   kubectl port-forward -n kv-sync-test <pod-name> 5557:5557

   # Test with zmq utilities
   zmq_sub tcp://localhost:5557

Common Issues
~~~~~~~~~~~~~

**ZMQ Connection Failures**

- Ensure pods have the correct labels: ``model.aibrix.ai/kv-events-enabled=true``
- Check firewall rules and network policies
- Verify ZMQ ports (5557, 5558) are exposed

**Event Processing Delays**

- Check sync indexer memory usage
- Verify no CPU throttling on pods
- Look for backpressure in event queues

**Test Timeouts**

- Increase test timeout: ``-timeout 60m``
- Check for resource constraints in Kind
- Ensure sufficient Docker resources

Writing New E2E Tests
---------------------

Test Structure
~~~~~~~~~~~~~~

.. code-block:: go

   func TestKVSyncE2ENewScenario(t *testing.T) {
       // 1. Setup
       ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
       defer cancel()
       
       k8sClient, _ := initializeClient(ctx, t)
       helper := NewKVEventTestHelper(k8sClient, "test-namespace")
       
       // 2. Create namespace and cleanup
       helper.CreateTestNamespace(t)
       defer helper.CleanupTestNamespace(t)
       defer helper.CleanupDeployments(t)
       
       // 3. Deploy vLLM pods
       deployment := helper.CreateVLLMPodWithKVEvents(t, "test-deployment", 1)
       helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)
       
       // 4. Test logic
       // ... your test scenarios ...
       
       // 5. Assertions
       assert.True(t, condition, "Test condition should be met")
   }

Best Practices
~~~~~~~~~~~~~~

1. **Isolation**: Use unique namespaces for each test
2. **Cleanup**: Always defer cleanup functions
3. **Timeouts**: Set appropriate timeouts for operations
4. **Logging**: Use ``t.Logf()`` for debugging information
5. **Assertions**: Use clear assertion messages

CI/CD Integration
-----------------

GitHub Actions Workflow
~~~~~~~~~~~~~~~~~~~~~~~

The E2E tests run automatically in CI:

- On every PR to main branch
- On pushes to main and release branches
- Nightly scheduled runs

CI Environment
~~~~~~~~~~~~~~

- Uses Kind cluster in GitHub Actions
- Builds fresh images for each run
- Runs tests in parallel when possible
- Collects logs on failure

Running E2E Tests in CI
~~~~~~~~~~~~~~~~~~~~~~~

.. code-block:: yaml

   - name: Run E2E tests
     run: |
       export KUBECONFIG="${HOME}/.kube/config"
       go test -v ./test/e2e/kv_sync_e2e_test.go -timeout 30m

Performance Considerations
--------------------------

Resource Requirements
~~~~~~~~~~~~~~~~~~~~~

- **Minimum**: 4 CPU cores, 8GB RAM
- **Recommended**: 8 CPU cores, 16GB RAM
- **Large Scale Tests**: 16+ CPU cores, 32GB RAM

Optimization Tips
~~~~~~~~~~~~~~~~~

1. Run tests in parallel: ``go test -parallel 4``
2. Use resource limits on test pods
3. Clean up resources between test runs
4. Monitor cluster resource usage

Troubleshooting Guide
---------------------

Test Failures
~~~~~~~~~~~~~

Collect all logs:

.. code-block:: bash

   kubectl logs --all-containers=true -n kv-sync-test > test-logs.txt

Check events:

.. code-block:: bash

   kubectl get events -n kv-sync-test --sort-by='.lastTimestamp'

Verify cluster state:

.. code-block:: bash

   kubectl get all --all-namespaces

Debugging Flaky Tests
~~~~~~~~~~~~~~~~~~~~~

1. Run with race detection: ``go test -race``
2. Increase verbosity: ``go test -v -vv``
3. Add debug logging to helper functions
4. Check for timing-dependent assertions

Clean Up Stuck Resources
~~~~~~~~~~~~~~~~~~~~~~~~

.. code-block:: bash

   # Delete test namespaces
   kubectl delete namespace kv-sync-test --force --grace-period=0

   # Clean up Kind cluster
   kind delete cluster --name aibrix-e2e

Related Documentation
---------------------

- :doc:`/features/kv-event-sync`
- :doc:`/deployment/kv-event-sync-setup`
- :doc:`/api/kv-event-sync`
- :doc:`/migration/enable-kv-events`