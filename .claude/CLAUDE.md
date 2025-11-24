# EZThrottle Go SDK - AI Assistant Guide

## The Vision

EZThrottle is a distributed webhook-based job execution platform. The Go SDK provides **two workflow types** for different optimization strategies:

1. **FrugalWorkflow** - Cost optimization (client executes, only queue on failure)
2. **PerformanceWorkflow** - Speed optimization (distributed execution with racing/webhooks)

---

## Workflow Types Explained

### FrugalWorkflow (Cost Optimization)

**Philosophy:** Client executes HTTP calls locally, only forwards to EZThrottle on specific error codes.

**Why "Frugal":**
- User doesn't pay for successful requests (client executes them)
- No webhook delivery cost (client gets response immediately)
- Only pay EZThrottle when APIs fail/rate-limit

**Use Cases:**
- High success rate APIs (95%+ succeed)
- Cost-sensitive workflows
- Simple HTTP calls that rarely fail
- Development/testing environments

**Example:**
```go
package main

import (
	"github.com/your-org/ezthrottle-go"
)

func main() {
	client := ezthrottle.NewClient("your_api_key")

	// Execute locally first, forward to EZThrottle only on 429/500
	result, err := ezthrottle.NewStep(client).
		URL("https://api.openai.com/v1/chat/completions").
		Method("POST").
		Header("Authorization", "Bearer sk-...").
		Body(`{"model": "gpt-4", "messages": [...]}`).
		Type(ezthrottle.StepTypeFrugal).
		FallbackOnError([]int{429, 500, 502, 503}).
		Execute(context.Background())

	if err != nil {
		log.Fatal(err)
	}
}
```

---

### PerformanceWorkflow (Speed Optimization)

**Philosophy:** Submit job to EZThrottle immediately for distributed execution with multi-region racing and webhooks.

**Why "Performance":**
- Multi-region racing (submit to 3 regions, fastest wins)
- Distributed execution (no client bottleneck)
- Webhook delivery (fire-and-forget)
- Fallback chains handled server-side

**Example:**
```go
result, err := ezthrottle.NewStep(client).
	URL("https://api.stripe.com/charges").
	Method("POST").
	Header("Authorization", "Bearer sk_live_...").
	Body(`{"amount": 1000, "currency": "usd"}`).
	Type(ezthrottle.StepTypePerformance).
	Webhooks([]ezthrottle.Webhook{
		{URL: "https://app.com/payment-complete", HasQuorumVote: true},
	}).
	Regions([]string{"iad", "lax", "ord"}).
	ExecutionMode("race").
	Execute(context.Background())
```

---

## Idempotent Key Strategies

### IdempotentStrategyHash (Default)

Backend generates deterministic hash of (url, method, body, customer_id). **Prevents duplicates.**

