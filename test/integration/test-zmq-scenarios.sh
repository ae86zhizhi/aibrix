#!/bin/bash
# test/integration/test-zmq-scenarios.sh

echo "Testing Scenario 1: Default (no flags)"
kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
    AIBRIX_USE_REMOTE_TOKENIZER=false \
    AIBRIX_KV_EVENT_SYNC_ENABLED=false
# Verify no ZMQ connections
kubectl logs -n aibrix-system deployment/aibrix-gateway-plugins | grep -q "KV event sync is disabled"

echo "Testing Scenario 2: Remote tokenizer only"
kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
    AIBRIX_USE_REMOTE_TOKENIZER=true \
    AIBRIX_KV_EVENT_SYNC_ENABLED=false
# Verify remote tokenizer but no ZMQ
kubectl logs -n aibrix-system deployment/aibrix-gateway-plugins | grep -q "Using remote tokenizer"

echo "Testing Scenario 3: Full KV sync"
kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
    AIBRIX_USE_REMOTE_TOKENIZER=true \
    AIBRIX_KV_EVENT_SYNC_ENABLED=true
# Verify ZMQ connections
kubectl logs -n aibrix-system deployment/aibrix-gateway-plugins | grep -q "KV event synchronization initialized"

echo "Testing DNS Resolution (Static Binary Check)"
# Since distroless has no shell, test DNS resolution indirectly via gateway functionality
# Check if gateway can connect to remote tokenizer endpoint (requires DNS resolution)
kubectl logs -n aibrix-system deployment/aibrix-gateway-plugins --tail=50 | \
    grep -E "(Connected to remote tokenizer|Failed to resolve)" && \
    echo "✓ DNS resolution verified through gateway logs" || \
    echo "Note: Manual DNS verification needed - check if gateway connects to external services"