//go:build nozmq
// +build nozmq

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

import (
	"strings"
	"testing"
)

// TestKVEventManagerNoZMQValidateConfiguration tests that the stub implementation
// correctly returns an error indicating ZMQ support is required
func TestKVEventManagerNoZMQValidateConfiguration(t *testing.T) {
	// Create a new stub KV event manager
	manager := NewKVEventManager(nil)

	// Call validateConfiguration
	err := manager.validateConfiguration()

	// Verify it returns an error
	if err == nil {
		t.Fatal("Expected validateConfiguration to return an error for nozmq build")
	}

	// Verify the error message indicates ZMQ support is required
	expectedMsg := "ZMQ support"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error message to contain '%s', got: %v", expectedMsg, err)
	}
}
