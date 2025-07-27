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
```bash
kubectl apply -f samples/quickstart/model-with-kv-events.yaml
```

### 2. Existing Deployments
Run the migration script:
```bash
./migration/enable-kv-events.sh default my-model
```

### 3. Configure Gateway
Ensure gateway has remote tokenizer enabled:
```bash
kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
  AIBRIX_USE_REMOTE_TOKENIZER=true \
  AIBRIX_KV_EVENT_SYNC_ENABLED=true \
  AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE=remote \
  AIBRIX_REMOTE_TOKENIZER_ENGINE=vllm \
  AIBRIX_REMOTE_TOKENIZER_ENDPOINT=http://vllm-service.default:8000
```

### 4. Verify Configuration
Check that events are being published:
```bash
kubectl logs deployment/my-model | grep "KV cache events"
```

Check gateway configuration:
```bash
kubectl logs -n aibrix-system deployment/aibrix-gateway-plugins | grep "KV event"
```

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