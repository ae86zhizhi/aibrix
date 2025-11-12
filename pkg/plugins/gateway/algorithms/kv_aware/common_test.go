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

package kvaware

import (
	v1 "k8s.io/api/core/v1"
)

// SimplePodList implements types.PodList for testing
// This is shared across unit tests, E2E tests, and benchmarks
type SimplePodList struct {
	pods []*v1.Pod
}

func (p *SimplePodList) All() []*v1.Pod {
	return p.pods
}

func (p *SimplePodList) Len() int {
	return len(p.pods)
}

func (p *SimplePodList) Indexes() []string {
	return []string{"default"}
}

func (p *SimplePodList) ListByIndex(index string) []*v1.Pod {
	return p.pods
}