**Use when:**
- Payment processing (don't charge twice!)
- Critical operations (create user, send notification)
- You want automatic deduplication

**Example:**
```go
// Same request twice = same job_id (deduplicated)
_, err := ezthrottle.NewStep(client).
	URL("https://api.stripe.com/charges").
	Body(`{"amount": 1000, "currency": "usd"}`).
	IdempotentStrategy(ezthrottle.IdempotentStrategyHash).
	Execute(ctx)
```

### IdempotentStrategyUnique

SDK generates unique UUID per request. **Allows duplicates.**

**Use when:**
- Polling endpoints (same URL, different data each time)
- Webhooks (want to send every time)
- Scheduled jobs (run every minute/hour)

**Example:**
```go
// Poll API every minute - each request gets unique UUID
ticker := time.NewTicker(1 * time.Minute)
defer ticker.Stop()

for range ticker.C {
	_, err := ezthrottle.NewStep(client).
		URL("https://api.example.com/status").
		IdempotentStrategy(ezthrottle.IdempotentStrategyUnique).
		Execute(ctx)
}
```

---

## Chain Mixing: The Killer Feature

**Every step can chain to EITHER Frugal or Performance workflows.**

```go
// Save money on API call, pay for fast webhook delivery
_, err := ezthrottle.NewStep(client).
	URL("https://api.openai.com/v1/chat/completions").
	Type(ezthrottle.StepTypeFrugal).
	FallbackOnError([]int{429}).
	OnSuccess(
		ezthrottle.NewStep(client).
			URL("https://api.sendgrid.com/send").
			Type(ezthrottle.StepTypePerformance).
			Webhooks([]ezthrottle.Webhook{...}).
			Regions([]string{"iad", "lax", "ord"}),
	).
	Execute(ctx)
```

---

## EZThrottle API Specification

### POST /api/v1/jobs

**Required Fields:**
- `url` (string) - Target URL to request
- `method` (string) - HTTP method (GET, POST, PUT, DELETE, etc.)

**Optional Fields (Webhooks):**
```go
type Webhook struct {
	URL           string   `json:"url"`
	Regions       []string `json:"regions,omitempty"`
	HasQuorumVote bool     `json:"has_quorum_vote,omitempty"`
}

type JobOptions struct {
	Webhooks      []Webhook `json:"webhooks,omitempty"`
	WebhookQuorum int       `json:"webhook_quorum,omitempty"`
}
```

**Optional Fields (Multi-Region):**
```go
type JobOptions struct {
	Regions       []string `json:"regions,omitempty"`
	RegionPolicy  string   `json:"region_policy,omitempty"`  // "fallback" or "strict"
	ExecutionMode string   `json:"execution_mode,omitempty"` // "race" or "fanout"
}
```

**Optional Fields (Fallbacks):**
```go
type FallbackTrigger struct {
	Type      string `json:"type"`              // "on_error" or "on_timeout"
	Codes     []int  `json:"codes,omitempty"`   // For on_error
	TimeoutMs int    `json:"timeout_ms,omitempty"` // For on_timeout
}

type JobOptions struct {
	FallbackJob *JobOptions      `json:"fallback_job,omitempty"`
	Trigger     *FallbackTrigger `json:"trigger,omitempty"`
}
```

**Optional Fields (Workflows):**
```go
type JobOptions struct {
	OnSuccess           *JobOptions `json:"on_success,omitempty"`
	OnFailure           *JobOptions `json:"on_failure,omitempty"`
	OnFailureTimeoutMs  int         `json:"on_failure_timeout_ms,omitempty"`
}
```

---

## Implementation Plan

### Phase 1: Core `SubmitJob()` with ALL API Features (2-3 hours)

**Goal:** Create `SubmitJob()` method supporting all EZThrottle API features

**Files to create:**
- `client.go` - EZThrottle client
- `types.go` - Type definitions

**Type definitions:**
```go
package ezthrottle

import (
	"context"
	"net/http"
)

type Client struct {
	apiKey        string
	ezthrottleURL string
	httpClient    *http.Client
}

type Webhook struct {
	URL           string   `json:"url"`
	Regions       []string `json:"regions,omitempty"`
	HasQuorumVote bool     `json:"has_quorum_vote,omitempty"`
}

type RetryPolicy struct {
	MaxRetries   int   `json:"max_retries,omitempty"`
	MaxReroutes  int   `json:"max_reroutes,omitempty"`
	RetryCodes   []int `json:"retry_codes,omitempty"`
	RerouteCodes []int `json:"reroute_codes,omitempty"`
}

type FallbackTrigger struct {
	Type      string `json:"type"`
	Codes     []int  `json:"codes,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type JobOptions struct {
	URL                string            `json:"url"`
	Method             string            `json:"method"`
	Headers            map[string]string `json:"headers,omitempty"`
	Body               string            `json:"body,omitempty"`
	Webhooks           []Webhook         `json:"webhooks,omitempty"`
	WebhookQuorum      int               `json:"webhook_quorum,omitempty"`
	Regions            []string          `json:"regions,omitempty"`
	RegionPolicy       string            `json:"region_policy,omitempty"`
	ExecutionMode      string            `json:"execution_mode,omitempty"`
	RetryPolicy        *RetryPolicy      `json:"retry_policy,omitempty"`
	FallbackJob        *JobOptions       `json:"fallback_job,omitempty"`
	OnSuccess          *JobOptions       `json:"on_success,omitempty"`
	OnFailure          *JobOptions       `json:"on_failure,omitempty"`
	OnFailureTimeoutMs int               `json:"on_failure_timeout_ms,omitempty"`
	Metadata           map[string]any    `json:"metadata,omitempty"`
	IdempotentKey      string            `json:"idempotent_key,omitempty"`
	RetryAt            int64             `json:"retry_at,omitempty"`
}

type JobResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

func NewClient(apiKey string, opts ...ClientOption) *Client {
	client := &Client{
		apiKey:        apiKey,
		ezthrottleURL: "https://api.trackttags.com/api/v1/jobs",
		httpClient:    &http.Client{},
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

type ClientOption func(*Client)

func WithEZThrottleURL(url string) ClientOption {
	return func(c *Client) {
		c.ezthrottleURL = url
	}
}

func (c *Client) SubmitJob(ctx context.Context, opts *JobOptions) (*JobResponse, error) {
	// Build JSON payload
	payload, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job options: %w", err)
	}

	// POST to TracktTags proxy → EZThrottle
	req, err := http.NewRequestWithContext(ctx, "POST", c.ezthrottleURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var result JobResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
```

**Deliverable:** Can submit jobs with ANY combination of API features

---

### Phase 2: Builder Pattern (Step + StepType) (3-4 hours)

**Goal:** Create ergonomic builder API with `Step` and `StepType`

**Files to create:**
- `step.go` - Step builder class
- `step_type.go` - StepType constants
- `idempotent_strategy.go` - IdempotentStrategy constants

**Step API:**
```go
package ezthrottle

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

type StepType string

const (
	StepTypeFrugal      StepType = "frugal"
	StepTypePerformance StepType = "performance"
)

type IdempotentStrategy string

const (
	IdempotentStrategyHash   IdempotentStrategy = "hash"
	IdempotentStrategyUnique IdempotentStrategy = "unique"
)

type Step struct {
	client             *Client
	url                string
	method             string
	headers            map[string]string
	body               string
	stepType           StepType
	webhooks           []Webhook
	webhookQuorum      int
	regions            []string
	regionPolicy       string
	executionMode      string
	retryPolicy        *RetryPolicy
	fallbackOnError    []int
	timeout            int
	idempotentStrategy IdempotentStrategy
	idempotentKey      string
	metadata           map[string]any
	onSuccess          *Step
	onFailure          *Step
	fallbacks          []*Step
	fallbackTriggers   []*FallbackTrigger
}

func NewStep(client *Client) *Step {
	return &Step{
		client:   client,
		method:   "GET",
		stepType: StepTypePerformance,
		headers:  make(map[string]string),
		metadata: make(map[string]any),
	}
}

func (s *Step) URL(url string) *Step {
	s.url = url
	return s
}

func (s *Step) Method(method string) *Step {
	s.method = method
	return s
}

func (s *Step) Header(key, value string) *Step {
	s.headers[key] = value
	return s
}

func (s *Step) Headers(headers map[string]string) *Step {
	s.headers = headers
	return s
}

func (s *Step) Body(body string) *Step {
	s.body = body
	return s
}

func (s *Step) Type(stepType StepType) *Step {
	s.stepType = stepType
	return s
}

func (s *Step) Webhooks(webhooks []Webhook) *Step {
	s.webhooks = webhooks
	return s
}

func (s *Step) WebhookQuorum(quorum int) *Step {
	s.webhookQuorum = quorum
	return s
}

func (s *Step) Regions(regions []string) *Step {
	s.regions = regions
	return s
}

func (s *Step) RegionPolicy(policy string) *Step {
	s.regionPolicy = policy
	return s
}

func (s *Step) ExecutionMode(mode string) *Step {
	s.executionMode = mode
	return s
}

func (s *Step) RetryPolicy(policy *RetryPolicy) *Step {
	s.retryPolicy = policy
	return s
}

func (s *Step) FallbackOnError(codes []int) *Step {
	s.fallbackOnError = codes
	return s
}

func (s *Step) Timeout(timeout int) *Step {
	s.timeout = timeout
	return s
}

func (s *Step) IdempotentStrategy(strategy IdempotentStrategy) *Step {
	s.idempotentStrategy = strategy
	return s
}

func (s *Step) IdempotentKey(key string) *Step {
	s.idempotentKey = key
	return s
}

func (s *Step) Metadata(metadata map[string]any) *Step {
	s.metadata = metadata
	return s
}

func (s *Step) OnSuccess(step *Step) *Step {
	s.onSuccess = step
	return s
}

func (s *Step) OnFailure(step *Step) *Step {
	s.onFailure = step
	return s
}

func (s *Step) Fallback(step *Step, trigger *FallbackTrigger) *Step {
	s.fallbacks = append(s.fallbacks, step)
	s.fallbackTriggers = append(s.fallbackTriggers, trigger)
	return s
}

func (s *Step) Execute(ctx context.Context) (*JobResponse, error) {
	if s.stepType == StepTypeFrugal {
		return s.executeFrugal(ctx)
	}
	return s.executePerformance(ctx)
}

func (s *Step) executeFrugal(ctx context.Context) (*JobResponse, error) {
	// Try locally first
	req, err := http.NewRequestWithContext(ctx, s.method, s.url, bytes.NewBufferString(s.body))
	if err != nil {
		return s.forwardToEZThrottle(ctx)
	}

	for k, v := range s.headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Network error → forward to EZThrottle
		return s.forwardToEZThrottle(ctx)
	}
	defer resp.Body.Close()

	// Success?
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Fire on_success asynchronously
		if s.onSuccess != nil {
			go s.onSuccess.Execute(context.Background())
		}

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		return &JobResponse{Status: "success"}, nil
	}

	// Error matches fallback trigger?
	for _, code := range s.fallbackOnError {
		if resp.StatusCode == code {
			return s.forwardToEZThrottle(ctx)
		}
	}

	return nil, fmt.Errorf("request failed: %d", resp.StatusCode)
}

func (s *Step) executePerformance(ctx context.Context) (*JobResponse, error) {
	return s.forwardToEZThrottle(ctx)
}

func (s *Step) forwardToEZThrottle(ctx context.Context) (*JobResponse, error) {
	opts := s.buildPayload()
	return s.client.SubmitJob(ctx, opts)
}

func (s *Step) buildPayload() *JobOptions {
	opts := &JobOptions{
		URL:           s.url,
		Method:        s.method,
		Headers:       s.headers,
		Body:          s.body,
		Webhooks:      s.webhooks,
		WebhookQuorum: s.webhookQuorum,
		Regions:       s.regions,
		RegionPolicy:  s.regionPolicy,
		ExecutionMode: s.executionMode,
		RetryPolicy:   s.retryPolicy,
		Metadata:      s.metadata,
	}

	// Generate idempotent key
	if s.idempotentKey != "" {
		opts.IdempotentKey = s.idempotentKey
	} else if s.idempotentStrategy == IdempotentStrategyUnique {
		opts.IdempotentKey = uuid.New().String()
	}

	// Build fallback chain
	if len(s.fallbacks) > 0 {
		opts.FallbackJob = s.buildFallbackChain()
	}

	// Build on_success
	if s.onSuccess != nil && s.onSuccess.stepType == StepTypePerformance {
		opts.OnSuccess = s.onSuccess.buildPayload()
	}

	// Build on_failure
	if s.onFailure != nil && s.onFailure.stepType == StepTypePerformance {
		opts.OnFailure = s.onFailure.buildPayload()
	}

	return opts
}

func (s *Step) buildFallbackChain() *JobOptions {
	if len(s.fallbacks) == 0 {
		return nil
	}

	// Build first fallback
	fallback := s.fallbacks[0]
	trigger := s.fallbackTriggers[0]

	opts := fallback.buildPayload()
	opts.FallbackJob = nil // Clear nested fallbacks for now

	// Add trigger
	// This would be set in the parent's FallbackJob field

	return opts
}
```

**Deliverable:** Can build step trees declaratively

---

### Phase 3: DFS Execution & Batching (3-4 hours)

**Goal:** Walk step tree, batch consecutive Performance steps, execute mixed workflows

**Core algorithm:** Same as Phase 2 implementation above (already included in `executeFrugal` and `buildPayload`)

**Deliverable:** Mixed workflows (Frugal → Performance → Frugal) work correctly

---

### Phase 4: Testing & Examples (2-3 hours)

**Goal:** Integration tests and real-world examples

**Example test app (net/http):**
```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/your-org/ezthrottle-go"
)

