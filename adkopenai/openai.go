package adkopenai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

type Config struct {
	APIKey  string
	BaseURL string
}

type openaiModel struct {
	client openai.Client
	name   string
}

// NewModel creates a new OpenAI model adapter for ADK.
// If cfg is nil, it will use environment variables (OPENAI_API_KEY, OPENAI_API_BASE_URL).
func NewModel(modelName string, cfg *Config) model.LLM {
	if cfg == nil {
		cfg = &Config{}
	}
	return &openaiModel{
		client: newClient(cfg),
		name:   modelName,
	}
}

func newClient(cfg *Config) openai.Client {
	var opts []option.RequestOption

	if apiKey := getConfigOrEnv(cfg.APIKey, "OPENAI_API_KEY"); apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if baseURL := getConfigOrEnv(cfg.BaseURL, "OPENAI_API_BASE_URL"); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	return openai.NewClient(opts...)
}

func getConfigOrEnv(configValue, envKey string) string {
	if configValue != "" {
		return configValue
	}
	return os.Getenv(envKey)
}

func (m *openaiModel) Name() string {
	return m.name
}

func (m *openaiModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	if stream {
		return m.generateStream(ctx, req)
	}
	return func(yield func(*model.LLMResponse, error) bool) {
		resp, err := m.generate(ctx, req)
		yield(resp, err)
	}
}

func (m *openaiModel) generate(ctx context.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	params, err := m.convertRequest(req)
	if err != nil {
		return nil, err
	}

	completion, err := m.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}

	return chatCompletionToLLMResponse(completion), nil
}

func (m *openaiModel) generateStream(ctx context.Context, req *model.LLMRequest) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		params, err := m.convertRequest(req)
		if err != nil {
			yield(nil, err)
			return
		}

		params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		}

		stream := m.client.Chat.Completions.NewStreaming(ctx, params)

		// Accumulate tool call chunks since they arrive in pieces
		var chunks []*openai.ChatCompletionChunk
		var hasToolCalls bool

		for stream.Next() {
			chunk := stream.Current()

			if len(chunk.Choices) > 0 && len(chunk.Choices[0].Delta.ToolCalls) > 0 {
				hasToolCalls = true
			}

			if hasToolCalls {
				chunks = append(chunks, &chunk)
				if len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason != "" {
					resp := accumulateChunks(chunks)
					if !yield(resp, nil) {
						return
					}
				}
				continue
			}

			if resp := chatCompletionChunkToLLMResponse(&chunk); resp != nil {
				if !yield(resp, nil) {
					return
				}
			}
		}

		if err := stream.Err(); err != nil {
			yield(nil, err)
		}
	}
}

func (m *openaiModel) convertRequest(req *model.LLMRequest) (openai.ChatCompletionNewParams, error) {
	params := openai.ChatCompletionNewParams{Model: m.name}

	var messages []openai.ChatCompletionMessageParamUnion

	cfg := req.Config
	if cfg != nil {
		if sysMsg, ok := systemInstructionToOpenAI(cfg.SystemInstruction); ok {
			messages = append(messages, sysMsg)
		}
	}

	messages = append(messages, contentsToOpenAIMessages(req.Contents)...)
	params.Messages = messages

	if cfg == nil {
		return params, nil
	}

	if cfg.MaxOutputTokens > 0 {
		params.MaxCompletionTokens = openai.Int(int64(cfg.MaxOutputTokens))
	}
	if cfg.Temperature != nil {
		params.Temperature = openai.Float(float64(*cfg.Temperature))
	}
	if cfg.TopP != nil {
		params.TopP = openai.Float(float64(*cfg.TopP))
	}
	if len(cfg.StopSequences) > 0 {
		params.Stop = openai.ChatCompletionNewParamsStopUnion{
			OfChatCompletionNewsStopArray: cfg.StopSequences,
		}
	}
	if cfg.PresencePenalty != nil {
		params.PresencePenalty = openai.Float(float64(*cfg.PresencePenalty))
	}
	if cfg.FrequencyPenalty != nil {
		params.FrequencyPenalty = openai.Float(float64(*cfg.FrequencyPenalty))
	}
	if cfg.Seed != nil {
		params.Seed = openai.Int(int64(*cfg.Seed))
	}
	if tools := toolsToOpenAITools(cfg.Tools); len(tools) > 0 {
		params.Tools = tools
	}
	if cfg.ResponseSchema != nil {
		if format := responseFormatToOpenAI(cfg.ResponseSchema); format != nil {
			params.ResponseFormat = *format
		}
	}

	return params, nil
}

