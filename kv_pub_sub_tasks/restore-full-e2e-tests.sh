#!/bin/bash
# Script to restore E2E tests to their full scale after CI debugging

echo "Restoring E2E tests to full scale..."

# Restore kv_sync_e2e_simple_test.go
echo "Restoring kv_sync_e2e_simple_test.go..."
sed -i 's/1\*time\.Minute/2*time.Minute/g' test/e2e/kv_sync_e2e_simple_test.go
sed -i 's/helper\.CreateVLLMPodWithKVEvents(t, "vllm-multi", 2)/helper.CreateVLLMPodWithKVEvents(t, "vllm-multi", 3)/g' test/e2e/kv_sync_e2e_simple_test.go
sed -i 's/require\.Equal(t, 2, len(pods), "Expected exactly 2 pods")/require.Equal(t, 3, len(pods), "Expected exactly 3 pods")/g' test/e2e/kv_sync_e2e_simple_test.go
sed -i 's/helper\.ScaleDeployment(t, deployment\.Name, 3)/helper.ScaleDeployment(t, deployment.Name, 5)/g' test/e2e/kv_sync_e2e_simple_test.go
sed -i 's/require\.Equal(t, 3, len(pods), "Expected 3 pods after scale up")/require.Equal(t, 5, len(pods), "Expected 5 pods after scale up")/g' test/e2e/kv_sync_e2e_simple_test.go
sed -i 's/numDeployments := 2  \/\/ Reduced for faster testing/numDeployments := 3/g' test/e2e/kv_sync_e2e_simple_test.go
sed -i 's/helper\.CreateVLLMPodWithKVEvents(t, name, 1)  \/\/ Reduced pods per deployment/helper.CreateVLLMPodWithKVEvents(t, name, 2)/g' test/e2e/kv_sync_e2e_simple_test.go
sed -i 's/assert\.Equal(t, numDeployments\*1, totalConnections/assert.Equal(t, numDeployments*2, totalConnections/g' test/e2e/kv_sync_e2e_simple_test.go

# Remove skip from large scale test
sed -i '/t\.Skip("Temporarily skipping large scale tests for faster debugging")/,/return/d' test/e2e/kv_sync_e2e_simple_test.go

# Restore kv_sync_e2e_test.go
echo "Restoring kv_sync_e2e_test.go..."
sed -i 's/1\*time\.Minute/2*time.Minute/g' test/e2e/kv_sync_e2e_test.go
sed -i 's/numPods := 2  \/\/ Reduced for faster testing/numPods := 3/g' test/e2e/kv_sync_e2e_test.go
sed -i 's/helper\.CreateVLLMPodWithKVEvents(t, "vllm-multi-pod", int32(numPods))/helper.CreateVLLMPodWithKVEvents(t, "vllm-multi-pod", int32(numPods))/g' test/e2e/kv_sync_e2e_test.go
sed -i 's/helper\.WaitForDeploymentReady(t, deployment\.Name, 1\*time\.Minute)/helper.WaitForDeploymentReady(t, deployment.Name, 3*time.Minute)/g' test/e2e/kv_sync_e2e_test.go
sed -i 's/helper\.ScaleDeployment(t, deployment\.Name, 2)  \/\/ Reduced for faster testing/helper.ScaleDeployment(t, deployment.Name, 3)/g' test/e2e/kv_sync_e2e_test.go
sed -i 's/require\.Equal(t, 2, len(pods), "Expected 2 pods after scale up")/require.Equal(t, 3, len(pods), "Expected 3 pods after scale up")/g' test/e2e/kv_sync_e2e_test.go

# Remove CI skip conditions from KV functionality tests
for func in "TestKVSyncE2EHappyPath" "TestKVSyncE2EMultiPod" "TestKVSyncE2EPodLifecycle" "TestKVSyncE2EMultiModel"; do
  sed -i "/${func}/,/defer cancel()/{/if os\.Getenv(\"CI\") == \"true\"/,/}/d}" test/e2e/kv_sync_e2e_test.go
done

# Restore network connectivity validation
echo "Restoring network connectivity validation..."
sed -i '/if os\.Getenv("CI") == "true" {/,/return/d' test/e2e/kv_sync_helpers.go

echo "Restoration complete! Remember to:"
echo "1. Review the changes with 'git diff'"
echo "2. Run tests locally to verify they work"
echo "3. Consider keeping some optimizations that don't affect coverage"