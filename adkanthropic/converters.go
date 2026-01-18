// Copyright 2025 Alcova AI
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adkanthropic

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/genai"

	"google.golang.org/adk/model"
)

// contentsToMessages converts genai Contents to Anthropic MessageParams.
// It handles role mapping and content part conversion.
func contentsToMessages(contents []*genai.Content) ([]anthropic.MessageParam, error) {
	if len(contents) == 0 {
		return nil, nil
	}

	var messages []anthropic.MessageParam
	for _, content := range contents {
		if content == nil {
			continue
		}

		msg, err := contentToMessage(content)
		if err != nil {
			return nil, fmt.Errorf("failed to convert content: %w", err)
		}
		if msg != nil {
			messages = append(messages, *msg)
		}
	}

	// Merge consecutive messages with the same role (Anthropic requires alternating roles)
	messages = mergeConsecutiveMessages(messages)

	return messages, nil
}

// contentToMessage converts a single genai.Content to an Anthropic MessageParam.
func contentToMessage(content *genai.Content) (*anthropic.MessageParam, error) {
	if content == nil || len(content.Parts) == 0 {
		return nil, nil
	}

	// Check if this content contains tool results (FunctionResponse).
	// Anthropic requires tool results to be in user messages.
	hasFunctionResponse := false
	hasFunctionCall := false
	for _, part := range content.Parts {
		if part != nil {
			if part.FunctionResponse != nil {
				hasFunctionResponse = true
			}
			if part.FunctionCall != nil {
				hasFunctionCall = true
			}
		}
	}

	// Determine the role - tool results must be user, tool calls must be assistant
	var role anthropic.MessageParamRole
	if hasFunctionResponse {
		// Tool results MUST be in user messages per Anthropic API requirements
		role = anthropic.MessageParamRoleUser
	} else if hasFunctionCall {
		// Tool calls (from model) MUST be in assistant messages
		role = anthropic.MessageParamRoleAssistant
	} else {
		var err error
		role, err = mapRole(content.Role)
		if err != nil {
			return nil, err
		}
	}

	var blocks []anthropic.ContentBlockParamUnion
	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		block, err := partToContentBlock(part)
		if err != nil {
			return nil, fmt.Errorf("failed to convert part: %w", err)
		}
		if block != nil {
			blocks = append(blocks, *block)
		}
	}

	if len(blocks) == 0 {
		return nil, nil
	}

	msg := anthropic.MessageParam{
		Role:    role,
		Content: blocks,
	}
	return &msg, nil
}

// mapRole maps genai role to Anthropic MessageParamRole.
func mapRole(role string) (anthropic.MessageParamRole, error) {
	switch strings.ToLower(role) {
	case "user":
		return anthropic.MessageParamRoleUser, nil
	case "model", "assistant":
		return anthropic.MessageParamRoleAssistant, nil
	default:
		return "", fmt.Errorf("unsupported role: %s", role)
	}
}

// partToContentBlock converts a genai Part to an Anthropic ContentBlockParamUnion.
func partToContentBlock(part *genai.Part) (*anthropic.ContentBlockParamUnion, error) {
	if part == nil {
		return nil, nil
	}

	// Text content
	if part.Text != "" {
		// Check if this is a thought block
		if part.Thought {
			// Thoughts from model responses need to be passed back with signature
			if len(part.ThoughtSignature) > 0 {
				block := anthropic.ContentBlockParamUnion{
					OfThinking: &anthropic.ThinkingBlockParam{
						Thinking:  part.Text,
						Signature: base64.StdEncoding.EncodeToString(part.ThoughtSignature),
					},
				}
				return &block, nil
			}
			// If no signature, treat as regular text (shouldn't happen in valid flow)
		}
		block := anthropic.NewTextBlock(part.Text)
		return &block, nil
	}

	// Inline binary data (images, PDFs)
	if part.InlineData != nil {
		return inlineDataToBlock(part.InlineData)
	}

	// File data (URI-based)
	if part.FileData != nil {
		return fileDataToBlock(part.FileData)
	}

	// Function response (tool result)
	if part.FunctionResponse != nil {
		return functionResponseToBlock(part.FunctionResponse)
	}

	// Function call - these should only appear in model responses, not requests
	// We return nil for these as they shouldn't be in user messages
	if part.FunctionCall != nil {
		return functionCallToBlock(part.FunctionCall)
	}

	// Executable code and CodeExecutionResult are Gemini-specific features
	// that don't have direct Anthropic equivalents
	if part.ExecutableCode != nil || part.CodeExecutionResult != nil {
		return nil, fmt.Errorf("ExecutableCode and CodeExecutionResult are not supported by Anthropic")
	}

	return nil, nil
}

