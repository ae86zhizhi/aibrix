# Task 004: vLLM Configuration and Deployment Updates

## Overview
Configure vLLM deployments to enable KV cache event publishing and update deployment templates to support the new functionality.

## Background
vLLM has built-in support for KV cache event publishing via ZMQ, but it needs to be explicitly enabled. We need to update all vLLM deployment configurations and provide guidance for users.

## Requirements

### Functional Requirements
1. Enable KV event publishing in vLLM configurations
2. Expose required ports (5557, 5558) in pod specs
3. Add required labels for pod discovery
4. Update service definitions if needed
5. Provide migration guide for existing deployments

### Non-Functional Requirements
1. Backward compatibility with existing deployments
2. Minimal performance impact when enabled
3. Clear documentation and examples
4. Support for various deployment methods

## Technical Specification

### vLLM Configuration

#### Command Line Arguments
```bash
# Required flags
--enable-kv-cache-events
--kv-events-publisher zmq
--kv-events-endpoint tcp://*:5557
--kv-events-replay-endpoint tcp://*:5558

# Optional flags
--kv-events-buffer-steps 10000      # Number of events to keep for replay
--kv-events-topic <model-name>      # Topic prefix for events
```

#### Environment Variables (Alternative)
```yaml
env:
  - name: VLLM_ENABLE_KV_CACHE_EVENTS
    value: "true"
  - name: VLLM_KV_EVENTS_PUBLISHER
    value: "zmq"
  - name: VLLM_KV_EVENTS_ENDPOINT
    value: "tcp://*:5557"
  - name: VLLM_KV_EVENTS_REPLAY_ENDPOINT
    value: "tcp://*:5558"
```

### Deployment Templates

#### Basic vLLM Deployment with KV Events

```yaml
# samples/quickstart/model-with-kv-events.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    model.aibrix.ai/name: deepseek-r1-distill-llama-8b
    model.aibrix.ai/port: "8000"
    model.aibrix.ai/kv-events-enabled: "true"  # NEW: Enable KV events
  name: deepseek-r1-distill-llama-8b
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      model.aibrix.ai/name: deepseek-r1-distill-llama-8b
  template:
    metadata:
      labels:
        model.aibrix.ai/name: deepseek-r1-distill-llama-8b
        model.aibrix.ai/kv-events-enabled: "true"  # NEW: Required for discovery
    spec:
      containers:
        - name: vllm-openai
          image: vllm/vllm-openai:v0.7.1
          command:
            - python3
            - -m
            - vllm.entrypoints.openai.api_server
            - --host
            - "0.0.0.0"
            - --port
            - "8000"
            - --uvicorn-log-level
            - warning
            - --model
            - deepseek-ai/DeepSeek-R1-Distill-Llama-8B
            - --served-model-name
            - deepseek-r1-distill-llama-8b
            - --max-model-len
            - "12288"
            # NEW: KV event publishing configuration
            - --enable-kv-cache-events
            - --kv-events-publisher
            - zmq
            - --kv-events-endpoint
            - "tcp://*:5557"
            - --kv-events-replay-endpoint
            - "tcp://*:5558"
            - --kv-events-buffer-steps
            - "10000"
          ports:
            - containerPort: 8000
              protocol: TCP
              name: api
            # NEW: KV event ports
            - containerPort: 5557
              protocol: TCP
              name: kv-events
            - containerPort: 5558
              protocol: TCP
              name: kv-replay
          resources:
            limits:
              nvidia.com/gpu: "1"
            requests:
              nvidia.com/gpu: "1"
          # Health checks remain the same
          livenessProbe:
            httpGet:
              path: /health
              port: 8000
              scheme: HTTP
            failureThreshold: 3
            periodSeconds: 5
            successThreshold: 1
            timeoutSeconds: 1
          readinessProbe:
            httpGet:
              path: /health
              port: 8000
              scheme: HTTP
            failureThreshold: 5
            periodSeconds: 5
            successThreshold: 1
            timeoutSeconds: 1
          startupProbe:
            httpGet:
              path: /health
              port: 8000
              scheme: HTTP
            failureThreshold: 30
            periodSeconds: 5
            successThreshold: 1
            timeoutSeconds: 1

---

apiVersion: v1
kind: Service
metadata:
  labels:
    model.aibrix.ai/name: deepseek-r1-distill-llama-8b
    prometheus-discovery: "true"
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8080"
  name: deepseek-r1-distill-llama-8b
  namespace: default
spec:
  ports:
    - name: serve
      port: 8000
      protocol: TCP
      targetPort: 8000
    - name: metrics
      port: 8080
      protocol: TCP
      targetPort: 8080
    # NEW: Expose KV event ports (optional, for debugging)
    - name: kv-events
      port: 5557
      protocol: TCP
      targetPort: 5557
    - name: kv-replay
      port: 5558
      protocol: TCP
      targetPort: 5558
  selector:
    model.aibrix.ai/name: deepseek-r1-distill-llama-8b
  type: ClusterIP
```

