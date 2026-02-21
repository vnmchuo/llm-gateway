package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vnmchuo/llm-gateway/internal/auth"
	"github.com/vnmchuo/llm-gateway/internal/billing"
	"github.com/vnmchuo/llm-gateway/internal/provider"
	"github.com/vnmchuo/llm-gateway/pkg/ratelimit"
	extratelimit "github.com/vnmchuo/ratelimiter"
	"go.opentelemetry.io/otel/trace/noop"
)

// Mock Billing Store
type mockBillingStore struct {
	logUsageFunc         func(ctx context.Context, log *billing.UsageLog) error
	getUsageByTenantFunc func(ctx context.Context, tenantID string, from, to time.Time) ([]*billing.UsageLog, error)
	getTotalCostFunc     func(ctx context.Context, tenantID string, from, to time.Time) (float64, error)
}

func (m *mockBillingStore) LogUsage(ctx context.Context, log *billing.UsageLog) error {
	if m.logUsageFunc != nil {
		return m.logUsageFunc(ctx, log)
	}
	return nil
}

func (m *mockBillingStore) GetUsageByTenant(ctx context.Context, tenantID string, from, to time.Time) ([]*billing.UsageLog, error) {
	if m.getUsageByTenantFunc != nil {
		return m.getUsageByTenantFunc(ctx, tenantID, from, to)
	}
	return nil, nil
}

func (m *mockBillingStore) GetTotalCostByTenant(ctx context.Context, tenantID string, from, to time.Time) (float64, error) {
	if m.getTotalCostFunc != nil {
		return m.getTotalCostFunc(ctx, tenantID, from, to)
	}
	return 0, nil
}

// Mock Limiter Store
type mockLimiterStore struct {
	allowed bool
	err     error
}

func (m *mockLimiterStore) AllowN(ctx context.Context, key string, n int) (*extratelimit.Result, error) {
	return &extratelimit.Result{Allowed: m.allowed}, m.err
}

func (m *mockLimiterStore) Allow(ctx context.Context, key string) (*extratelimit.Result, error) {
	return &extratelimit.Result{Allowed: m.allowed}, m.err
}

func (m *mockLimiterStore) Status(ctx context.Context, key string) (*extratelimit.Result, error) {
	return &extratelimit.Result{Allowed: m.allowed}, m.err
}

// Test Suite
func setupTest(providers []provider.Provider, limiterAllowed bool) (*Handler, *mockBillingStore) {
	router := NewRouter(providers)
	billingStore := &mockBillingStore{}
	limiter := ratelimit.NewTestLimiter(&mockLimiterStore{allowed: limiterAllowed})
	tracer := noop.NewTracerProvider().Tracer("test")

	return NewHandler(router, billingStore, limiter, tracer), billingStore
}

func TestHandleComplete_Unauthorized(t *testing.T) {
	h, _ := setupTest(nil, true)
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	w := httptest.NewRecorder()

	h.HandleComplete(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "unauthorized" {
		t.Errorf("Expected unauthorized error, got %v", resp["error"])
	}
}

func TestHandleComplete_InvalidBody(t *testing.T) {
	h, _ := setupTest(nil, true)
	reqBody := strings.NewReader(`{invalid json}`)
	req := httptest.NewRequest("POST", "/v1/chat/completions", reqBody)
	req = req.WithContext(auth.WithTenantID(req.Context(), "test-tenant"))
	w := httptest.NewRecorder()

	h.HandleComplete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "invalid request body" {
		t.Errorf("Expected invalid request body error, got %v", resp["error"])
	}
}

func TestHandleComplete_RateLimited(t *testing.T) {
	h, _ := setupTest(nil, false)
	reqBody, _ := json.Marshal(map[string]string{"model": "gpt-4"})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(reqBody))
	req = req.WithContext(auth.WithTenantID(req.Context(), "test-tenant"))
	w := httptest.NewRecorder()

	h.HandleComplete(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "rate limit exceeded" {
		t.Errorf("Expected rate limit exceeded error, got %v", resp["error"])
	}
	if w.Header().Get("Retry-After") != "60s" {
		t.Errorf("Expected Retry-After: 60s header, got %s", w.Header().Get("Retry-After"))
	}
}

func TestHandleComplete_ProviderUnavailable(t *testing.T) {
	// Router.Route will return error if no providers match or all are down
	h, _ := setupTest([]provider.Provider{}, true) 
	reqBody, _ := json.Marshal(map[string]string{"model": "gpt-4"})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(reqBody))
	req = req.WithContext(auth.WithTenantID(req.Context(), "test-tenant"))
	w := httptest.NewRecorder()

	h.HandleComplete(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] == "" {
		t.Errorf("Expected error message, got empty")
	}
}

func TestHandleComplete_Success(t *testing.T) {
	p := &MockProvider{
		name:            "test-provider",
		cost:            0.01,
		supportedModels: []string{"gpt-4"},
	}
	h, _ := setupTest([]provider.Provider{p}, true)

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model":      "gpt-4",
		"max_tokens": 100,
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(reqBody))
	req = req.WithContext(auth.WithTenantID(req.Context(), "test-tenant"))
	w := httptest.NewRecorder()

	h.HandleComplete(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["model"] != "gpt-4" {
		t.Errorf("Expected model gpt-4, got %v", resp["model"])
	}
	if resp["provider"] != "test-provider" {
		t.Errorf("Expected provider test-provider, got %v", resp["provider"])
	}

	choices := resp["choices"].([]interface{})
	if len(choices) != 1 {
		t.Errorf("Expected 1 choice, got %d", len(choices))
	}
	message := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	if message["content"] != "mock" {
		t.Errorf("Expected content 'mock', got %v", message["content"])
	}

	usage := resp["usage"].(map[string]interface{})
	if usage["total_tokens"] == 0 {
		t.Errorf("Expected non-zero total_tokens")
	}
}