// inlineDataToBlock converts inline binary data to an Anthropic content block.
func inlineDataToBlock(blob *genai.Blob) (*anthropic.ContentBlockParamUnion, error) {
	if blob == nil {
		return nil, nil
	}

	mimeType := strings.ToLower(blob.MIMEType)

	// Handle images
	if strings.HasPrefix(mimeType, "image/") {
		mediaType, err := mapImageMediaType(mimeType)
		if err != nil {
			return nil, err
		}
		block := anthropic.ContentBlockParamUnion{
			OfImage: &anthropic.ImageBlockParam{
				Source: anthropic.ImageBlockParamSourceUnion{
					OfBase64: &anthropic.Base64ImageSourceParam{
						Data:      base64.StdEncoding.EncodeToString(blob.Data),
						MediaType: mediaType,
					},
				},
			},
		}
		return &block, nil
	}

	// Handle PDFs (beta feature)
	if mimeType == "application/pdf" {
		block := anthropic.ContentBlockParamUnion{
			OfDocument: &anthropic.DocumentBlockParam{
				Source: anthropic.DocumentBlockParamSourceUnion{
					OfBase64: &anthropic.Base64PDFSourceParam{
						Data: base64.StdEncoding.EncodeToString(blob.Data),
					},
				},
			},
		}
		return &block, nil
	}

	return nil, fmt.Errorf("unsupported MIME type for inline data: %s", mimeType)
}

// mapImageMediaType maps MIME types to Anthropic Base64ImageSourceMediaType.
func mapImageMediaType(mimeType string) (anthropic.Base64ImageSourceMediaType, error) {
	switch mimeType {
	case "image/jpeg":
		return anthropic.Base64ImageSourceMediaTypeImageJPEG, nil
	case "image/png":
		return anthropic.Base64ImageSourceMediaTypeImagePNG, nil
	case "image/gif":
		return anthropic.Base64ImageSourceMediaTypeImageGIF, nil
	case "image/webp":
		return anthropic.Base64ImageSourceMediaTypeImageWebP, nil
	default:
		return "", fmt.Errorf("unsupported image media type: %s", mimeType)
	}
}

// fileDataToBlock converts URI-based file data to an Anthropic content block.
func fileDataToBlock(fileData *genai.FileData) (*anthropic.ContentBlockParamUnion, error) {
	if fileData == nil {
		return nil, nil
	}

	mimeType := strings.ToLower(fileData.MIMEType)

	// Handle images via URL
	if strings.HasPrefix(mimeType, "image/") {
		block := anthropic.ContentBlockParamUnion{
			OfImage: &anthropic.ImageBlockParam{
				Source: anthropic.ImageBlockParamSourceUnion{
					OfURL: &anthropic.URLImageSourceParam{
						URL: fileData.FileURI,
					},
				},
			},
		}
		return &block, nil
	}

	// Handle PDFs via URL (beta feature)
	if mimeType == "application/pdf" {
		block := anthropic.ContentBlockParamUnion{
			OfDocument: &anthropic.DocumentBlockParam{
				Source: anthropic.DocumentBlockParamSourceUnion{
					OfURL: &anthropic.URLPDFSourceParam{
						URL: fileData.FileURI,
					},
				},
			},
		}
		return &block, nil
	}

	return nil, fmt.Errorf("unsupported MIME type for file data: %s", mimeType)
}

// functionResponseToBlock converts a FunctionResponse to an Anthropic tool result block.
func functionResponseToBlock(resp *genai.FunctionResponse) (*anthropic.ContentBlockParamUnion, error) {
	if resp == nil {
		return nil, nil
	}

	// The function ID is required for proper tool call correlation.
	// Without it, Anthropic cannot match tool results to their originating tool calls.
	if resp.ID == "" {
		return nil, fmt.Errorf("FunctionResponse.ID is required for tool call correlation (function: %s)", resp.Name)
	}

	// Convert the response to JSON string
	var content string
	if resp.Response != nil {
		jsonBytes, err := json.Marshal(resp.Response)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal function response: %w", err)
		}
		content = string(jsonBytes)
	}

	block := anthropic.NewToolResultBlock(resp.ID, content, false)
	return &block, nil
}

// functionCallToBlock converts a FunctionCall to an Anthropic tool use block.
// This is used when passing model responses back (e.g., in conversation history).
func functionCallToBlock(call *genai.FunctionCall) (*anthropic.ContentBlockParamUnion, error) {
	if call == nil {
		return nil, nil
	}

	// Anthropic requires input to be a dictionary - ensure we have a valid map
	// After JSON round-trip, nil maps stay nil, so we must always provide a valid map
	var input any = call.Args
	if call.Args == nil || len(call.Args) == 0 {
		input = map[string]any{}
	}

	block := anthropic.NewToolUseBlock(call.ID, input, call.Name)
	return &block, nil
}

// systemInstructionToSystem converts a genai SystemInstruction to Anthropic system text blocks.
func systemInstructionToSystem(instruction *genai.Content) []anthropic.TextBlockParam {
	if instruction == nil || len(instruction.Parts) == 0 {
		return nil
	}

	var blocks []anthropic.TextBlockParam
	for _, part := range instruction.Parts {
		if part != nil && part.Text != "" {
			blocks = append(blocks, anthropic.TextBlockParam{
				Text: part.Text,
			})
		}
	}
	return blocks
}