#### Using Environment Variables (Alternative)

```yaml
# samples/quickstart/model-with-kv-events-env.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    model.aibrix.ai/name: llama-8b-instruct
    model.aibrix.ai/kv-events-enabled: "true"
  name: llama-8b-instruct
  namespace: default
spec:
  replicas: 2
  selector:
    matchLabels:
      model.aibrix.ai/name: llama-8b-instruct
  template:
    metadata:
      labels:
        model.aibrix.ai/name: llama-8b-instruct
        model.aibrix.ai/kv-events-enabled: "true"
    spec:
      containers:
        - name: vllm-openai
          image: vllm/vllm-openai:v0.7.1
          command:
            - python3
            - -m
            - vllm.entrypoints.openai.api_server
            - --host
            - "0.0.0.0"
            - --port
            - "8000"
            - --model
            - meta-llama/Llama-3.1-8B-Instruct
            - --served-model-name
            - llama-8b-instruct
          env:
            # NEW: KV event configuration via environment
            - name: VLLM_ENABLE_KV_CACHE_EVENTS
              value: "true"
            - name: VLLM_KV_EVENTS_PUBLISHER
              value: "zmq"
            - name: VLLM_KV_EVENTS_ENDPOINT
              value: "tcp://*:5557"
            - name: VLLM_KV_EVENTS_REPLAY_ENDPOINT
              value: "tcp://*:5558"
            - name: VLLM_KV_EVENTS_BUFFER_STEPS
              value: "10000"
            # Performance tuning
            - name: VLLM_KV_EVENTS_HWM
              value: "100000"  # ZMQ high water mark
          ports:
            - containerPort: 8000
              protocol: TCP
              name: api
            - containerPort: 5557
              protocol: TCP
              name: kv-events
            - containerPort: 5558
              protocol: TCP
              name: kv-replay
          resources:
            limits:
              nvidia.com/gpu: "1"
            requests:
              nvidia.com/gpu: "1"
```

#### With PodAutoscaler

```yaml
# samples/autoscaling/kpa-with-kv-events.yaml
apiVersion: autoscaling.aibrix.ai/v1alpha1
kind: PodAutoscaler
metadata:
  name: llama-8b-kpa
  namespace: default
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: llama-8b-instruct
  metric:
    name: pending_requests_v2
  algorithm:
    type: KPA
    kpa:
      targetValue: 5
      scaleUpRate: 100
      scaleDownRate: 10
  minReplicas: 1
  maxReplicas: 10
  # Template modifications for scaled pods
  template:
    metadata:
      labels:
        model.aibrix.ai/kv-events-enabled: "true"
```

### Helm Chart Updates

