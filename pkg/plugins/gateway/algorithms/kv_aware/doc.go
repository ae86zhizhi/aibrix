/*
Copyright 2024 The Aibrix Team.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package kvaware implements KVCache-aware routing algorithm based on
// Mooncake paper's Algorithm 1. It optimizes Time To First Token (TTFT)
// by considering KV cache reuse opportunities while maintaining Time
// Between Tokens (TBT) SLO constraints.
//
// The algorithm decomposes TTFT into three components:
//   - T_transfer: Time to transfer KV cache (MVP: estimated only)
//   - T_queue: Time waiting in queue
//   - T_prefill: Time to compute prefill for uncached tokens
//
// MVP constraints:
//   - No actual KV transfer (estimation only)
//   - Constant bandwidth model
//   - Uses existing metrics infrastructure
//   - P/D separation at pod level
package kvaware
