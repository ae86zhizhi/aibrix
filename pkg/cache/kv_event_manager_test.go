//go:build !zmq

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

package cache

import "encoding/binary"

// tokenIDsToBytes converts int32 token IDs to byte array (for tests)
func tokenIDsToBytes(tokenIDs []int32) []byte {
	bytes := make([]byte, len(tokenIDs)*4)
	for i, id := range tokenIDs {
		binary.BigEndian.PutUint32(bytes[i*4:], uint32(id))
	}
	return bytes
}