func systemInstructionToOpenAI(instruction *genai.Content) (openai.ChatCompletionMessageParamUnion, bool) {
	if instruction == nil || len(instruction.Parts) == 0 {
		return openai.ChatCompletionMessageParamUnion{}, false
	}

	var text string
	for _, part := range instruction.Parts {
		if part != nil && part.Text != "" {
			if text != "" {
				text += "\n"
			}
			text += part.Text
		}
	}

	if text == "" {
		return openai.ChatCompletionMessageParamUnion{}, false
	}

	return openai.SystemMessage(text), true
}

func contentsToOpenAIMessages(contents []*genai.Content) []openai.ChatCompletionMessageParamUnion {
	if len(contents) == 0 {
		return nil
	}

	var messages []openai.ChatCompletionMessageParamUnion
	for _, content := range contents {
		if content != nil {
			messages = append(messages, contentToOpenAIMessages(content)...)
		}
	}
	return messages
}

func contentToOpenAIMessages(content *genai.Content) []openai.ChatCompletionMessageParamUnion {
	if content == nil || len(content.Parts) == 0 {
		return nil
	}

	role := roleToOpenAI(content.Role)

	// Separate tool results from other parts (OpenAI requires separate messages)
	var toolResults []*genai.Part
	var otherParts []*genai.Part
	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		if part.FunctionResponse != nil {
			toolResults = append(toolResults, part)
		} else {
			otherParts = append(otherParts, part)
		}
	}

	var messages []openai.ChatCompletionMessageParamUnion

	for _, tr := range toolResults {
		messages = append(messages, toolResultToOpenAIMessage(tr))
	}

	if len(otherParts) > 0 {
		switch role {
		case "system":
			if msg, ok := partsToSystemMessage(otherParts); ok {
				messages = append(messages, msg)
			}
		case "user":
			if msg, ok := partsToUserMessage(otherParts); ok {
				messages = append(messages, msg)
			}
		case "assistant":
			if msg, ok := partsToAssistantMessage(otherParts); ok {
				messages = append(messages, msg)
			}
		}
	}

	return messages
}

func roleToOpenAI(role string) string {
	switch role {
	case "model":
		return "assistant"
	case "user", "system", "tool":
		return role
	default:
		return "user"
	}
}

func toolResultToOpenAIMessage(part *genai.Part) openai.ChatCompletionMessageParamUnion {
	funcResp := part.FunctionResponse

	content, err := json.Marshal(funcResp.Response)
	if err != nil {
		content = fmt.Appendf(nil, "%v", funcResp.Response)
	}

	return openai.ToolMessage(string(content), funcResp.ID)
}

func partsToSystemMessage(parts []*genai.Part) (openai.ChatCompletionMessageParamUnion, bool) {
	var textParts []string
	for _, part := range parts {
		if part != nil && part.Text != "" {
			textParts = append(textParts, part.Text)
		}
	}
	if len(textParts) == 0 {
		return openai.ChatCompletionMessageParamUnion{}, false
	}
	return openai.SystemMessage(strings.Join(textParts, "\n")), true
}

func partsToUserMessage(parts []*genai.Part) (openai.ChatCompletionMessageParamUnion, bool) {
	var contentParts []openai.ChatCompletionContentPartUnionParam
	for _, part := range parts {
		if cp, ok := partToUserContentPart(part); ok {
			contentParts = append(contentParts, cp)
		}
	}
	if len(contentParts) == 0 {
		return openai.ChatCompletionMessageParamUnion{}, false
	}
	return openai.UserMessage(contentParts), true
}

func partToUserContentPart(part *genai.Part) (openai.ChatCompletionContentPartUnionParam, bool) {
	if part == nil {
		return openai.ChatCompletionContentPartUnionParam{}, false
	}
	if part.Text != "" {
		return openai.TextContentPart(part.Text), true
	}
	if part.InlineData != nil {
		return blobToImagePart(part.InlineData), true
	}
	if part.FileData != nil && part.FileData.FileURI != "" {
		return openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
			URL: part.FileData.FileURI,
		}), true
	}
	return openai.ChatCompletionContentPartUnionParam{}, false
}

func blobToImagePart(blob *genai.Blob) openai.ChatCompletionContentPartUnionParam {
	dataURI := fmt.Sprintf("data:%s;base64,%s", blob.MIMEType, base64.StdEncoding.EncodeToString(blob.Data))
	return openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
		URL: dataURI,
	})
}

