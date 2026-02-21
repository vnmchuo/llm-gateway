package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/vnmchuo/llm-gateway/internal/provider"
)

type OpenAIProvider struct {
	apiKey  string
	baseURL string
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	ID      string         `json:"id"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
	Model   string         `json:"model"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
	Delta   openAIDelta   `json:"delta"`
}

type openAIDelta struct {
	Content string `json:"content"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func New(apiKey string) provider.Provider {
	return &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: "https://api.openai.com/v1",
	}
}

func (p *OpenAIProvider) Complete(ctx context.Context, req *provider.Request) (*provider.Response, error) {
	openAIReq := p.mapRequest(req)
	body, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/chat/completions", p.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var openAIResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, err
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("openai api returned no choices")
	}

	return &provider.Response{
		ID:           openAIResp.ID,
		Content:      openAIResp.Choices[0].Message.Content,
		InputTokens:  openAIResp.Usage.PromptTokens,
		OutputTokens: openAIResp.Usage.CompletionTokens,
		Model:        openAIResp.Model,
		Provider:     p.Name(),
	}, nil
}

func (p *OpenAIProvider) mapRequest(req *provider.Request) openAIRequest {
	messages := make([]openAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = openAIMessage{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	return openAIRequest{
		Model:       req.Model,
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      req.Stream,
	}
}

func (p *OpenAIProvider) CompleteStream(ctx context.Context, req *provider.Request) (<-chan *provider.Chunk, error) {
	openAIReq := p.mapRequest(req)
	openAIReq.Stream = true
	body, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/chat/completions", p.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

	ch := make(chan *provider.Chunk)

	go func() {
		defer close(ch)

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			select {
			case ch <- &provider.Chunk{Err: err}:
			case <-ctx.Done():
			}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			select {
			case ch <- &provider.Chunk{Err: fmt.Errorf("openai api error (status %d): %s", resp.StatusCode, string(respBody))}:
			case <-ctx.Done():
			}
			return
		}

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					select {
					case ch <- &provider.Chunk{Done: true}:
					case <-ctx.Done():
					}
					return
				}
				select {
				case ch <- &provider.Chunk{Err: err}:
				case <-ctx.Done():
				}
				return
			}

			line = strings.TrimSpace(line)
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				select {
				case ch <- &provider.Chunk{Done: true}:
				case <-ctx.Done():
				}
				return
			}

			var openAIResp openAIResponse
			if err := json.Unmarshal([]byte(data), &openAIResp); err != nil {
				select {
				case ch <- &provider.Chunk{Err: err}:
				case <-ctx.Done():
				}
				return
			}

			if len(openAIResp.Choices) > 0 {
				content := openAIResp.Choices[0].Delta.Content
				if content != "" {
					select {
					case ch <- &provider.Chunk{Delta: content}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch, nil
}

func (p *OpenAIProvider) Name() string {
	return "openai"
}

func (p *OpenAIProvider) CostPerInputToken() float64 {
	return 0.00000015
}

func (p *OpenAIProvider) CostPerOutputToken() float64 {
	return 0.00000060
}

func (p *OpenAIProvider) SupportedModels() []string {
	return []string{"gpt-4o", "gpt-4o-mini", "gpt-4", "gpt-3.5-turbo"}
}