func TestHandleCompleteStream_Unauthorized(t *testing.T) {
	h, _ := setupTest(nil, true)
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	w := httptest.NewRecorder()

	h.HandleCompleteStream(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
}

func TestHandleCompleteStream_InvalidBody(t *testing.T) {
	h, _ := setupTest(nil, true)
	reqBody := strings.NewReader(`{invalid json}`)
	req := httptest.NewRequest("POST", "/v1/chat/completions", reqBody)
	req = req.WithContext(auth.WithTenantID(req.Context(), "test-tenant"))
	w := httptest.NewRecorder()

	h.HandleCompleteStream(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestHandleCompleteStream_RateLimited(t *testing.T) {
	h, _ := setupTest(nil, false)
	reqBody, _ := json.Marshal(map[string]interface{}{"model": "gpt-4", "stream": true})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(reqBody))
	req = req.WithContext(auth.WithTenantID(req.Context(), "test-tenant"))
	w := httptest.NewRecorder()

	h.HandleCompleteStream(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", w.Code)
	}
}

func TestHandleCompleteStream_Success(t *testing.T) {
	// MockProvider.CompleteStream returns 1 chunk then close
	// Actually router.ExecuteStream wraps it.
	// Let's modify MockProvider to return two chunks for this test
	p := &MockStreamProvider{
		MockProvider: MockProvider{
			name:            "test-provider",
			supportedModels: []string{"gpt-4"},
		},
		chunks: []*provider.Chunk{
			{Delta: "hello"},
			{Delta: " world"},
			{Done: true},
		},
	}

	h, _ := setupTest([]provider.Provider{p}, true)

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model":  "gpt-4",
		"stream": true,
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(reqBody))
	req = req.WithContext(auth.WithTenantID(req.Context(), "test-tenant"))
	w := httptest.NewRecorder()

	h.HandleCompleteStream(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected text/event-stream content type, got %s", w.Header().Get("Content-Type"))
	}

	body := w.Body.String()
	if !strings.Contains(body, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"index\":0}]}") {
		t.Errorf("Body missing first chunk: %s", body)
	}
	if !strings.Contains(body, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"index\":0}]}") {
		t.Errorf("Body missing second chunk: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("Body missing DONE marker: %s", body)
	}
}

type MockStreamProvider struct {
	MockProvider
	chunks          []*provider.Chunk
}

func (m *MockStreamProvider) CompleteStream(ctx context.Context, req *provider.Request) (<-chan *provider.Chunk, error) {
	ch := make(chan *provider.Chunk)
	go func() {
		for _, c := range m.chunks {
			ch <- c
		}
		close(ch)
	}()
	return ch, nil
}

func (m *MockStreamProvider) Name() string               { return m.MockProvider.name }
func (m *MockStreamProvider) SupportedModels() []string { return m.MockProvider.supportedModels }
func (m *MockStreamProvider) CostPerInputToken() float64 { return m.MockProvider.cost }
func (m *MockStreamProvider) CostPerOutputToken() float64 { return 0 }

func TestHandleUsage_Unauthorized(t *testing.T) {
	h, _ := setupTest(nil, true)
	req := httptest.NewRequest("GET", "/v1/usage", nil)
	w := httptest.NewRecorder()

	h.HandleUsage(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
}

func TestHandleUsage_InvalidDateFormat(t *testing.T) {
	h, _ := setupTest(nil, true)
	req := httptest.NewRequest("GET", "/v1/usage?from=not-a-date", nil)
	req = req.WithContext(auth.WithTenantID(req.Context(), "test-tenant"))
	w := httptest.NewRecorder()

	h.HandleUsage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestHandleUsage_Success(t *testing.T) {
	h, b := setupTest(nil, true)
	b.getUsageByTenantFunc = func(ctx context.Context, tenantID string, from, to time.Time) ([]*billing.UsageLog, error) {
		return []*billing.UsageLog{
			{TenantID: "test-tenant", Model: "gpt-4"},
			{TenantID: "test-tenant", Model: "gpt-4"},
		}, nil
	}
	b.getTotalCostFunc = func(ctx context.Context, tenantID string, from, to time.Time) (float64, error) {
		return 0.005, nil
	}

	req := httptest.NewRequest("GET", "/v1/usage", nil)
	req = req.WithContext(auth.WithTenantID(req.Context(), "test-tenant"))
	w := httptest.NewRecorder()

	h.HandleUsage(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["total_requests"].(float64) != 2 {
		t.Errorf("Expected total_requests == 2, got %v", resp["total_requests"])
	}
	if resp["total_cost_usd"].(float64) != 0.005 {
		t.Errorf("Expected total_cost_usd == 0.005, got %v", resp["total_cost_usd"])
	}
	logs := resp["logs"].([]interface{})
	if len(logs) != 2 {
		t.Errorf("Expected 2 logs, got %d", len(logs))
	}
}

func TestHandleUsage_DefaultDates(t *testing.T) {
	h, _ := setupTest(nil, true)
	req := httptest.NewRequest("GET", "/v1/usage", nil)
	req = req.WithContext(auth.WithTenantID(req.Context(), "test-tenant"))
	w := httptest.NewRecorder()

	h.HandleUsage(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["from"] == "" || resp["to"] == "" {
		t.Errorf("Expected from/to dates in response")
	}
}
