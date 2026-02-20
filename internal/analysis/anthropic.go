package analysis

import (
	"context"
	"fmt"

	anthropic "github.com/liushuangls/go-anthropic/v2"
)

// AnthropicClient implements LLMClient using the Anthropic Claude API.
type AnthropicClient struct {
	client *anthropic.Client
	model  string
}

// NewAnthropicClient creates a new Anthropic Claude client.
func NewAnthropicClient(apiKey, model string) *AnthropicClient {
	client := anthropic.NewClient(apiKey)
	return &AnthropicClient{
		client: client,
		model:  model,
	}
}

// Complete sends a completion request to the Anthropic Claude API.
func (c *AnthropicClient) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	temperature := req.Temperature
	if temperature <= 0 {
		temperature = 0.3
	}
	temperatureF32 := float32(temperature)

	messages := []anthropic.Message{
		anthropic.NewUserTextMessage(req.UserPrompt),
	}

	apiReq := anthropic.MessagesRequest{
		Model:       anthropic.Model(c.model),
		MaxTokens:   maxTokens,
		Temperature: &temperatureF32,
		Messages:    messages,
	}

	if req.SystemPrompt != "" {
		apiReq.MultiSystem = []anthropic.MessageSystemPart{
			anthropic.NewSystemMessagePart(req.SystemPrompt),
		}
	}

	resp, err := c.client.CreateMessages(ctx, apiReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic API error: %w", err)
	}

	content := resp.GetFirstContentText()

	tokensUsed := resp.Usage.InputTokens + resp.Usage.OutputTokens

	return &CompletionResponse{
		Content:    content,
		Model:      string(resp.Model),
		TokensUsed: tokensUsed,
	}, nil
}