```yaml
# helm/aibrix-model/values.yaml
model:
  name: "my-model"
  image: "vllm/vllm-openai:v0.7.1"
  
  # NEW: KV event configuration
  kvEvents:
    enabled: true
    publisherType: "zmq"
    eventPort: 5557
    replayPort: 5558
    bufferSteps: 10000
    
  # Existing configuration...
```

```yaml
# helm/aibrix-model/templates/deployment.yaml
{{- if .Values.model.kvEvents.enabled }}
- --enable-kv-cache-events
- --kv-events-publisher
- {{ .Values.model.kvEvents.publisherType }}
- --kv-events-endpoint
- "tcp://*:{{ .Values.model.kvEvents.eventPort }}"
- --kv-events-replay-endpoint
- "tcp://*:{{ .Values.model.kvEvents.replayPort }}"
{{- end }}
```

### Migration Guide

#### For Existing Deployments

```bash
#!/bin/bash
# migration/enable-kv-events.sh

# Script to enable KV events for existing deployments

NAMESPACE=${1:-default}
DEPLOYMENT=$2

if [ -z "$DEPLOYMENT" ]; then
    echo "Usage: $0 [namespace] <deployment-name>"
    exit 1
fi

echo "Enabling KV events for deployment $DEPLOYMENT in namespace $NAMESPACE"

# Add label to deployment
kubectl label deployment -n $NAMESPACE $DEPLOYMENT \
    model.aibrix.ai/kv-events-enabled=true --overwrite

# Patch deployment to add KV event configuration
kubectl patch deployment -n $NAMESPACE $DEPLOYMENT --type='json' -p='[
  {
    "op": "add",
    "path": "/spec/template/metadata/labels/model.aibrix.ai~1kv-events-enabled",
    "value": "true"
  },
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/env/-",
    "value": {
      "name": "VLLM_ENABLE_KV_CACHE_EVENTS",
      "value": "true"
    }
  },
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/env/-",
    "value": {
      "name": "VLLM_KV_EVENTS_PUBLISHER",
      "value": "zmq"
    }
  },
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/env/-",
    "value": {
      "name": "VLLM_KV_EVENTS_ENDPOINT",
      "value": "tcp://*:5557"
    }
  },
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/env/-",
    "value": {
      "name": "VLLM_KV_EVENTS_REPLAY_ENDPOINT",
      "value": "tcp://*:5558"
    }
  },
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/ports/-",
    "value": {
      "containerPort": 5557,
      "protocol": "TCP",
      "name": "kv-events"
    }
  },
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/ports/-",
    "value": {
      "containerPort": 5558,
      "protocol": "TCP",
      "name": "kv-replay"
    }
  }
]'

echo "Deployment updated. Pods will be recreated with KV events enabled."
```

### Network Policies

```yaml
# samples/network-policies/allow-kv-events.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-kv-events
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: gateway-plugins
  policyTypes:
  - Ingress
  ingress:
  - from:
    - podSelector:
        matchLabels:
          model.aibrix.ai/kv-events-enabled: "true"
    ports:
    - protocol: TCP
      port: 5557
    - protocol: TCP
      port: 5558
```

### Verification Steps

```bash
# 1. Check if KV events are enabled
kubectl logs -n default deployment/my-model | grep "KV cache events enabled"

# 2. Test ZMQ connection
kubectl exec -n aibrix-system deployment/aibrix-gateway-plugins -- \
    nc -zv <pod-ip> 5557

# 3. Check event publishing
kubectl exec -n default deployment/my-model -- \
    python -c "import zmq; ctx = zmq.Context(); \
    sub = ctx.socket(zmq.SUB); \
    sub.connect('tcp://localhost:5557'); \
    sub.subscribe(b''); \
    print('Listening for events...'); \
    msg = sub.recv_multipart(); \
    print(f'Received: {len(msg)} parts')"
```

## Documentation

### User Guide