// mergeConsecutiveMessages merges consecutive messages with the same role.
// Anthropic requires strictly alternating user/assistant messages.
func mergeConsecutiveMessages(messages []anthropic.MessageParam) []anthropic.MessageParam {
	if len(messages) <= 1 {
		return messages
	}

	var merged []anthropic.MessageParam
	for i, msg := range messages {
		if i == 0 {
			merged = append(merged, msg)
			continue
		}

		last := &merged[len(merged)-1]
		if last.Role == msg.Role {
			// Merge content blocks
			last.Content = append(last.Content, msg.Content...)
		} else {
			merged = append(merged, msg)
		}
	}
	return merged
}

// contentsToBetaMessages converts genai Contents to Anthropic BetaMessageParams.
// Used for the Beta API (structured outputs).
func contentsToBetaMessages(contents []*genai.Content) ([]anthropic.BetaMessageParam, error) {
	if len(contents) == 0 {
		return nil, nil
	}

	var messages []anthropic.BetaMessageParam
	for _, content := range contents {
		if content == nil {
			continue
		}

		msg, err := contentToBetaMessage(content)
		if err != nil {
			return nil, fmt.Errorf("failed to convert content: %w", err)
		}
		if msg != nil {
			messages = append(messages, *msg)
		}
	}

	// Merge consecutive messages with the same role
	messages = mergeConsecutiveBetaMessages(messages)

	return messages, nil
}

// contentToBetaMessage converts a single genai.Content to an Anthropic BetaMessageParam.
func contentToBetaMessage(content *genai.Content) (*anthropic.BetaMessageParam, error) {
	if content == nil || len(content.Parts) == 0 {
		return nil, nil
	}

	// Check if this content contains tool results or tool calls
	hasFunctionResponse := false
	hasFunctionCall := false
	for _, part := range content.Parts {
		if part != nil {
			if part.FunctionResponse != nil {
				hasFunctionResponse = true
			}
			if part.FunctionCall != nil {
				hasFunctionCall = true
			}
		}
	}

	// Determine the role
	var role anthropic.BetaMessageParamRole
	if hasFunctionResponse {
		role = anthropic.BetaMessageParamRoleUser
	} else if hasFunctionCall {
		role = anthropic.BetaMessageParamRoleAssistant
	} else {
		var err error
		role, err = mapBetaRole(content.Role)
		if err != nil {
			return nil, err
		}
	}

	var blocks []anthropic.BetaContentBlockParamUnion
	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		block, err := partToBetaContentBlock(part)
		if err != nil {
			return nil, fmt.Errorf("failed to convert part: %w", err)
		}
		if block != nil {
			blocks = append(blocks, *block)
		}
	}

	if len(blocks) == 0 {
		return nil, nil
	}

	msg := anthropic.BetaMessageParam{
		Role:    role,
		Content: blocks,
	}
	return &msg, nil
}

// mapBetaRole maps genai role to Anthropic BetaMessageParamRole.
func mapBetaRole(role string) (anthropic.BetaMessageParamRole, error) {
	switch strings.ToLower(role) {
	case "user":
		return anthropic.BetaMessageParamRoleUser, nil
	case "model", "assistant":
		return anthropic.BetaMessageParamRoleAssistant, nil
	default:
		return "", fmt.Errorf("unsupported role: %s", role)
	}
}

// partToBetaContentBlock converts a genai Part to an Anthropic BetaContentBlockParamUnion.
func partToBetaContentBlock(part *genai.Part) (*anthropic.BetaContentBlockParamUnion, error) {
	if part == nil {
		return nil, nil
	}

	// Text content
	if part.Text != "" {
		if part.Thought && len(part.ThoughtSignature) > 0 {
			block := anthropic.BetaContentBlockParamUnion{
				OfThinking: &anthropic.BetaThinkingBlockParam{
					Thinking:  part.Text,
					Signature: base64.StdEncoding.EncodeToString(part.ThoughtSignature),
				},
			}
			return &block, nil
		}
		block := anthropic.BetaContentBlockParamUnion{
			OfText: &anthropic.BetaTextBlockParam{
				Text: part.Text,
			},
		}
		return &block, nil
	}

	// Inline binary data (images, PDFs)
	if part.InlineData != nil {
		return inlineDataToBetaBlock(part.InlineData)
	}

	// File data (URI-based)
	if part.FileData != nil {
		return fileDataToBetaBlock(part.FileData)
	}

	// Function response (tool result)
	if part.FunctionResponse != nil {
		return functionResponseToBetaBlock(part.FunctionResponse)
	}

	// Function call
	if part.FunctionCall != nil {
		return functionCallToBetaBlock(part.FunctionCall)
	}

	// Executable code and CodeExecutionResult are Gemini-specific
	if part.ExecutableCode != nil || part.CodeExecutionResult != nil {
		return nil, fmt.Errorf("ExecutableCode and CodeExecutionResult are not supported by Anthropic")
	}

	return nil, nil
}

