=====================================
Migrating to KV Event Synchronization
=====================================

Overview
--------

This guide helps you migrate existing vLLM deployments to use KV event synchronization. The migration process is designed to be safe and reversible, with minimal downtime.

Pre-Migration Checklist
-----------------------

Before starting the migration:

- [ ] Backup current configurations
- [ ] Plan maintenance window (10-15 minutes per model)
- [ ] Verify remote tokenizer is deployed
- [ ] Test in staging environment first
- [ ] Ensure vLLM version >= 0.7.0
- [ ] Check ZMQ library availability in vLLM image
- [ ] Review current autoscaling settings

Migration Steps
---------------

Step 1: Prepare Remote Tokenizer
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Deploy the remote tokenizer service if not already present::

    kubectl apply -f samples/tokenizer-service.yaml
    
    # Verify it's running
    kubectl wait --for=condition=ready pod -l app=tokenizer-service --timeout=300s
    
    # Test the endpoint
    kubectl run test-tokenizer --rm -it --image=curlimages/curl -- \
      curl http://tokenizer-service:8080/health

Step 2: Update Gateway Configuration
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Enable remote tokenizer in the gateway::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_USE_REMOTE_TOKENIZER=true \
      AIBRIX_REMOTE_TOKENIZER_ENDPOINT=http://tokenizer-service.default:8080 \
      AIBRIX_REMOTE_TOKENIZER_ENGINE=vllm \
      AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE=remote

Wait for gateway to restart::

    kubectl rollout status deployment/aibrix-gateway-plugins -n aibrix-system

Step 3: Update vLLM Deployment
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

For each vLLM deployment, add KV event configuration.

**Option A: Using kubectl edit**::

    kubectl edit deployment <model-deployment>

Add to container args::

    - --enable-kv-cache-events
    - --kv-events-publisher=zmq
    - --kv-events-endpoint=tcp://*:5557
    - --kv-events-replay-endpoint=tcp://*:5558

Add to container ports::

    - name: kv-events
      containerPort: 5557
      protocol: TCP
    - name: kv-replay
      containerPort: 5558
      protocol: TCP

**Option B: Using kubectl patch**::

    kubectl patch deployment <model-deployment> --type='json' -p='[
      {
        "op": "add",
        "path": "/spec/template/spec/containers/0/args/-",
        "value": "--enable-kv-cache-events"
      },
      {
        "op": "add",
        "path": "/spec/template/spec/containers/0/args/-",
        "value": "--kv-events-publisher=zmq"
      },
      {
        "op": "add",
        "path": "/spec/template/spec/containers/0/ports/-",
        "value": {"name": "kv-events", "containerPort": 5557, "protocol": "TCP"}
      }
    ]'

Step 4: Update Service Definition
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Add KV event ports to the service::

    kubectl patch service <model-service> --type='json' -p='[
      {
        "op": "add",
        "path": "/spec/ports/-",
        "value": {
          "name": "kv-events",
          "port": 5557,
          "targetPort": 5557,
          "protocol": "TCP"
        }
      },
      {
        "op": "add",
        "path": "/spec/ports/-",
        "value": {
          "name": "kv-replay",
          "port": 5558,
          "targetPort": 5558,
          "protocol": "TCP"
        }
      }
    ]'

Step 5: Add Required Labels
~~~~~~~~~~~~~~~~~~~~~~~~~~~

Add labels to enable KV event discovery::

    kubectl label deployment <model-deployment> \
      model.aibrix.ai/kv-events-enabled=true \
      model.aibrix.ai/name=<model-name>

Step 6: Enable KV Event Sync in Gateway
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Enable KV event synchronization::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_KV_EVENT_SYNC_ENABLED=true

Step 7: Verify Migration
~~~~~~~~~~~~~~~~~~~~~~~~

Check vLLM logs for KV events::

    kubectl logs deployment/<model-deployment> | grep -E "KV cache events|ZMQ"

Check gateway is receiving events::

    kubectl logs -n aibrix-system deployment/aibrix-gateway-plugins | grep "KV event"

Test prefix caching::

    # Send two identical requests
    curl -X POST http://gateway-endpoint/v1/completions \
      -H "Content-Type: application/json" \
      -d '{"prompt": "Once upon a time", "max_tokens": 100}'
    
    # Second request should show cache hit in logs

Automated Migration Script
--------------------------

For convenience, use the provided migration script::

    ./hack/migrate-to-kv-events.sh <namespace> <deployment-name>

The script performs all migration steps automatically and includes:

- Pre-flight checks
- Configuration backup
- Step-by-step migration
- Verification tests
- Rollback capability

Script Options
~~~~~~~~~~~~~~

.. code-block:: bash

   # Basic usage
   ./hack/migrate-to-kv-events.sh default my-model
   
   # Dry run mode
   ./hack/migrate-to-kv-events.sh --dry-run default my-model
   
   # Custom tokenizer endpoint
   ./hack/migrate-to-kv-events.sh --tokenizer http://custom-tokenizer:8080 default my-model
   
   # Skip verification
   ./hack/migrate-to-kv-events.sh --skip-verify default my-model

