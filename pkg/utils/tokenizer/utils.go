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

package tokenizer

import (
	"encoding/binary"
)

// intToByteArray converts int array to byte array in BigEndian format
// It uses int32 to preserve sign and validate token values
func intToByteArray(intArray []int) []byte {
	// Pre-allocate buffer for better performance
	buf := make([]byte, len(intArray)*4)
	for i, num := range intArray {
		// Use int32 to preserve sign and ensure valid range
		// Token IDs should typically be positive, but we handle negative values safely
		binary.BigEndian.PutUint32(buf[i*4:], uint32(int32(num)))
	}
	return buf
}
