/*
Copyright 2025 The Aibrix Team.

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
	"encoding/json"
	"fmt"
)

const (
	vllmTokenizePath   = "/tokenize"
	vllmDetokenizePath = "/detokenize"
)

// VLLMAdapter implements EngineAdapter for vLLM inference engine
type VLLMAdapter struct {
	model string
}

// NewVLLMAdapter creates a new vLLM adapter
func NewVLLMAdapter(model string) *VLLMAdapter {
	return &VLLMAdapter{
		model: model,
	}
}

// GetTokenizePath returns the tokenize endpoint path for vLLM
func (va *VLLMAdapter) GetTokenizePath() string {
	return vllmTokenizePath
}

// GetDetokenizePath returns the detokenize endpoint path for vLLM
func (va *VLLMAdapter) GetDetokenizePath() string {
	return vllmDetokenizePath
}

// SupportsTokenization returns true as vLLM supports tokenization
func (va *VLLMAdapter) SupportsTokenization() bool {
	return true
}

// SupportsDetokenization returns true as vLLM supports detokenization
func (va *VLLMAdapter) SupportsDetokenization() bool {
	return true
}

// SupportsChat returns true as vLLM supports chat tokenization
func (va *VLLMAdapter) SupportsChat() bool {
	return true
}

// PrepareTokenizeRequest prepares a tokenize request for vLLM
func (va *VLLMAdapter) PrepareTokenizeRequest(input TokenizeInput) (interface{}, error) {
	switch input.Type {
	case CompletionInput:
		req := &VLLMTokenizeCompletionRequest{
			Prompt:           input.Text,
			AddSpecialTokens: &input.AddSpecialTokens,
			ReturnTokenStrs:  &input.ReturnTokenStrings,
		}
		if va.model != "" {
			req.Model = va.model
		}
		return req, nil

	case ChatInput:
		req := &VLLMTokenizeChatRequest{
			Messages:            input.Messages,
			AddSpecialTokens:    &input.AddSpecialTokens,
			AddGenerationPrompt: &input.AddGenerationPrompt,
			ReturnTokenStrs:     &input.ReturnTokenStrings,
		}
		if va.model != "" {
			req.Model = va.model
		}
		return req, nil

	default:
		return nil, fmt.Errorf("unsupported input type: %s", input.Type)
	}
}

// PrepareDetokenizeRequest prepares a detokenize request for vLLM
func (va *VLLMAdapter) PrepareDetokenizeRequest(tokens []int) (interface{}, error) {
	req := &VLLMDetokenizeRequest{
		Tokens: tokens,
	}
	if va.model != "" {
		req.Model = va.model
	}
	return req, nil
}

// ParseTokenizeResponse parses a vLLM tokenize response
func (va *VLLMAdapter) ParseTokenizeResponse(data []byte) (*TokenizeResult, error) {
	var resp VLLMTokenizeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse tokenize response: %w", err)
	}

	return &TokenizeResult{
		Count:        resp.Count,
		MaxModelLen:  resp.MaxModelLen,
		Tokens:       resp.Tokens,
		TokenStrings: resp.TokenStrs,
	}, nil
}

// ParseDetokenizeResponse parses a vLLM detokenize response
func (va *VLLMAdapter) ParseDetokenizeResponse(data []byte) (string, error) {
	var resp VLLMDetokenizeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("failed to parse detokenize response: %w", err)
	}
	return resp.Prompt, nil
}