func partsToAssistantMessage(parts []*genai.Part) (openai.ChatCompletionMessageParamUnion, bool) {
	var textContent string
	var toolCalls []openai.ChatCompletionMessageToolCallParam

	for _, part := range parts {
		if part == nil {
			continue
		}
		if part.Text != "" {
			textContent += part.Text
		}
		if part.FunctionCall != nil {
			toolCalls = append(toolCalls, functionCallToToolCall(part.FunctionCall))
		}
	}

	if len(toolCalls) > 0 {
		msg := openai.ChatCompletionAssistantMessageParam{
			ToolCalls: toolCalls,
		}
		if textContent != "" {
			msg.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
				OfString: openai.String(textContent),
			}
		}
		return openai.ChatCompletionMessageParamUnion{OfAssistant: &msg}, true
	}

	if textContent != "" {
		return openai.AssistantMessage(textContent), true
	}

	return openai.ChatCompletionMessageParamUnion{}, false
}

func functionCallToToolCall(fc *genai.FunctionCall) openai.ChatCompletionMessageToolCallParam {
	args, err := json.Marshal(fc.Args)
	if err != nil {
		args = []byte("{}")
	}
	return openai.ChatCompletionMessageToolCallParam{
		ID:   fc.ID,
		Type: "function",
		Function: openai.ChatCompletionMessageToolCallFunctionParam{
			Name:      fc.Name,
			Arguments: string(args),
		},
	}
}

func toolsToOpenAITools(tools []*genai.Tool) []openai.ChatCompletionToolParam {
	var result []openai.ChatCompletionToolParam
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		for _, fd := range tool.FunctionDeclarations {
			if fd != nil {
				result = append(result, functionDeclarationToTool(fd))
			}
		}
	}
	return result
}

func functionDeclarationToTool(fd *genai.FunctionDeclaration) openai.ChatCompletionToolParam {
	params := schemaToMap(fd.Parameters)
	if params == nil {
		params = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: openai.FunctionDefinitionParam{
			Name:        fd.Name,
			Description: openai.String(fd.Description),
			Parameters:  params,
		},
	}
}

func schemaToMap(schema *genai.Schema) map[string]any {
	if schema == nil {
		return nil
	}

	result := map[string]any{
		"type": schemaTypeToString(schema.Type),
	}

	if schema.Description != "" {
		result["description"] = schema.Description
	}
	if len(schema.Enum) > 0 {
		result["enum"] = schema.Enum
	}
	if schema.Format != "" {
		result["format"] = schema.Format
	}
	if len(schema.Properties) > 0 {
		props := make(map[string]any)
		for name, propSchema := range schema.Properties {
			props[name] = schemaToMap(propSchema)
		}
		result["properties"] = props
	}
	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}
	if schema.Items != nil {
		result["items"] = schemaToMap(schema.Items)
	}
	if schema.Minimum != nil {
		result["minimum"] = *schema.Minimum
	}
	if schema.Maximum != nil {
		result["maximum"] = *schema.Maximum
	}
	if schema.MinItems != nil {
		result["minItems"] = *schema.MinItems
	}
	if schema.MaxItems != nil {
		result["maxItems"] = *schema.MaxItems
	}

	return result
}

func schemaTypeToString(t genai.Type) string {
	switch t {
	case genai.TypeString:
		return "string"
	case genai.TypeNumber:
		return "number"
	case genai.TypeInteger:
		return "integer"
	case genai.TypeBoolean:
		return "boolean"
	case genai.TypeArray:
		return "array"
	case genai.TypeObject:
		return "object"
	default:
		return "string"
	}
}

func responseFormatToOpenAI(schema *genai.Schema) *openai.ChatCompletionNewParamsResponseFormatUnion {
	if schema == nil {
		return nil
	}

	jsonSchema := schemaToMap(schema)

	format := openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
			JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:   "response",
				Schema: jsonSchema,
				Strict: openai.Bool(true),
			},
		},
	}

	return &format
}

func chatCompletionToLLMResponse(completion *openai.ChatCompletion) *model.LLMResponse {
	if completion == nil {
		return nil
	}

	resp := &model.LLMResponse{
		TurnComplete: true,
	}

	if completion.Usage.TotalTokens > 0 {
		resp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(completion.Usage.PromptTokens),
			CandidatesTokenCount: int32(completion.Usage.CompletionTokens),
			TotalTokenCount:      int32(completion.Usage.TotalTokens),
		}
	}

	if len(completion.Choices) > 0 {
		choice := completion.Choices[0]
		resp.FinishReason = convertFinishReason(string(choice.FinishReason))
		resp.Content = choiceToContent(choice)
	}

	return resp
}