// inlineDataToBetaBlock converts inline binary data to a Beta content block.
func inlineDataToBetaBlock(blob *genai.Blob) (*anthropic.BetaContentBlockParamUnion, error) {
	if blob == nil {
		return nil, nil
	}

	mimeType := strings.ToLower(blob.MIMEType)

	// Handle images
	if strings.HasPrefix(mimeType, "image/") {
		mediaType, err := mapBetaImageMediaType(mimeType)
		if err != nil {
			return nil, err
		}
		block := anthropic.BetaContentBlockParamUnion{
			OfImage: &anthropic.BetaImageBlockParam{
				Source: anthropic.BetaImageBlockParamSourceUnion{
					OfBase64: &anthropic.BetaBase64ImageSourceParam{
						Data:      base64.StdEncoding.EncodeToString(blob.Data),
						MediaType: mediaType,
					},
				},
			},
		}
		return &block, nil
	}

	// Handle PDFs
	if mimeType == "application/pdf" {
		block := anthropic.BetaContentBlockParamUnion{
			OfDocument: &anthropic.BetaRequestDocumentBlockParam{
				Source: anthropic.BetaRequestDocumentBlockSourceUnionParam{
					OfBase64: &anthropic.BetaBase64PDFSourceParam{
						Data: base64.StdEncoding.EncodeToString(blob.Data),
					},
				},
			},
		}
		return &block, nil
	}

	return nil, fmt.Errorf("unsupported MIME type for inline data: %s", mimeType)
}

// mapBetaImageMediaType maps MIME types to Anthropic BetaBase64ImageSourceMediaType.
func mapBetaImageMediaType(mimeType string) (anthropic.BetaBase64ImageSourceMediaType, error) {
	switch mimeType {
	case "image/jpeg":
		return anthropic.BetaBase64ImageSourceMediaTypeImageJPEG, nil
	case "image/png":
		return anthropic.BetaBase64ImageSourceMediaTypeImagePNG, nil
	case "image/gif":
		return anthropic.BetaBase64ImageSourceMediaTypeImageGIF, nil
	case "image/webp":
		return anthropic.BetaBase64ImageSourceMediaTypeImageWebP, nil
	default:
		return "", fmt.Errorf("unsupported image media type: %s", mimeType)
	}
}

// fileDataToBetaBlock converts URI-based file data to a Beta content block.
func fileDataToBetaBlock(fileData *genai.FileData) (*anthropic.BetaContentBlockParamUnion, error) {
	if fileData == nil {
		return nil, nil
	}

	mimeType := strings.ToLower(fileData.MIMEType)

	// Handle images via URL
	if strings.HasPrefix(mimeType, "image/") {
		block := anthropic.BetaContentBlockParamUnion{
			OfImage: &anthropic.BetaImageBlockParam{
				Source: anthropic.BetaImageBlockParamSourceUnion{
					OfURL: &anthropic.BetaURLImageSourceParam{
						URL: fileData.FileURI,
					},
				},
			},
		}
		return &block, nil
	}

	// Handle PDFs via URL
	if mimeType == "application/pdf" {
		block := anthropic.BetaContentBlockParamUnion{
			OfDocument: &anthropic.BetaRequestDocumentBlockParam{
				Source: anthropic.BetaRequestDocumentBlockSourceUnionParam{
					OfURL: &anthropic.BetaURLPDFSourceParam{
						URL: fileData.FileURI,
					},
				},
			},
		}
		return &block, nil
	}

	return nil, fmt.Errorf("unsupported MIME type for file data: %s", mimeType)
}

// functionResponseToBetaBlock converts a FunctionResponse to a Beta tool result block.
func functionResponseToBetaBlock(resp *genai.FunctionResponse) (*anthropic.BetaContentBlockParamUnion, error) {
	if resp == nil {
		return nil, nil
	}

	if resp.ID == "" {
		return nil, fmt.Errorf("FunctionResponse.ID is required for tool call correlation (function: %s)", resp.Name)
	}

	var content string
	if resp.Response != nil {
		jsonBytes, err := json.Marshal(resp.Response)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal function response: %w", err)
		}
		content = string(jsonBytes)
	}

	block := anthropic.BetaContentBlockParamUnion{
		OfToolResult: &anthropic.BetaToolResultBlockParam{
			ToolUseID: resp.ID,
			Content: []anthropic.BetaToolResultBlockParamContentUnion{
				{OfText: &anthropic.BetaTextBlockParam{Text: content}},
			},
		},
	}
	return &block, nil
}

// functionCallToBetaBlock converts a FunctionCall to a Beta tool use block.
func functionCallToBetaBlock(call *genai.FunctionCall) (*anthropic.BetaContentBlockParamUnion, error) {
	if call == nil {
		return nil, nil
	}

	var input any = call.Args
	if call.Args == nil || len(call.Args) == 0 {
		input = map[string]any{}
	}

	block := anthropic.BetaContentBlockParamUnion{
		OfToolUse: &anthropic.BetaToolUseBlockParam{
			ID:    call.ID,
			Name:  call.Name,
			Input: input,
		},
	}
	return &block, nil
}

