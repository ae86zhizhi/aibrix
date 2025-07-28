# Prompt for Fixing CI Test Failure

## Task
Fix the failing CI test `TestMockZMQPublisher` that passes locally but fails in GitHub Actions.

## Context
The test expects to receive 1 event but receives 2 events in CI environment. This is likely a timing or race condition issue.

## Instructions

1. **First, read the analysis document:**
   ```bash
   cat kv_pub_sub_tasks/ci-test-failure-analysis.md
   ```

2. **Examine the failing test at line 257:**
   ```bash
   sed -n '250,270p' pkg/cache/kvcache/zmq_client_test.go
   ```

3. **Run the test locally to reproduce:**
   ```bash
   # Run multiple times to catch intermittent failures
   for i in {1..20}; do 
     go test -v -tags="zmq" -run TestMockZMQPublisher ./pkg/cache/kvcache/ || break
   done
   ```

4. **Analyze the test logic:**
   - Why might it receive 2 events instead of 1?
   - Is there a race condition?
   - Are events being duplicated?

5. **Fix the issue by either:**
   - Adding proper synchronization
   - Filtering duplicate events
   - Adjusting test expectations for CI environment
   - Adding delays or wait conditions

6. **Verify the fix:**
   - Test should pass consistently locally
   - Consider CI environment differences (slower, different timing)

## Expected Outcome
- The test should pass both locally and in CI
- The fix should be minimal and not affect other tests
- Add comments explaining why the fix was needed

## Time Estimate
15-30 minutes

## Priority
High - This blocks the PR from being merged