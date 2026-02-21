package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vnmchuo/llm-gateway/internal/provider"
)

func TestComplete_Mock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: geminiContent{
						Parts: []geminiPart{{Text: "Hello from mock!"}},
					},
				},
			},
			UsageMetadata: geminiUsageMetadata{
				PromptTokenCount:     10,
				CandidatesTokenCount: 20,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &GeminiProvider{
		apiKey:  "test-key",
		baseURL: server.URL,
	}

	req := &provider.Request{
		Model: "gemini-pro",
		Messages: []provider.Message{
			{Role: "user", Content: "hi"},
		},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if resp.Content != "Hello from mock!" {
		t.Errorf("Expected 'Hello from mock!', got %s", resp.Content)
	}
	if resp.InputTokens != 10 {
		t.Errorf("Expected 10 input tokens, got %d", resp.InputTokens)
	}
	if resp.OutputTokens != 20 {
		t.Errorf("Expected 20 output tokens, got %d", resp.OutputTokens)
	}
}

func TestCompleteStream_Mock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		
		chunks := []string{"Hello", " world", "!"}
		for _, chunk := range chunks {
			resp := geminiResponse{
				Candidates: []geminiCandidate{
					{
						Content: geminiContent{
							Parts: []geminiPart{{Text: chunk}},
						},
					},
				},
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(w, "data: %s\n\n", string(data))
		}
	}))
	defer server.Close()

	p := &GeminiProvider{
		apiKey:  "test-key",
		baseURL: server.URL,
	}

	req := &provider.Request{
		Model: "gemini-pro",
		Messages: []provider.Message{
			{Role: "user", Content: "hi"},
		},
	}

	ch, err := p.CompleteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("CompleteStream failed: %v", err)
	}

	var content string
	var done bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("Received error from chunk: %v", chunk.Err)
		}
		if chunk.Done {
			done = true
			continue
		}
		content += chunk.Delta
	}

	if !done {
		t.Error("Expected stream to be done")
	}
	if content != "Hello world!" {
		t.Errorf("Expected 'Hello world!', got %s", content)
	}
}