// systemInstructionToBetaSystem converts a genai SystemInstruction to Beta system text blocks.
func systemInstructionToBetaSystem(instruction *genai.Content) []anthropic.BetaTextBlockParam {
	if instruction == nil || len(instruction.Parts) == 0 {
		return nil
	}

	var blocks []anthropic.BetaTextBlockParam
	for _, part := range instruction.Parts {
		if part != nil && part.Text != "" {
			blocks = append(blocks, anthropic.BetaTextBlockParam{
				Text: part.Text,
			})
		}
	}
	return blocks
}

// mergeConsecutiveBetaMessages merges consecutive Beta messages with the same role.
func mergeConsecutiveBetaMessages(messages []anthropic.BetaMessageParam) []anthropic.BetaMessageParam {
	if len(messages) <= 1 {
		return messages
	}

	var merged []anthropic.BetaMessageParam
	for i, msg := range messages {
		if i == 0 {
			merged = append(merged, msg)
			continue
		}

		last := &merged[len(merged)-1]
		if last.Role == msg.Role {
			last.Content = append(last.Content, msg.Content...)
		} else {
			merged = append(merged, msg)
		}
	}
	return merged
}

// messageToLLMResponse converts an Anthropic Message to a model.LLMResponse.
func messageToLLMResponse(msg *anthropic.Message) (*model.LLMResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil message received")
	}

	content := &genai.Content{
		Role:  "model",
		Parts: make([]*genai.Part, 0, len(msg.Content)),
	}

	var allCitations []*genai.Citation
	for _, block := range msg.Content {
		part, err := contentBlockToGenaiPart(block)
		if err != nil {
			return nil, fmt.Errorf("failed to convert content block: %w", err)
		}
		if part != nil {
			content.Parts = append(content.Parts, part)
		}
		// Collect citations from text blocks
		if textBlock, ok := block.AsAny().(anthropic.TextBlock); ok {
			if citations := textCitationsToSlice(textBlock.Citations); len(citations) > 0 {
				allCitations = append(allCitations, citations...)
			}
		}
	}

	resp := &model.LLMResponse{
		Content:       content,
		UsageMetadata: usageToMetadata(msg.Usage),
		FinishReason:  stopReasonToFinishReason(msg.StopReason),
	}

	if len(allCitations) > 0 {
		resp.CitationMetadata = &genai.CitationMetadata{Citations: allCitations}
	}

	return resp, nil
}

// contentBlockToGenaiPart converts an Anthropic ContentBlockUnion to a genai.Part.
func contentBlockToGenaiPart(block anthropic.ContentBlockUnion) (*genai.Part, error) {
	switch variant := block.AsAny().(type) {
	case anthropic.TextBlock:
		return &genai.Part{Text: variant.Text}, nil

	case anthropic.ThinkingBlock:
		// Map thinking blocks to genai.Part with Thought=true
		signature, _ := base64.StdEncoding.DecodeString(variant.Signature)
		return &genai.Part{
			Text:             variant.Thinking,
			Thought:          true,
			ThoughtSignature: signature,
		}, nil

	case anthropic.RedactedThinkingBlock:
		// Redacted thinking - we can't see the content but preserve the marker
		return &genai.Part{
			Text:    "[thinking redacted]",
			Thought: true,
		}, nil

	case anthropic.ToolUseBlock:
		// Convert to FunctionCall
		args := make(map[string]any)
		if variant.Input != nil {
			// Input is json.RawMessage, unmarshal it
			if err := json.Unmarshal(variant.Input, &args); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tool input for %q (id=%s): %w", variant.Name, variant.ID, err)
			}
		}
		return &genai.Part{
			FunctionCall: &genai.FunctionCall{
				ID:   variant.ID,
				Name: variant.Name,
				Args: args,
			},
		}, nil

	case anthropic.ServerToolUseBlock:
		// Server-side tool use (web search, etc.)
		args := make(map[string]any)
		if variant.Input != nil {
			// Input is an any type, convert through JSON
			inputBytes, err := json.Marshal(variant.Input)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal server tool input for %q (id=%s): %w", variant.Name, variant.ID, err)
			}
			if err := json.Unmarshal(inputBytes, &args); err != nil {
				return nil, fmt.Errorf("failed to unmarshal server tool input for %q (id=%s): %w", variant.Name, variant.ID, err)
			}
		}
		return &genai.Part{
			FunctionCall: &genai.FunctionCall{
				ID:   variant.ID,
				Name: string(variant.Name),
				Args: args,
			},
		}, nil

	case anthropic.WebSearchToolResultBlock:
		// Web search results from Anthropic's built-in web search tool
		return webSearchResultToFunctionResponse(variant), nil

	default:
		// Unknown block type - skip
		return nil, nil
	}
}