var (
	client        *ezthrottle.Client
	webhookStore  = make(map[string]WebhookData)
	webhookMutex  sync.RWMutex
)

type WebhookData struct {
	JobID         string `json:"job_id"`
	IdempotentKey string `json:"idempotent_key"`
	Status        string `json:"status"`
	Response      any    `json:"response"`
}

func main() {
	apiKey := os.Getenv("EZTHROTTLE_API_KEY")
	ezthrottleURL := os.Getenv("EZTHROTTLE_URL")
	appURL := os.Getenv("APP_URL")

	client = ezthrottle.NewClient(
		apiKey,
		ezthrottle.WithEZThrottleURL(ezthrottleURL),
	)

	http.HandleFunc("/webhook", handleWebhook)
	http.HandleFunc("/webhooks/", handleGetWebhook)
	http.HandleFunc("/test/performance/basic", handleTestPerformanceBasic(appURL))
	http.HandleFunc("/test/performance/racing", handleTestPerformanceRacing(appURL))
	http.HandleFunc("/test/frugal/local", handleTestFrugalLocal)
	http.HandleFunc("/test/idempotent/hash", handleTestIdempotentHash(appURL))

	log.Println("Test app running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	var data WebhookData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	webhookMutex.Lock()
	webhookStore[data.JobID] = data
	webhookMutex.Unlock()

	log.Printf("✅ Webhook: %s | key: %s | status: %s", data.JobID, data.IdempotentKey, data.Status)

	json.NewEncoder(w).Encode(map[string]string{
		"status": "received",
		"job_id": data.JobID,
	})
}

func handleGetWebhook(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Path[len("/webhooks/"):]

	webhookMutex.RLock()
	webhook, found := webhookStore[jobID]
	webhookMutex.RUnlock()

	if !found {
		http.Error(w, "Webhook not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"found":   true,
		"job_id":  jobID,
		"webhook": webhook,
	})
}

func handleTestPerformanceBasic(appURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		testKey := fmt.Sprintf("performance_basic_%d", time.Now().UnixMilli())

		result, err := ezthrottle.NewStep(client).
			URL("https://httpbin.org/status/200").
			Type(ezthrottle.StepTypePerformance).
			Webhooks([]ezthrottle.Webhook{
				{URL: appURL + "/webhook", HasQuorumVote: true},
			}).
			IdempotentKey(testKey).
			Execute(r.Context())

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"test":           "performance_basic",
			"idempotent_key": testKey,
			"result":         result,
		})
	}
}