func chatCompletionChunkToLLMResponse(chunk *openai.ChatCompletionChunk) *model.LLMResponse {
	if chunk == nil || len(chunk.Choices) == 0 {
		return nil
	}

	resp := &model.LLMResponse{
		Partial: true,
	}

	if chunk.Usage.TotalTokens > 0 {
		resp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(chunk.Usage.PromptTokens),
			CandidatesTokenCount: int32(chunk.Usage.CompletionTokens),
			TotalTokenCount:      int32(chunk.Usage.TotalTokens),
		}
	}

	choice := chunk.Choices[0]

	if choice.FinishReason != "" {
		resp.FinishReason = convertFinishReason(string(choice.FinishReason))
		resp.Partial = false
		resp.TurnComplete = true
	}

	resp.Content = deltaToContent(choice.Delta)

	return resp
}

func choiceToContent(choice openai.ChatCompletionChoice) *genai.Content {
	content := &genai.Content{Role: "model"}
	msg := choice.Message

	if msg.Content != "" {
		content.Parts = append(content.Parts, genai.NewPartFromText(msg.Content))
	}

	for _, tc := range msg.ToolCalls {
		content.Parts = append(content.Parts, toolCallToFunctionCallPart(tc.ID, tc.Function.Name, tc.Function.Arguments))
	}

	return content
}

func deltaToContent(delta openai.ChatCompletionChunkChoiceDelta) *genai.Content {
	content := &genai.Content{Role: "model"}

	if delta.Content != "" {
		content.Parts = append(content.Parts, genai.NewPartFromText(delta.Content))
	}

	for _, tc := range delta.ToolCalls {
		content.Parts = append(content.Parts, toolCallToFunctionCallPart(tc.ID, tc.Function.Name, tc.Function.Arguments))
	}

	return content
}

func toolCallToFunctionCallPart(id, name, arguments string) *genai.Part {
	args := make(map[string]any)
	if arguments != "" {
		_ = json.Unmarshal([]byte(arguments), &args)
	}

	return &genai.Part{
		FunctionCall: &genai.FunctionCall{
			ID:   id,
			Name: name,
			Args: args,
		},
	}
}

func convertFinishReason(reason string) genai.FinishReason {
	switch reason {
	case "stop", "tool_calls", "function_call":
		return genai.FinishReasonStop
	case "length":
		return genai.FinishReasonMaxTokens
	case "content_filter":
		return genai.FinishReasonSafety
	default:
		return genai.FinishReasonUnspecified
	}
}

type accumulatedToolCall struct {
	id        string
	name      string
	arguments string
}

// accumulateChunks combines streaming chunks with partial tool calls into a complete response.
// This is necessary because OpenAI streams tool calls in fragments.
func accumulateChunks(chunks []*openai.ChatCompletionChunk) *model.LLMResponse {
	if len(chunks) == 0 {
		return nil
	}

	resp := &model.LLMResponse{
		Content:      &genai.Content{Role: "model"},
		TurnComplete: true,
	}

	var textContent string
	toolCalls := make(map[int]*accumulatedToolCall)

	for _, chunk := range chunks {
		if chunk == nil || len(chunk.Choices) == 0 {
			continue
		}

		if chunk.Usage.TotalTokens > 0 {
			resp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     int32(chunk.Usage.PromptTokens),
				CandidatesTokenCount: int32(chunk.Usage.CompletionTokens),
				TotalTokenCount:      int32(chunk.Usage.TotalTokens),
			}
		}

		choice := chunk.Choices[0]

		if choice.FinishReason != "" {
			resp.FinishReason = convertFinishReason(string(choice.FinishReason))
		}

		textContent += choice.Delta.Content

		for _, tc := range choice.Delta.ToolCalls {
			idx := int(tc.Index)
			if _, ok := toolCalls[idx]; !ok {
				toolCalls[idx] = &accumulatedToolCall{}
			}
			acc := toolCalls[idx]
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			acc.arguments += tc.Function.Arguments
		}
	}

	if textContent != "" {
		resp.Content.Parts = append(resp.Content.Parts, genai.NewPartFromText(textContent))
	}

	for i := 0; i < len(toolCalls); i++ {
		if tc, ok := toolCalls[i]; ok {
			resp.Content.Parts = append(resp.Content.Parts, toolCallToFunctionCallPart(tc.id, tc.name, tc.arguments))
		}
	}

	return resp
}