// webSearchResultToFunctionResponse converts a WebSearchToolResultBlock to a FunctionResponse Part.
func webSearchResultToFunctionResponse(block anthropic.WebSearchToolResultBlock) *genai.Part {
	response := make(map[string]any)

	// Check if it's an error or results
	if results := block.Content.AsWebSearchResultBlockArray(); len(results) > 0 {
		searchResults := make([]map[string]any, 0, len(results))
		for _, result := range results {
			searchResults = append(searchResults, map[string]any{
				"title":   result.Title,
				"url":     result.URL,
				"pageAge": result.PageAge,
			})
		}
		response["results"] = searchResults
	} else if errBlock := block.Content.AsResponseWebSearchToolResultError(); errBlock.ErrorCode != "" {
		response["error"] = string(errBlock.ErrorCode)
	}

	return &genai.Part{
		FunctionResponse: &genai.FunctionResponse{
			ID:       block.ToolUseID,
			Name:     "web_search",
			Response: response,
		},
	}
}

// textCitationsToSlice converts Anthropic text citations to a slice of genai.Citation.
func textCitationsToSlice(citations []anthropic.TextCitationUnion) []*genai.Citation {
	if len(citations) == 0 {
		return nil
	}

	result := make([]*genai.Citation, 0, len(citations))
	for _, c := range citations {
		citation := &genai.Citation{
			Title: c.DocumentTitle,
		}

		// Map based on citation type
		switch c.Type {
		case "char_location":
			citation.StartIndex = int32(c.StartCharIndex)
			citation.EndIndex = int32(c.EndCharIndex)
		case "web_search_result_location":
			citation.Title = c.Title
			citation.URI = c.URL
		case "search_result_location":
			citation.Title = c.Title
		}

		result = append(result, citation)
	}

	return result
}

// usageToMetadata converts Anthropic Usage to genai UsageMetadata.
func usageToMetadata(usage anthropic.Usage) *genai.GenerateContentResponseUsageMetadata {
	return &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     int32(usage.InputTokens),
		CandidatesTokenCount: int32(usage.OutputTokens),
		TotalTokenCount:      int32(usage.InputTokens + usage.OutputTokens),
	}
}

// stopReasonToFinishReason maps Anthropic StopReason to genai FinishReason.
func stopReasonToFinishReason(sr anthropic.StopReason) genai.FinishReason {
	switch sr {
	case anthropic.StopReasonEndTurn:
		return genai.FinishReasonStop
	case anthropic.StopReasonMaxTokens:
		return genai.FinishReasonMaxTokens
	case anthropic.StopReasonStopSequence:
		return genai.FinishReasonStop
	case anthropic.StopReasonToolUse:
		return genai.FinishReasonStop
	default:
		return genai.FinishReasonUnspecified
	}
}

// streamDeltaToPartialResponse converts a streaming content block delta to a partial LLMResponse.
// Used for streaming text updates.
func streamDeltaToPartialResponse(text string) *model.LLMResponse {
	return &model.LLMResponse{
		Content: &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{Text: text},
			},
		},
		Partial: true,
	}
}

// streamThinkingDeltaToPartialResponse converts a streaming thinking delta to a partial LLMResponse.
func streamThinkingDeltaToPartialResponse(thinking string) *model.LLMResponse {
	return &model.LLMResponse{
		Content: &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{
					Text:    thinking,
					Thought: true,
				},
			},
		},
		Partial: true,
	}
}

// betaMessageToLLMResponse converts an Anthropic BetaMessage to a model.LLMResponse.
// Used for the Beta API (structured outputs).
func betaMessageToLLMResponse(msg *anthropic.BetaMessage) (*model.LLMResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil message received")
	}

	content := &genai.Content{
		Role:  "model",
		Parts: make([]*genai.Part, 0, len(msg.Content)),
	}

	for _, block := range msg.Content {
		part, err := betaContentBlockToGenaiPart(block)
		if err != nil {
			return nil, fmt.Errorf("failed to convert content block: %w", err)
		}
		if part != nil {
			content.Parts = append(content.Parts, part)
		}
	}

	resp := &model.LLMResponse{
		Content:       content,
		UsageMetadata: betaUsageToMetadata(msg.Usage),
		FinishReason:  betaStopReasonToFinishReason(msg.StopReason),
	}

	return resp, nil
}

// betaContentBlockToGenaiPart converts an Anthropic BetaContentBlockUnion to a genai.Part.
func betaContentBlockToGenaiPart(block anthropic.BetaContentBlockUnion) (*genai.Part, error) {
	switch variant := block.AsAny().(type) {
	case anthropic.BetaTextBlock:
		return &genai.Part{Text: variant.Text}, nil

	case anthropic.BetaThinkingBlock:
		signature, _ := base64.StdEncoding.DecodeString(variant.Signature)
		return &genai.Part{
			Text:             variant.Thinking,
			Thought:          true,
			ThoughtSignature: signature,
		}, nil

	case anthropic.BetaRedactedThinkingBlock:
		return &genai.Part{
			Text:    "[thinking redacted]",
			Thought: true,
		}, nil

	case anthropic.BetaToolUseBlock:
		args := make(map[string]any)
		if variant.Input != nil {
			// Input is 'any' type, convert through JSON round-trip
			inputBytes, err := json.Marshal(variant.Input)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal tool input for %q (id=%s): %w", variant.Name, variant.ID, err)
			}
			if err := json.Unmarshal(inputBytes, &args); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tool input for %q (id=%s): %w", variant.Name, variant.ID, err)
			}
		}
		return &genai.Part{
			FunctionCall: &genai.FunctionCall{
				ID:   variant.ID,
				Name: variant.Name,
				Args: args,
			},
		}, nil

	default:
		return nil, nil
	}
}