Rollback Procedure
------------------

If issues occur during or after migration:

Immediate Rollback
~~~~~~~~~~~~~~~~~~

1. Disable KV event sync in gateway::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_KV_EVENT_SYNC_ENABLED=false

2. This immediately stops using KV events while keeping vLLM configuration unchanged.

Complete Rollback
~~~~~~~~~~~~~~~~~

1. Remove KV event arguments from vLLM::

    kubectl edit deployment <model-deployment>
    # Remove the --enable-kv-cache-events and related arguments

2. Remove labels::

    kubectl label deployment <model-deployment> \
      model.aibrix.ai/kv-events-enabled-

3. Disable remote tokenizer (if not needed elsewhere)::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_USE_REMOTE_TOKENIZER=false

Migration Strategies
--------------------

Rolling Migration
~~~~~~~~~~~~~~~~~

For production environments with multiple models:

1. Migrate one model at a time
2. Monitor for 24 hours before proceeding
3. Keep detailed logs of each migration
4. Have rollback plan ready

Blue-Green Migration
~~~~~~~~~~~~~~~~~~~~

For critical services:

1. Deploy new vLLM instances with KV events enabled
2. Gradually shift traffic using gateway weights
3. Monitor performance and error rates
4. Cut over completely once stable
5. Keep old instances for quick rollback

Canary Migration
~~~~~~~~~~~~~~~~

For large deployments:

1. Enable KV events on 10% of pods
2. Monitor metrics and compare performance
3. Gradually increase percentage
4. Full deployment after validation

Common Issues and Solutions
---------------------------

ZMQ Connection Errors
~~~~~~~~~~~~~~~~~~~~~

**Symptom**: "Failed to bind ZMQ socket" errors

**Solution**:

1. Check port availability::

    kubectl exec <pod> -- netstat -tulpn | grep 5557

2. Ensure no port conflicts in pod spec

3. Verify ZMQ library is installed::

    kubectl exec <pod> -- ldd /usr/local/bin/vllm | grep zmq

Remote Tokenizer Failures
~~~~~~~~~~~~~~~~~~~~~~~~~

**Symptom**: "Failed to connect to remote tokenizer"

**Solution**:

1. Verify tokenizer service is running::

    kubectl get pods -l app=tokenizer-service

2. Check network connectivity::

    kubectl exec <gateway-pod> -- nc -zv tokenizer-service 8080

3. Verify tokenizer configuration matches vLLM

Performance Degradation
~~~~~~~~~~~~~~~~~~~~~~~

**Symptom**: Increased latency after migration

**Solution**:

1. Check event processing lag::

    kubectl logs -n aibrix-system deployment/aibrix-gateway-plugins | grep lag

2. Tune ZMQ parameters::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_ZMQ_POLL_TIMEOUT=50ms

3. Adjust worker count for event processing

Memory Usage Increase
~~~~~~~~~~~~~~~~~~~~~

**Symptom**: Gateway OOM errors

**Solution**:

1. Increase gateway memory limits::

    kubectl patch deployment aibrix-gateway-plugins -n aibrix-system --type='json' -p='[
      {
        "op": "replace",
        "path": "/spec/template/spec/containers/0/resources/limits/memory",
        "value": "4Gi"
      }
    ]'

2. Reduce event buffer size::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_KV_EVENT_BUFFER_SIZE=5000

Post-Migration Optimization
---------------------------

Performance Tuning
~~~~~~~~~~~~~~~~~~

After successful migration:

1. **Monitor Cache Hit Rates**::

    # Check Prometheus metrics
    aibrix_kv_cache_hit_rate

2. **Adjust Prefix Length**::

    kubectl set env deployment/<model-deployment> \
      --prefix-cache-min-length=10

3. **Optimize Event Batching**::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_KV_EVENT_BATCH_SIZE=200

Monitoring Setup
~~~~~~~~~~~~~~~~

1. **Add Grafana Dashboard**: Import the KV event sync dashboard from ``monitoring/dashboards/kv-event-sync.json``

2. **Set Up Alerts**:

   - High event processing lag (> 1s)
   - Low cache hit rate (< 50%)
   - ZMQ connection failures
   - Memory usage above 80%

3. **Enable Debug Logging** (temporarily)::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_LOG_LEVEL=debug \
      AIBRIX_KV_EVENT_DEBUG=true

Best Practices
--------------

1. **Test Thoroughly**: Always test in staging before production
2. **Monitor Closely**: Watch metrics for 24-48 hours post-migration
3. **Document Changes**: Keep detailed migration logs
4. **Gradual Rollout**: Use canary or blue-green strategies
5. **Backup Configs**: Save original configurations before changes
6. **Coordinate Teams**: Inform all stakeholders of migration windows

Next Steps
----------

After successful migration:

- :doc:`/features/kv-event-sync` - Understand the feature in detail
- :doc:`/deployment/kv-event-sync-setup` - Advanced configuration options
- :doc:`/api/kv-event-sync` - API reference for custom integrations
- :doc:`/testing/kv-event-sync-e2e` - Run validation tests