func handleTestPerformanceRacing(appURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		testKey := fmt.Sprintf("performance_racing_%d", time.Now().UnixMilli())

		result, err := ezthrottle.NewStep(client).
			URL("https://httpbin.org/delay/1").
			Type(ezthrottle.StepTypePerformance).
			Regions([]string{"iad", "lax", "ord"}).
			ExecutionMode("race").
			Webhooks([]ezthrottle.Webhook{
				{URL: appURL + "/webhook", HasQuorumVote: true},
			}).
			IdempotentKey(testKey).
			Execute(r.Context())

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"test":           "performance_racing",
			"idempotent_key": testKey,
			"result":         result,
		})
	}
}

func handleTestFrugalLocal(w http.ResponseWriter, r *http.Request) {
	result, err := ezthrottle.NewStep(client).
		URL("https://httpbin.org/status/200").
		Type(ezthrottle.StepTypeFrugal).
		Execute(r.Context())

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"test":   "frugal_local",
		"result": result,
	})
}

func handleTestIdempotentHash(appURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := time.Now().UnixMilli()
		url := fmt.Sprintf("https://httpbin.org/get?test=idempotent_hash&run=%d", runID)

		result1, err := ezthrottle.NewStep(client).
			URL(url).
			Type(ezthrottle.StepTypePerformance).
			Webhooks([]ezthrottle.Webhook{
				{URL: appURL + "/webhook", HasQuorumVote: true},
			}).
			IdempotentStrategy(ezthrottle.IdempotentStrategyHash).
			Execute(r.Context())

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		result2, err := ezthrottle.NewStep(client).
			URL(url).
			Type(ezthrottle.StepTypePerformance).
			Webhooks([]ezthrottle.Webhook{
				{URL: appURL + "/webhook", HasQuorumVote: true},
			}).
			IdempotentStrategy(ezthrottle.IdempotentStrategyHash).
			Execute(r.Context())

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"test":    "idempotent_hash",
			"result1": result1,
			"result2": result2,
			"deduped": result1.JobID == result2.JobID,
		})
	}
}
```

**Deploy to Fly.io:**
```bash
fly launch --name ezthrottle-sdk-go
fly secrets set EZTHROTTLE_API_KEY=ck_live_...
fly secrets set EZTHROTTLE_URL=https://ezthrottle.fly.dev
fly secrets set APP_URL=https://ezthrottle-sdk-go.fly.dev
```

**GitHub Actions CI:**
```yaml
name: Integration Tests

