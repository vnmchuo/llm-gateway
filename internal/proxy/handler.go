package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vnmchuo/llm-gateway/internal/auth"
	"github.com/vnmchuo/llm-gateway/internal/billing"
	"github.com/vnmchuo/llm-gateway/internal/provider"
	"github.com/vnmchuo/llm-gateway/pkg/ratelimit"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Handler struct {
	router  *Router
	billing billing.Store
	limiter *ratelimit.Limiter
	tracer  trace.Tracer
}

func NewHandler(router *Router, billing billing.Store, limiter *ratelimit.Limiter, tracer trace.Tracer) *Handler {
	return &Handler{
		router:  router,
		billing: billing,
		limiter: limiter,
		tracer:  tracer,
	}
}

func (h *Handler) HandleComplete(w http.ResponseWriter, r *http.Request) {
	tenantID, requestID, req, selectedProvider, err := h.prepare(w, r)
	if err != nil {
		return
	}

	response, err := h.router.Execute(r.Context(), req, selectedProvider)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Step 9: Log usage asynchronously
	go func() {
		_ = h.billing.LogUsage(context.Background(), &billing.UsageLog{
			TenantID:     tenantID,
			RequestID:    requestID,
			Provider:     response.Provider,
			Model:        response.Model,
			InputTokens:  response.InputTokens,
			OutputTokens: response.OutputTokens,
			CostUSD:      float64(response.InputTokens)*selectedProvider.CostPerInputToken() + float64(response.OutputTokens)*selectedProvider.CostPerOutputToken(),
			LatencyMs:    response.LatencyMs,
		})
	}()

	// Step 10: Return 200 with OpenAI-compatible JSON
	respID := response.ID
	if respID == "" {
		respID = uuid.New().String()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       respID,
		"object":   "chat.completion",
		"model":    response.Model,
		"provider": response.Provider,
		"choices": []interface{}{
			map[string]interface{}{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": response.Content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     response.InputTokens,
			"completion_tokens": response.OutputTokens,
			"total_tokens":      response.InputTokens + response.OutputTokens,
		},
	})
}

func (h *Handler) HandleCompleteStream(w http.ResponseWriter, r *http.Request) {
	tenantID, requestID, req, selectedProvider, err := h.prepare(w, r)
	if err != nil {
		return
	}

	ch, err := h.router.ExecuteStream(r.Context(), req, selectedProvider)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	for chunk := range ch {
		if chunk.Err != nil {
			fmt.Fprintf(w, "event: error\ndata: {\"error\": \"%s\"}\n\n", chunk.Err.Error())
			flusher.Flush()
			break
		}

		if chunk.Done {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			break
		}

		escaped := strings.ReplaceAll(chunk.Delta, `"`, `\"`)
		escaped = strings.ReplaceAll(escaped, "\n", `\n`)
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"%s\"},\"index\":0}]}\n\n", escaped)
		flusher.Flush()
	}

	go func() {
		_ = h.billing.LogUsage(context.Background(), &billing.UsageLog{
			TenantID:  tenantID,
			RequestID: requestID,
			Provider:  selectedProvider.Name(),
			Model:     req.Model,
		})
	}()
}

func (h *Handler) prepare(w http.ResponseWriter, r *http.Request) (string, string, *provider.Request, provider.Provider, error) {
	ctx := r.Context()
	tenantID := auth.GetTenantID(ctx)
	if tenantID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return "", "", nil, nil, fmt.Errorf("unauthorized")
	}

	requestID := auth.GetRequestID(ctx)
	if requestID == "" {
		requestID = uuid.New().String()
	}

	var req provider.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return "", "", nil, nil, err
	}

	_, span := h.tracer.Start(ctx, "proxy.complete")
	defer span.End()
	span.SetAttributes(
		attribute.String("tenant_id", tenantID),
		attribute.String("request_id", requestID),
		attribute.String("model", req.Model),
	)

	estimatedTokens := req.MaxTokens
	if estimatedTokens <= 0 {
		estimatedTokens = 1000
	}

	allowed, err := h.limiter.Allow(ctx, tenantID, estimatedTokens)
	if err != nil || !allowed {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "60s")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{
			"error":       "rate limit exceeded",
			"retry_after": "60s",
		})
		return "", "", nil, nil, fmt.Errorf("rate limit exceeded")
	}

	selectedProvider, err := h.router.Route(ctx, &req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return "", "", nil, nil, err
	}

	return tenantID, requestID, &req, selectedProvider, nil
}

func (h *Handler) HandleUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := auth.GetTenantID(ctx)
	if tenantID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	// Parse query parameters
	now := time.Now()
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	from := now.AddDate(0, 0, -30) // Default: last 30 days
	to := now

	if fromStr != "" {
		var err error
		from, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid 'from' date format (use RFC3339)"})
			return
		}
	}

	if toStr != "" {
		var err error
		to, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid 'to' date format (use RFC3339)"})
			return
		}
	}

	logs, err := h.billing.GetUsageByTenant(ctx, tenantID, from, to)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	totalCost, err := h.billing.GetTotalCostByTenant(ctx, tenantID, from, to)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tenant_id":      tenantID,
		"total_requests": len(logs),
		"total_cost_usd": totalCost,
		"logs":           logs,
		"from":           from,
		"to":             to,
	})
}
