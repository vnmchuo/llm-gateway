package gemini

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

type GeminiProvider struct {
	apiKey  string
	baseURL string
}

type geminiRequest struct {
	Contents         []geminiContent  `json:"contents"`
	GenerationConfig generationConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type generationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate   `json:"candidates"`
	UsageMetadata geminiUsageMetadata `json:"usageMetadata"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
}

func New(apiKey string) provider.Provider {
	return &GeminiProvider{
		apiKey:  apiKey,
		baseURL: "https://generativelanguage.googleapis.com",
	}
}

func (p *GeminiProvider) Complete(ctx context.Context, req *provider.Request) (*provider.Response, error) {
	geminiReq := p.mapRequest(req)
	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", req.Model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var geminiResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, err
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini api returned no candidates")
	}

	return &provider.Response{
		Content:      geminiResp.Candidates[0].Content.Parts[0].Text,
		InputTokens:  geminiResp.UsageMetadata.PromptTokenCount,
		OutputTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
		Model:        req.Model,
		Provider:     p.Name(),
	}, nil
}

func (p *GeminiProvider) mapRequest(req *provider.Request) geminiRequest {
	contents := make([]geminiContent, len(req.Messages))
	for i, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents[i] = geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: m.Content}},
		}
	}

	return geminiRequest{
		Contents: contents,
		GenerationConfig: generationConfig{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
		},
	}
}

func (p *GeminiProvider) CompleteStream(ctx context.Context, req *provider.Request) (<-chan *provider.Chunk, error) {
	geminiReq := p.mapRequest(req)
	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?key=%s&alt=sse", p.baseURL, req.Model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

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
			case ch <- &provider.Chunk{Err: fmt.Errorf("gemini api error (status %d): %s", resp.StatusCode, string(respBody))}:
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
			var geminiResp geminiResponse
			if err := json.Unmarshal([]byte(data), &geminiResp); err != nil {
				select {
				case ch <- &provider.Chunk{Err: err}:
				case <-ctx.Done():
				}
				return
			}

			if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
				text := geminiResp.Candidates[0].Content.Parts[0].Text
				if text != "" {
					select {
					case ch <- &provider.Chunk{Delta: text}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch, nil
}

func (p *GeminiProvider) Name() string {
	return "gemini"
}

func (p *GeminiProvider) CostPerInputToken() float64 {
	return 0.000000125
}

func (p *GeminiProvider) CostPerOutputToken() float64 {
	return 0.000000375
}

func (p *GeminiProvider) SupportedModels() []string {
	return []string{"gemini-1.5-pro", "gemini-1.5-flash", "gemini-2.0-flash"}
}
