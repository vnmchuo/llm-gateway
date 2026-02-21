package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vnmchuo/llm-gateway/internal/provider"
)

func TestComplete_Mock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := claudeResponse{
			ID: "msg_123",
			Content: []claudeContent{
				{Type: "text", Text: "Hello from Claude mock!"},
			},
			Usage: claudeUsage{
				InputTokens:  10,
				OutputTokens: 20,
			},
			Model: "claude-3-5-sonnet-20241022",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &ClaudeProvider{
		apiKey:  "test-key",
		baseURL: server.URL,
	}

	req := &provider.Request{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []provider.Message{
			{Role: "user", Content: "hi"},
		},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if resp.Content != "Hello from Claude mock!" {
		t.Errorf("Expected 'Hello from Claude mock!', got %s", resp.Content)
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
		
		fmt.Fprintf(w, "event: content_block_delta\n")
		data1, _ := json.Marshal(claudeStreamDelta{
			Type: "content_block_delta",
			Delta: claudeDelta{Type: "text_delta", Text: "Hello"},
		})
		fmt.Fprintf(w, "data: %s\n\n", string(data1))

		fmt.Fprintf(w, "event: content_block_delta\n")
		data2, _ := json.Marshal(claudeStreamDelta{
			Type: "content_block_delta",
			Delta: claudeDelta{Type: "text_delta", Text: " world!"},
		})
		fmt.Fprintf(w, "data: %s\n\n", string(data2))

		fmt.Fprintf(w, "event: message_stop\n")
		fmt.Fprintf(w, "data: {\"type\": \"message_stop\"}\n\n")
	}))
	defer server.Close()

	p := &ClaudeProvider{
		apiKey:  "test-key",
		baseURL: server.URL,
	}

	req := &provider.Request{
		Model: "claude-3-5-sonnet-20241022",
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

func TestName(t *testing.T) {
	p := New("key")
	if p.Name() != "claude" {
		t.Errorf("Expected 'claude', got %s", p.Name())
	}
}

func TestSupportedModels(t *testing.T) {
	p := New("key")
	models := p.SupportedModels()
	found := false
	for _, m := range models {
		if m == "claude-3-5-haiku-20241022" {
			found = true
			break
		}
	}
	if !found {
		t.Error("claude-3-5-haiku-20241022 should be in supported models")
	}
}

func TestSystemMessageExtraction(t *testing.T) {
	var capturedReq claudeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedReq)
		
		resp := claudeResponse{
			ID: "msg_123",
			Content: []claudeContent{{Type: "text", Text: "ok"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &ClaudeProvider{
		apiKey:  "test-key",
		baseURL: server.URL,
	}

	req := &provider.Request{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []provider.Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "hi"},
		},
	}

	_, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if capturedReq.System != "You are a helpful assistant." {
		t.Errorf("Expected system message to be extracted, got %s", capturedReq.System)
	}
	if len(capturedReq.Messages) != 1 {
		t.Errorf("Expected 1 message after system extraction, got %d", len(capturedReq.Messages))
	}
	if capturedReq.Messages[0].Role != "user" {
		t.Errorf("Expected first message role to be 'user', got %s", capturedReq.Messages[0].Role)
	}
}