// betaUsageToMetadata converts Anthropic BetaUsage to genai UsageMetadata.
func betaUsageToMetadata(usage anthropic.BetaUsage) *genai.GenerateContentResponseUsageMetadata {
	return &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     int32(usage.InputTokens),
		CandidatesTokenCount: int32(usage.OutputTokens),
		TotalTokenCount:      int32(usage.InputTokens + usage.OutputTokens),
	}
}

// betaStopReasonToFinishReason maps Anthropic BetaStopReason to genai FinishReason.
func betaStopReasonToFinishReason(sr anthropic.BetaStopReason) genai.FinishReason {
	switch sr {
	case anthropic.BetaStopReasonEndTurn:
		return genai.FinishReasonStop
	case anthropic.BetaStopReasonMaxTokens:
		return genai.FinishReasonMaxTokens
	case anthropic.BetaStopReasonStopSequence:
		return genai.FinishReasonStop
	case anthropic.BetaStopReasonToolUse:
		return genai.FinishReasonStop
	default:
		return genai.FinishReasonUnspecified
	}
}

// toolsToAnthropicTools converts genai Tools to Anthropic ToolUnionParams.
func toolsToAnthropicTools(tools []*genai.Tool) []anthropic.ToolUnionParam {
	if len(tools) == 0 {
		return nil
	}

	var result []anthropic.ToolUnionParam
	for _, tool := range tools {
		if tool == nil || len(tool.FunctionDeclarations) == 0 {
			continue
		}
		for _, fd := range tool.FunctionDeclarations {
			if fd == nil {
				continue
			}
			toolParam := functionDeclarationToTool(fd)
			result = append(result, toolParam)
		}
	}
	return result
}

// functionDeclarationToTool converts a genai FunctionDeclaration to an Anthropic ToolUnionParam.
//
// If Parameters is set, it takes precedence over ParametersJsonSchema.
// ParametersJsonSchema currently supports:
//   - map[string]any with "properties" and "required" keys
//   - *jsonschema.Schema
//
// Other ParametersJsonSchema types are ignored.
func functionDeclarationToTool(fd *genai.FunctionDeclaration) anthropic.ToolUnionParam {
	inputSchema := anthropic.ToolInputSchemaParam{
		// Anthropic tools require an object schema at the root.
		// The SDK defaults to type "object" when not specified.
		Properties: map[string]any{},
	}

	// Convert parameters schema - Parameters takes precedence over ParametersJsonSchema
	if fd.Parameters != nil {
		props := schemaPropertiesToMap(fd.Parameters.Properties)
		if props != nil {
			inputSchema.Properties = props
		}
		if len(fd.Parameters.Required) > 0 {
			inputSchema.Required = fd.Parameters.Required
		}
	} else if fd.ParametersJsonSchema != nil {
		switch schema := fd.ParametersJsonSchema.(type) {
		case map[string]any:
			if props, ok := schema["properties"].(map[string]any); ok {
				inputSchema.Properties = props
			}
			inputSchema.Required = extractRequiredFields(schema["required"])
		case *jsonschema.Schema:
			if props := jsonSchemaToProperties(schema); props != nil {
				inputSchema.Properties = props
			}
			if len(schema.Required) > 0 {
				inputSchema.Required = schema.Required
			}
		}
	}

	return anthropic.ToolUnionParam{
		OfTool: &anthropic.ToolParam{
			Name:        fd.Name,
			Description: anthropic.String(fd.Description),
			InputSchema: inputSchema,
		},
	}
}

