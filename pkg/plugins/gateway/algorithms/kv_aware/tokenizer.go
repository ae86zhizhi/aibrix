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
	"fmt"

	"github.com/vllm-project/aibrix/pkg/utils"
	"k8s.io/klog/v2"
)

// Tokenizer defines the interface for tokenizing text input
type Tokenizer interface {
	// Tokenize converts text to token IDs
	Tokenize(prompt string, modelName string) ([]int, error)
}

// defaultTokenizer implements Tokenizer using utils.TokenizeInputText
type defaultTokenizer struct{}

// NewTokenizer creates a new Tokenizer instance
func NewTokenizer() Tokenizer {
	return &defaultTokenizer{}
}

// Tokenize converts the input prompt to token IDs using the AIBrix tokenizer
func (t *defaultTokenizer) Tokenize(prompt string, modelName string) ([]int, error) {
	if prompt == "" {
		return nil, fmt.Errorf("empty prompt")
	}
	if modelName == "" {
		return nil, fmt.Errorf("empty model name")
	}

	klog.V(5).Infof("Tokenizing prompt for model %s (length: %d chars)", modelName, len(prompt))

	// Call the existing AIBrix tokenizer utility
	// Note: utils.TokenizeInputText only takes the prompt text
	tokens, err := utils.TokenizeInputText(prompt)
	if err != nil {
		klog.Errorf("Failed to tokenize prompt for model %s: %v", modelName, err)
		return nil, fmt.Errorf("tokenization failed: %w", err)
	}

	klog.V(5).Infof("Tokenization successful: %d tokens", len(tokens))
	return tokens, nil
}
