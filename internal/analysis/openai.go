package analysis

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

// OpenAIClient implements LLMClient using the OpenAI API.
type OpenAIClient struct {
	client *openai.Client
	model  string
}

// NewOpenAIClient creates a new OpenAI client.
func NewOpenAIClient(apiKey, model string) *OpenAIClient {
	client := openai.NewClient(apiKey)
	return &OpenAIClient{
		client: client,
		model:  model,
	}
}

// Complete sends a completion request to the OpenAI API.
func (c *OpenAIClient) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	temperature := req.Temperature
	if temperature <= 0 {
		temperature = 0.3
	}
	temperatureF32 := float32(temperature)

	messages := []openai.ChatCompletionMessage{}

	if req.SystemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.SystemPrompt,
		})
	}

	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: req.UserPrompt,
	})

	apiReq := openai.ChatCompletionRequest{
		Model:       c.model,
		MaxTokens:   maxTokens,
		Temperature: temperatureF32,
		Messages:    messages,
	}

	resp, err := c.client.CreateChatCompletion(ctx, apiReq)
	if err != nil {
		return nil, fmt.Errorf("openai API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai API returned no choices")
	}

	tokensUsed := resp.Usage.TotalTokens

	return &CompletionResponse{
		Content:    resp.Choices[0].Message.Content,
		Model:      resp.Model,
		TokensUsed: tokensUsed,
	}, nil
}