on:
  push:
    branches: [ main, master ]

jobs:
  integration:
    runs-on: ubuntu-latest

    steps:
    - uses: actions/checkout@v4

    - name: Set up Go
      uses: actions/setup-go@v5
      with:
        go-version: '1.21'

    - name: Install dependencies
      run: go mod download

    - name: Install Hurl
      run: |
        VERSION=6.2.0
        curl -sL https://github.com/Orange-OpenSource/hurl/releases/download/$VERSION/hurl_${VERSION}_amd64.deb -o hurl.deb
        sudo dpkg -i hurl.deb

    - name: Install Fly CLI
      uses: superfly/flyctl-actions/setup-flyctl@master

    - name: Run Integration Tests
      env:
        FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
      run: |
        cd test-app
        make integration
```

---

## Total Time: 10-15 hours (2-3 days)

**Timeline:**
- **Day 1:** Phase 1 (Core `SubmitJob()` with all API features)
- **Day 2:** Phase 2 + 3 (Builder pattern + DFS execution)
- **Day 3:** Phase 4 (Testing + examples + publish to pkg.go.dev)

---

## Key Technical Decisions

1. **Context-aware**: All operations accept `context.Context`
2. **Error handling**: Return `error` from all fallible operations (idiomatic Go)
3. **Builder pattern**: Use method chaining (return `*Step`)
4. **Immutability**: Consider making Step immutable (copy-on-write)
5. **Concurrency**: Use goroutines for async workflow continuation

---

## Package Structure

```
ezthrottle-go/
├── client.go         # EZThrottle client
├── step.go           # Step builder
├── step_type.go      # StepType constants
├── idempotent_strategy.go  # IdempotentStrategy constants
├── types.go          # Type definitions
├── webhook.go        # Webhook server (net/http)
├── go.mod
├── go.sum
├── test-app/
│   ├── main.go       # Integration test app
│   ├── tests/        # Hurl tests
│   └── Makefile
└── README.md
```

---

## Publishing

```bash
# Tag release
git tag v1.1.0
git push origin v1.1.0

# pkg.go.dev will automatically index the module
# Users can install with:
# go get github.com/your-org/ezthrottle-go@v1.1.0
```

---

## Reference: Python SDK Implementation

The Python SDK has already implemented all features. Reference:
- `/Users/rahmijamalpruitt/SAAS/ezthrottle-python/ezthrottle/client.py` - submitJob()
- `/Users/rahmijamalpruitt/SAAS/ezthrottle-python/ezthrottle/step.py` - Step builder
- `/Users/rahmijamalpruitt/SAAS/ezthrottle-python/test-app/app.py` - Integration tests

Key commits:
- `21bae93` - submit_job() with full API
- `a6d8035` - Step builder with idempotent strategies
- `23decac` - FRUGAL execution + workflows
- `d5975af` - Integration test suite
- `1c2217e` - Production documentation

All integration tests passing in CI ✅