```markdown
# Enabling KV Cache Event Synchronization

## Overview
KV cache event synchronization allows AIBrix gateway to make routing decisions based on real-time cache state from vLLM engines.

## Requirements
- vLLM version 0.7.0 or later
- AIBrix version 0.4.0 or later
- ZMQ library support in vLLM image
- Remote tokenizer enabled in gateway (prerequisite)

## Quick Start

### 1. New Deployments
Use the provided templates with KV events pre-configured:
\`\`\`bash
kubectl apply -f samples/quickstart/model-with-kv-events.yaml
\`\`\`

### 2. Existing Deployments
Run the migration script:
\`\`\`bash
./migration/enable-kv-events.sh default my-model
\`\`\`

### 3. Configure Gateway
Ensure gateway has remote tokenizer enabled:
\`\`\`bash
kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
  AIBRIX_USE_REMOTE_TOKENIZER=true \
  AIBRIX_KV_EVENT_SYNC_ENABLED=true \
  AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE=remote \
  AIBRIX_REMOTE_TOKENIZER_ENGINE=vllm \
  AIBRIX_REMOTE_TOKENIZER_ENDPOINT=http://vllm-service.default:8000
\`\`\`

### 4. Verify Configuration
Check that events are being published:
\`\`\`bash
kubectl logs deployment/my-model | grep "KV cache events"
\`\`\`

Check gateway configuration:
\`\`\`bash
kubectl logs -n aibrix-system deployment/aibrix-gateway-plugins | grep "KV event"
\`\`\`

## Configuration Options

### vLLM Options
| Option | Default | Description |
|--------|---------|-------------|
| --enable-kv-cache-events | false | Enable KV event publishing |
| --kv-events-buffer-steps | 10000 | Number of events to buffer for replay |
| --kv-events-topic | "" | Topic prefix for events |

### Gateway Requirements
| Environment Variable | Required | Description |
|---------------------|----------|-------------|
| AIBRIX_USE_REMOTE_TOKENIZER | Yes | Must be "true" for KV sync |
| AIBRIX_KV_EVENT_SYNC_ENABLED | Yes | Enable KV event consumption |
| AIBRIX_REMOTE_TOKENIZER_ENDPOINT | Yes | vLLM service endpoint |

**Important**: KV event sync requires remote tokenizer to ensure consistent tokenization between gateway and vLLM.

## Troubleshooting

### Events Not Publishing
1. Check vLLM logs for errors
2. Verify ZMQ ports are accessible
3. Ensure labels are correctly set

### High Memory Usage
- Reduce buffer steps
- Enable event compression (if supported)

### Performance Impact
- KV events add <1% CPU overhead
- Network bandwidth: ~1MB/s per pod at high load
```

## Testing Plan

### Deployment Testing
1. Test new deployment with KV events
2. Test migration of existing deployment
3. Test scaling behavior
4. Test with different vLLM versions

### Integration Testing
1. Test event flow end-to-end
2. Test with multiple models
3. Test pod restart scenarios
4. Test network failures

## Implementation Steps

1. **Create Templates** (Day 1)
   - Basic deployment template
   - Autoscaling templates
   - Migration scripts

2. **Documentation** (Day 2)
   - User guide
   - Configuration reference
   - Troubleshooting guide

3. **Testing** (Day 3)
   - Template validation
   - Migration testing
   - Performance impact analysis

## Success Criteria

1. Templates work with latest vLLM
2. Migration script handles all cases
3. Clear documentation
4. No breaking changes
5. Performance impact <1%

## Dependencies

- vLLM support for KV events (already available)
- Tasks 001-003 for gateway-side implementation

## Risks and Mitigations

1. **Version Compatibility**
   - Risk: Older vLLM versions don't support KV events
   - Mitigation: Document minimum version requirements

2. **Network Security**
   - Risk: ZMQ ports exposed unnecessarily
   - Mitigation: Provide network policy examples

3. **Resource Usage**
   - Risk: Event publishing increases resource usage
   - Mitigation: Provide tuning guidelines