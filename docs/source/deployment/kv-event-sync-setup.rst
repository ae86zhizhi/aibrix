===================================
Setting up KV Event Synchronization
===================================

Prerequisites
-------------

Before enabling KV event sync, ensure:

1. **Remote Tokenizer Service** is deployed::

    kubectl apply -f samples/tokenizer-service.yaml

2. **vLLM version** supports KV events (>= 0.7.0)

3. **Network policies** allow ZMQ traffic on ports 5557-5558

Deployment Steps
----------------

Step 1: Deploy vLLM with KV Events
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Using Helm::

    helm install my-model ./helm/aibrix-model \
      --set model.kvEvents.enabled=true \
      --set model.kvEvents.publisherType=zmq \
      --set model.kvEvents.eventPort=5557 \
      --set model.kvEvents.replayPort=5558

Or using raw manifests::

    kubectl apply -f samples/vllm-with-kv-events.yaml

Step 2: Configure AIBrix Controllers
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Update the gateway deployment::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_KV_EVENT_SYNC_ENABLED=true \
      AIBRIX_USE_REMOTE_TOKENIZER=true \
      AIBRIX_REMOTE_TOKENIZER_ENDPOINT=http://tokenizer:8080

Step 3: Verify Setup
~~~~~~~~~~~~~~~~~~~~

Check vLLM is publishing events::

    kubectl logs deployment/vllm-model | grep "KV cache events enabled"

Check gateway is receiving events::

    kubectl logs -n aibrix-system deployment/aibrix-gateway-plugins | grep "Subscribed to KV events"

Troubleshooting
---------------

No Events Received
~~~~~~~~~~~~~~~~~~

1. Check network connectivity::

    kubectl exec -it <gateway-pod> -- nc -zv <vllm-pod-ip> 5557

2. Verify ZMQ is enabled in build (only for gateway-plugins)::

    kubectl exec <gateway-pod> -- ldd /gateway-plugin | grep zmq

3. Check tokenizer is accessible::

    kubectl exec <gateway-pod> -- curl http://tokenizer:8080/health

Connection Timeouts
~~~~~~~~~~~~~~~~~~~

If you see connection timeout errors:

1. Verify the vLLM pod has the correct labels::

    kubectl get pods -l model.aibrix.ai/kv-events-enabled=true

2. Check that network policies allow traffic::

    kubectl get networkpolicies -A

3. Ensure the ZMQ ports are exposed in the service::

    kubectl get svc <vllm-service> -o yaml | grep -A5 ports

Memory Issues
~~~~~~~~~~~~~

If the gateway is consuming too much memory:

1. Reduce the event buffer size::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_KV_EVENT_BUFFER_SIZE=1000

2. Enable event compression (if available in your vLLM version)

3. Monitor memory usage::

    kubectl top pod -n aibrix-system

Performance Tuning
~~~~~~~~~~~~~~~~~~

For optimal performance:

1. Adjust ZMQ polling timeout::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_ZMQ_POLL_TIMEOUT=50ms

2. Configure event batch size::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_KV_EVENT_BATCH_SIZE=100

3. Monitor event processing rate::

    kubectl logs -n aibrix-system deployment/aibrix-gateway-plugins | grep "Events processed"

Advanced Configuration
----------------------

Multi-Model Setup
~~~~~~~~~~~~~~~~~

When deploying multiple models with KV event sync:

1. Use unique topics per model::

    helm install model1 ./helm/aibrix-model \
      --set model.kvEvents.topic=model1 \
      --set model.kvEvents.enabled=true

    helm install model2 ./helm/aibrix-model \
      --set model.kvEvents.topic=model2 \
      --set model.kvEvents.enabled=true

2. Configure gateway to handle multiple topics::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_KV_EVENT_TOPICS=model1,model2

High Availability
~~~~~~~~~~~~~~~~~

For HA deployments:

1. Deploy multiple gateway replicas::

    kubectl scale deployment/aibrix-gateway-plugins -n aibrix-system --replicas=3

2. Use leader election for event processing::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_KV_EVENT_LEADER_ELECTION=true

3. Configure event replay for failover::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_KV_EVENT_REPLAY_ON_STARTUP=true

Security Considerations
~~~~~~~~~~~~~~~~~~~~~~~

1. **Network Isolation**: Use network policies to restrict ZMQ traffic
2. **TLS Support**: Enable TLS for ZMQ connections (if supported)
3. **Authentication**: Configure ZMQ authentication tokens
4. **Monitoring**: Set up alerts for suspicious event patterns

Monitoring and Observability
----------------------------

Metrics
~~~~~~~

The following Prometheus metrics are available:

- ``aibrix_kv_events_received_total``: Total events received
- ``aibrix_kv_events_processed_total``: Total events processed
- ``aibrix_kv_events_errors_total``: Total processing errors
- ``aibrix_kv_events_latency_seconds``: Event processing latency

Logging
~~~~~~~

Enable debug logging for troubleshooting::

    kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
      AIBRIX_LOG_LEVEL=debug \
      AIBRIX_KV_EVENT_DEBUG=true

Tracing
~~~~~~~

If OpenTelemetry is configured, KV event processing spans are automatically included in traces.

Next Steps
----------

- :doc:`/features/kv-event-sync` - Learn more about KV event synchronization
- :doc:`/api/kv-event-sync` - API reference for KV events
- :doc:`/migration/enable-kv-events` - Migrate existing deployments
- :doc:`/testing/kv-event-sync-e2e` - Run E2E tests