package openai

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
		resp := openAIResponse{
			ID: "test-id",
			Choices: []openAIChoice{
				{
					Message: openAIMessage{Role: "assistant", Content: "Hello from OpenAI mock!"},
				},
			},
			Usage: openAIUsage{
				PromptTokens:     15,
				CompletionTokens: 25,
			},
			Model: "gpt-4o-mini",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &OpenAIProvider{
		apiKey:  "test-key",
		baseURL: server.URL,
	}

	req := &provider.Request{
		Model: "gpt-4o-mini",
		Messages: []provider.Message{
			{Role: "user", Content: "hi"},
		},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if resp.Content != "Hello from OpenAI mock!" {
		t.Errorf("Expected 'Hello from OpenAI mock!', got %s", resp.Content)
	}
	if resp.InputTokens != 15 {
		t.Errorf("Expected 15 input tokens, got %d", resp.InputTokens)
	}
	if resp.OutputTokens != 25 {
		t.Errorf("Expected 25 output tokens, got %d", resp.OutputTokens)
	}
}

func TestCompleteStream_Mock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		
		chunks := []string{"Hello", " from", " OpenAI", "!"}
		for _, chunk := range chunks {
			resp := openAIResponse{
				Choices: []openAIChoice{
					{
						Delta: openAIDelta{Content: chunk},
					},
				},
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(w, "data: %s\n\n", string(data))
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := &OpenAIProvider{
		apiKey:  "test-key",
		baseURL: server.URL,
	}

	req := &provider.Request{
		Model: "gpt-4o-mini",
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
	if content != "Hello from OpenAI!" {
		t.Errorf("Expected 'Hello from OpenAI!', got %s", content)
	}
}

func TestName(t *testing.T) {
	p := New("key")
	if p.Name() != "openai" {
		t.Errorf("Expected 'openai', got %s", p.Name())
	}
}

func TestSupportedModels(t *testing.T) {
	p := New("key")
	models := p.SupportedModels()
	found := false
	for _, m := range models {
		if m == "gpt-4o-mini" {
			found = true
			break
		}
	}
	if !found {
		t.Error("gpt-4o-mini should be in supported models")
	}
}
