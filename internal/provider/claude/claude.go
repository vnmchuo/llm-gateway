package claude

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

type ClaudeProvider struct {
	apiKey  string
	baseURL string
}

type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"`
	Messages  []claudeMessage `json:"messages"`
	Stream    bool            `json:"stream,omitempty"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResponse struct {
	ID      string          `json:"id"`
	Content []claudeContent `json:"content"`
	Model   string          `json:"model"`
	Usage   claudeUsage     `json:"usage"`
}

type claudeContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type claudeStreamDelta struct {
	Type  string        `json:"type"`
	Delta claudeDelta   `json:"delta,omitempty"`
	Error *claudeError `json:"error,omitempty"`
}

type claudeDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type claudeError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func New(apiKey string) provider.Provider {
	return &ClaudeProvider{
		apiKey:  apiKey,
		baseURL: "https://api.anthropic.com/v1",
	}
}

func (p *ClaudeProvider) Complete(ctx context.Context, req *provider.Request) (*provider.Response, error) {
	claudeReq := p.mapRequest(req)
	body, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/messages", p.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("claude api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var claudeResp claudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&claudeResp); err != nil {
		return nil, err
	}

	if len(claudeResp.Content) == 0 {
		return nil, fmt.Errorf("claude api returned no content")
	}

	return &provider.Response{
		ID:           claudeResp.ID,
		Content:      claudeResp.Content[0].Text,
		InputTokens:  claudeResp.Usage.InputTokens,
		OutputTokens: claudeResp.Usage.OutputTokens,
		Model:        claudeResp.Model,
		Provider:     p.Name(),
	}, nil
}

func (p *ClaudeProvider) mapRequest(req *provider.Request) claudeRequest {
	var system string
	var messages []claudeMessage

	for _, m := range req.Messages {
		if m.Role == "system" {
			system = m.Content
			continue
		}
		role := m.Role
		if role == "assistant" {
			role = "assistant"
		} else {
			role = "user"
		}
		messages = append(messages, claudeMessage{
			Role:    role,
			Content: m.Content,
		})
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	return claudeRequest{
		Model:     req.Model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  messages,
		Stream:    req.Stream,
	}
}

func (p *ClaudeProvider) CompleteStream(ctx context.Context, req *provider.Request) (<-chan *provider.Chunk, error) {
	claudeReq := p.mapRequest(req)
	claudeReq.Stream = true
	body, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/messages", p.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

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
			case ch <- &provider.Chunk{Err: fmt.Errorf("claude api error (status %d): %s", resp.StatusCode, string(respBody))}:
			case <-ctx.Done():
			}
			return
		}

		reader := bufio.NewReader(resp.Body)
		var currentEvent string

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
			if line == "" {
				continue
			}

			if strings.HasPrefix(line, "event: ") {
				currentEvent = strings.TrimPrefix(line, "event: ")
				continue
			}

			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")

				switch currentEvent {
				case "content_block_delta":
					var delta claudeStreamDelta
					if err := json.Unmarshal([]byte(data), &delta); err != nil {
						continue
					}
					if delta.Delta.Type == "text_delta" && delta.Delta.Text != "" {
						select {
						case ch <- &provider.Chunk{Delta: delta.Delta.Text}:
						case <-ctx.Done():
							return
						}
					}
				case "message_stop":
					select {
					case ch <- &provider.Chunk{Done: true}:
					case <-ctx.Done():
					}
					return
				case "error":
					var delta claudeStreamDelta
					if err := json.Unmarshal([]byte(data), &delta); err == nil && delta.Error != nil {
						select {
						case ch <- &provider.Chunk{Err: fmt.Errorf("claude stream error: %s", delta.Error.Message)}:
						case <-ctx.Done():
						}
						return
					}
				}
			}
		}
	}()

	return ch, nil
}

func (p *ClaudeProvider) Name() string {
	return "claude"
}

func (p *ClaudeProvider) CostPerInputToken() float64 {
	return 0.0000008
}

func (p *ClaudeProvider) CostPerOutputToken() float64 {
	return 0.000004
}

func (p *ClaudeProvider) SupportedModels() []string {
	return []string{
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
	}
}