// extractRequiredFields extracts required field names from various input types.
// Supports []any (from JSON unmarshalling) and []string (from manual construction).
func extractRequiredFields(v any) []string {
	if v == nil {
		return nil
	}
	switch req := v.(type) {
	case []string:
		return req
	case []any:
		result := make([]string, 0, len(req))
		for _, r := range req {
			if s, ok := r.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// jsonSchemaToProperties converts a jsonschema.Schema to a properties map.
// Returns nil if schema or its properties are nil, consistent with schemaPropertiesToMap.
func jsonSchemaToProperties(schema *jsonschema.Schema) map[string]any {
	if schema == nil || schema.Properties == nil {
		return nil
	}

	props := make(map[string]any)
	for name, propSchema := range schema.Properties {
		props[name] = jsonSchemaPropertyToMap(propSchema)
	}
	return props
}

// jsonSchemaPropertyToMap converts a single jsonschema.Schema property to a map.
func jsonSchemaPropertyToMap(schema *jsonschema.Schema) map[string]any {
	if schema == nil {
		return nil
	}

	result := make(map[string]any)

	if schema.Type != "" {
		result["type"] = string(schema.Type)
	}
	if schema.Description != "" {
		result["description"] = schema.Description
	}
	if len(schema.Enum) > 0 {
		result["enum"] = schema.Enum
	}
	if schema.Items != nil {
		result["items"] = jsonSchemaPropertyToMap(schema.Items)
	}
	if schema.Properties != nil {
		result["properties"] = jsonSchemaToProperties(schema)
	}
	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}

	return result
}

// schemaPropertiesToMap converts genai Schema properties to a map for Anthropic.
func schemaPropertiesToMap(props map[string]*genai.Schema) map[string]any {
	if props == nil {
		return nil
	}

	result := make(map[string]any)
	for name, schema := range props {
		if schema == nil {
			continue
		}
		result[name] = schemaToMap(schema)
	}
	return result
}

// schemaToMap converts a genai.Schema to a map[string]any suitable for Anthropic.
func schemaToMap(schema *genai.Schema) map[string]any {
	if schema == nil {
		return nil
	}

	result := make(map[string]any)

	// Type
	if schema.Type != "" {
		result["type"] = strings.ToLower(string(schema.Type))
	}

	// Description
	if schema.Description != "" {
		result["description"] = schema.Description
	}

	// Enum
	if len(schema.Enum) > 0 {
		result["enum"] = schema.Enum
	}

	// Format
	if schema.Format != "" {
		result["format"] = schema.Format
	}

	// Items (for arrays)
	if schema.Items != nil {
		result["items"] = schemaToMap(schema.Items)
	}

	// Properties (for objects)
	if len(schema.Properties) > 0 {
		result["properties"] = schemaPropertiesToMap(schema.Properties)
	}

	// Required
	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}

	// Nullable
	if schema.Nullable != nil && *schema.Nullable {
		result["nullable"] = true
	}

	// Default
	if schema.Default != nil {
		result["default"] = schema.Default
	}

	// Min/Max constraints
	if schema.Minimum != nil {
		result["minimum"] = *schema.Minimum
	}
	if schema.Maximum != nil {
		result["maximum"] = *schema.Maximum
	}
	if schema.MinLength != nil {
		result["minLength"] = *schema.MinLength
	}
	if schema.MaxLength != nil {
		result["maxLength"] = *schema.MaxLength
	}
	if schema.MinItems != nil {
		result["minItems"] = *schema.MinItems
	}
	if schema.MaxItems != nil {
		result["maxItems"] = *schema.MaxItems
	}

	// Pattern
	if schema.Pattern != "" {
		result["pattern"] = schema.Pattern
	}

	// AnyOf
	if len(schema.AnyOf) > 0 {
		anyOf := make([]map[string]any, 0, len(schema.AnyOf))
		for _, s := range schema.AnyOf {
			if m := schemaToMap(s); m != nil {
				anyOf = append(anyOf, m)
			}
		}
		if len(anyOf) > 0 {
			result["anyOf"] = anyOf
		}
	}

	return result
}

// toolsToBetaAnthropicTools converts genai Tools to Anthropic BetaToolUnionParams.
// Used for the Beta API (structured outputs).
func toolsToBetaAnthropicTools(tools []*genai.Tool) []anthropic.BetaToolUnionParam {
	if len(tools) == 0 {
		return nil
	}

	var result []anthropic.BetaToolUnionParam
	for _, tool := range tools {
		if tool == nil || len(tool.FunctionDeclarations) == 0 {
			continue
		}
		for _, fd := range tool.FunctionDeclarations {
			if fd == nil {
				continue
			}
			toolParam := functionDeclarationToBetaTool(fd)
			result = append(result, toolParam)
		}
	}
	return result
}

// functionDeclarationToBetaTool converts a genai FunctionDeclaration to a BetaToolUnionParam.
func functionDeclarationToBetaTool(fd *genai.FunctionDeclaration) anthropic.BetaToolUnionParam {
	inputSchema := anthropic.BetaToolInputSchemaParam{
		Properties: map[string]any{},
	}

	// Convert parameters schema
	if fd.Parameters != nil {
		props := schemaPropertiesToMap(fd.Parameters.Properties)
		if props != nil {
			inputSchema.Properties = props
		}
		if len(fd.Parameters.Required) > 0 {
			inputSchema.Required = fd.Parameters.Required
		}
	} else if fd.ParametersJsonSchema != nil {
		switch schema := fd.ParametersJsonSchema.(type) {
		case map[string]any:
			if props, ok := schema["properties"].(map[string]any); ok {
				inputSchema.Properties = props
			}
			inputSchema.Required = extractRequiredFields(schema["required"])
		case *jsonschema.Schema:
			if props := jsonSchemaToProperties(schema); props != nil {
				inputSchema.Properties = props
			}
			if len(schema.Required) > 0 {
				inputSchema.Required = schema.Required
			}
		}
	}

	return anthropic.BetaToolUnionParam{
		OfTool: &anthropic.BetaToolParam{
			Name:        fd.Name,
			Description: anthropic.String(fd.Description),
			InputSchema: inputSchema,
		},
	}
}
