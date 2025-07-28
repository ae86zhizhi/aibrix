# Timestamp Precision Test Fix

## Issue Analysis

The test `TestParseTimestamp/float64_with_fractional_seconds` is failing in CI due to floating-point precision loss.

### Root Cause
- Test expects: `2025-07-28 10:09:19.123`
- Actual result: `2025-07-28 10:09:19.122999`
- Difference: 1 microsecond due to float64 precision

### Impact on CI/CD
- **YES**, this will cause CI/CD to fail
- The test is run by `make test-zmq-coverage` in `complete-testing.yml`
- Failed tests will block PR merges

## Fix Options

### Option 1: Adjust Test to Use Delta Comparison (Recommended)
```go
// Instead of:
assert.Equal(t, tt.want, got)

// Use:
assert.WithinDuration(t, tt.want, got, time.Microsecond)
```

### Option 2: Fix the Test Case
```go
{
    name:  "float64 with fractional seconds",
    input: float64(now.Unix()) + 0.123,
    want:  time.Unix(now.Unix(), 123000000).UTC().Truncate(time.Microsecond),
}
```

### Option 3: Add Tolerance to parseTimestamp
Round to nearest microsecond instead of truncating.

## Recommended Fix

Modify the test to use `assert.InDelta` or `assert.WithinDuration` to allow for floating-point precision issues. This is the most robust solution as it acknowledges the inherent limitations of float64 precision while still validating the core functionality.

## Implementation

In `pkg/cache/kvcache/msgpack_decoder_test.go`, line 302:

```go
// Change from:
assert.Equal(t, tt.want, got)

// To:
if tt.name == "float64 with fractional seconds" {
    assert.WithinDuration(t, tt.want, got, time.Microsecond, 
        "timestamp should be within 1 microsecond")
} else {
    assert.Equal(t, tt.want, got)
}
```

This fix acknowledges that float64 timestamp conversion may have microsecond-level precision variations while still ensuring the functionality works correctly.