# KV Event Sync Chaos Testing

This directory contains chaos testing experiments for the KV cache event synchronization system using Chaos Mesh.

## Prerequisites

1. Install Chaos Mesh in your Kubernetes cluster:
   ```bash
   curl -sSL https://mirrors.chaos-mesh.org/latest/install.sh | bash
   ```

2. Verify Chaos Mesh installation:
   ```bash
   kubectl get pods -n chaos-mesh
   ```

## Running Chaos Tests

### Run All Chaos Tests
```bash
go test -v ./test/chaos/
```

### Run Specific Chaos Test
```bash
go test -v ./test/chaos/ -run TestChaosNetworkPartition
```

### Skip Chaos Tests (if Chaos Mesh not installed)
```bash
go test -v ./test/chaos/ -short
```

## Chaos Experiments

### Network Failures (`experiments/network-partition.yaml`)
- **Network Partition**: Isolates vLLM pods from gateway
- **Packet Loss**: 50% packet loss with 25% correlation
- **Network Delay**: 500ms latency with 100ms jitter
- **Bandwidth Limit**: Restricts to 1Mbps

### Pod Failures (`experiments/pod-failures.yaml`)
- **Pod Kill**: Randomly kills one pod
- **Pod Failure**: Fails 50% of pods
- **CPU Stress**: 80% CPU load on 2 workers
- **Memory Stress**: 512MB memory pressure
- **IO Stress**: 100ms delay on 50% of IO operations

### ZMQ Failures (`experiments/zmq-failures.yaml`)
- **Port Block**: Blocks ZMQ ports 5557 and 5558
- **Connection Corruption**: 50% packet corruption
- **Time Skew**: 30-second time offset
- **API Delay**: 2-second delay on API calls

## Manual Chaos Experiment Application

### Apply an Experiment
```bash
kubectl apply -f test/chaos/experiments/network-partition.yaml
```

### Check Experiment Status
```bash
kubectl get networkchaos -n kv-sync-test
kubectl describe networkchaos kv-sync-network-partition -n kv-sync-test
```

### Delete an Experiment
```bash
kubectl delete -f test/chaos/experiments/network-partition.yaml
```

## Recovery Validation

All chaos tests include automatic recovery validation:
1. System recovers within 30 seconds after chaos ends
2. No data corruption occurs
3. Performance returns to baseline
4. Clear error messages in logs

## Safety Measures

- Chaos experiments run in isolated namespaces
- Automatic cleanup after each test
- Limited duration (30s-120s per experiment)
- Graceful degradation validation

## Troubleshooting

### Chaos Mesh Not Working
1. Check controller logs:
   ```bash
   kubectl logs -n chaos-mesh deployment/chaos-controller-manager
   ```

2. Verify webhook certificates:
   ```bash
   kubectl get mutatingwebhookconfigurations
   ```

### Experiments Not Applying
1. Check RBAC permissions:
   ```bash
   kubectl auth can-i create networkchaos --as=system:serviceaccount:chaos-mesh:chaos-controller-manager
   ```

2. Verify target pods exist:
   ```bash
   kubectl get pods -n kv-sync-test -l model.aibrix.ai/kv-events-enabled=true
   ```

### Cleanup Issues
Force delete stuck experiments:
```bash
kubectl delete chaosengine --all -n kv-sync-test
kubectl patch networkchaos <name> -n kv-sync-test -p '{"metadata":{"finalizers":[]}}' --type=merge
```

## Best Practices

1. **Start Small**: Begin with short-duration, low-impact experiments
2. **Monitor Closely**: Watch logs and metrics during chaos
3. **Document Results**: Record performance degradation and recovery times
4. **Incremental Testing**: Gradually increase chaos intensity
5. **Production Safety**: Never run chaos tests in production without approval

## Extending Chaos Tests

To add new chaos experiments:

1. Create YAML in `experiments/` directory
2. Add test function in `chaos_test.go`
3. Include recovery validation
4. Document expected behavior
5. Update this README

## CI Integration

Chaos tests run weekly in CI:
- Triggered by `.github/workflows/complete-testing.yml`
- Results uploaded as artifacts
- Failures notify team via Slack

## References

- [Chaos Mesh Documentation](https://chaos-mesh.org/docs/)
- [Chaos Engineering Principles](https://principlesofchaos.org/)
- [KV Event Sync Design](../../docs/kv-cache-events-guide.md)