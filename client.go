package ezthrottle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client represents an EZThrottle SDK client
type Client struct {
	apiKey        string
	tracktTagsURL string
	httpClient    *http.Client
	webhookStore  *WebhookStore
}

// NewClient creates a new EZThrottle client
func NewClient(apiKey string, opts ...ClientOption) *Client {
	c := &Client{
		apiKey:        apiKey,
		tracktTagsURL: "https://tracktags.fly.dev",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		webhookStore: NewWebhookStore(), // Always create webhook store
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// ClientOption is a functional option for configuring the client
type ClientOption func(*Client)

// WithTracktTagsURL sets a custom TracktTags URL
func WithTracktTagsURL(url string) ClientOption {
	return func(c *Client) {
		c.tracktTagsURL = url
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WebhookHandler returns an HTTP handler for receiving webhooks from EZThrottle.
// Mount this handler on YOUR existing server at any route you want.
//
// Example with Gin:
//
//	router := gin.Default()
//	router.POST("/webhook", gin.WrapH(client.WebhookHandler()))
//
// Example with net/http:
//
//	http.Handle("/webhook", client.WebhookHandler())
//
// Example with Echo:
//
//	e.POST("/webhook", echo.WrapHandler(client.WebhookHandler()))
func (c *Client) WebhookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.webhookStore.processWebhook(w, r)
	})
}

// GetWebhookResult retrieves a webhook result by idempotent key
func (c *Client) GetWebhookResult(idempotentKey string) (*WebhookResult, bool) {
	return c.webhookStore.Get(idempotentKey)
}

// WatchWebhook returns a channel that will receive the webhook result.
// Use this to wait for webhook results in a non-blocking way.
//
// Example:
//
//	result, _ := step.Execute(ctx)
//	webhookChan := client.WatchWebhook(result.IdempotentKey)
//	webhookResult := <-webhookChan  // Blocks until webhook arrives
func (c *Client) WatchWebhook(idempotentKey string) <-chan *WebhookResult {
	return c.webhookStore.Watch(idempotentKey)
}

// SubmitJob submits a job to EZThrottle with full API support
func (c *Client) SubmitJob(req *SubmitJobRequest) (*QueueResponse, error) {
	// Build the job payload
	jobPayload := map[string]interface{}{
		"url":    req.URL,
		"method": req.Method,
	}

	// Add optional fields
	if req.Headers != nil {
		jobPayload["headers"] = req.Headers
	}
	if req.Body != "" {
		jobPayload["body"] = req.Body
	}
	if req.Metadata != nil {
		jobPayload["metadata"] = req.Metadata
	}
	if req.Webhooks != nil && len(req.Webhooks) > 0 {
		jobPayload["webhooks"] = req.Webhooks
	}
	if req.WebhookQuorum > 0 {
		jobPayload["webhook_quorum"] = req.WebhookQuorum
	}
	if req.Regions != nil && len(req.Regions) > 0 {
		jobPayload["regions"] = req.Regions
	}
	if req.RegionPolicy != "" {
		jobPayload["region_policy"] = req.RegionPolicy
	}
	if req.ExecutionMode != "" {
		jobPayload["execution_mode"] = req.ExecutionMode
	}
	if req.RetryPolicy != nil {
		jobPayload["retry_policy"] = req.RetryPolicy
	}
	if req.FallbackJob != nil {
		jobPayload["fallback_job"] = req.FallbackJob
	}
	if req.OnSuccess != nil {
		jobPayload["on_success"] = req.OnSuccess
	}
	if req.OnFailure != nil {
		jobPayload["on_failure"] = req.OnFailure
	}
	if req.OnFailureTimeoutMs > 0 {
		jobPayload["on_failure_timeout_ms"] = req.OnFailureTimeoutMs
	}
	if req.IdempotentKey != "" {
		jobPayload["idempotent_key"] = req.IdempotentKey
	}
	if req.RetryAt > 0 {
		jobPayload["retry_at"] = req.RetryAt
	}

	// Marshal job payload to JSON string (TracktTags proxy expects string, not object)
	jobPayloadBytes, err := json.Marshal(jobPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job payload: %w", err)
	}

	// Build proxy payload
	proxyPayload := map[string]interface{}{
		"scope":       "customer",
		"metric_name": "", // Empty = check all plan limits
		"target_url":  "https://ezthrottle.fly.dev/api/v1/jobs",
		"method":      "POST",
		"headers": map[string]string{
			"Content-Type": "application/json",
		},
		"body": string(jobPayloadBytes), // Send as JSON string, not object
	}

	payloadBytes, err := json.Marshal(proxyPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Make request to TracktTags proxy
	httpReq, err := http.NewRequest("POST", c.tracktTagsURL+"/api/v1/proxy", bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Handle rate limiting
	if resp.StatusCode == 429 {
		var errResp ErrorResponse
		if err := json.Unmarshal(bodyBytes, &errResp); err == nil {
			// Extract Retry-After header if present
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				// Convert seconds to milliseconds timestamp
				var seconds int64
				fmt.Sscanf(retryAfter, "%d", &seconds)
				errResp.RetryAt = time.Now().UnixMilli() + (seconds * 1000)
			}
			return nil, &EZThrottleError{
				Message: errResp.Error,
				RetryAt: errResp.RetryAt,
			}
		}
		return nil, &EZThrottleError{Message: "Rate limited"}
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("proxy request failed: %s", string(bodyBytes))
	}

	// Parse proxy response
	var proxyResp ProxyResponse
	if err := json.Unmarshal(bodyBytes, &proxyResp); err != nil {
		return nil, fmt.Errorf("failed to parse proxy response: %w", err)
	}

	if proxyResp.Status != "allowed" {
		return nil, fmt.Errorf("request denied: %s", proxyResp.Error)
	}

	// Parse forwarded EZThrottle response
	forwarded := proxyResp.ForwardedResponse
	// Accept both 200 (duplicate job) and 201 (new job created)
	if forwarded.StatusCode != 200 && forwarded.StatusCode != 201 {
		return nil, fmt.Errorf("EZThrottle job creation failed: %s", forwarded.Body)
	}

	var queueResp QueueResponse
	if err := json.Unmarshal([]byte(forwarded.Body), &queueResp); err != nil {
		return nil, fmt.Errorf("failed to parse EZThrottle response: %w", err)
	}

	return &queueResp, nil
}

// QueueRequest queues a request through EZThrottle (DEPRECATED: Use SubmitJob)
// This method is kept for backward compatibility
func (c *Client) QueueRequest(req *QueueRequest) (*QueueResponse, error) {
	// Convert QueueRequest to SubmitJobRequest
	submitReq := &SubmitJobRequest{
		URL:     req.URL,
		Method:  req.Method,
		Headers: req.Headers,
		Body:    req.Body,
		RetryAt: req.RetryAt,
	}

	// Convert webhook_url to webhooks array
	if req.WebhookURL != "" {
		submitReq.Webhooks = []Webhook{{URL: req.WebhookURL, HasQuorumVote: true}}
		submitReq.WebhookQuorum = 1
	}

	// Convert metadata map[string]string to map[string]interface{}
	if req.Metadata != nil {
		submitReq.Metadata = make(map[string]interface{})
		for k, v := range req.Metadata {
			submitReq.Metadata[k] = v
		}
	}

	return c.SubmitJob(submitReq)
}

// Request makes a direct HTTP request (bypasses TracktTags and EZThrottle)
func (c *Client) Request(method, url string, headers map[string]string, body string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewReader([]byte(body))
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return c.httpClient.Do(req)
}

// QueueAndWait queues a request and polls for completion (blocking)
func (c *Client) QueueAndWait(req *QueueRequest, timeout time.Duration, pollInterval time.Duration) (map[string]interface{}, error) {
	resp, err := c.QueueRequest(req)
	if err != nil {
		return nil, err
	}

	// Start polling for webhook result
	start := time.Now()
	for time.Since(start) < timeout {
		time.Sleep(pollInterval)
		// User should implement their own webhook polling logic
		// This is just a placeholder
	}

	return nil, fmt.Errorf("timeout waiting for job %s", resp.JobID)
}

// Types

// QueueRequest represents a request to queue through EZThrottle (DEPRECATED: Use SubmitJobRequest)
type QueueRequest struct {
	URL        string            `json:"url"`
	WebhookURL string            `json:"webhook_url"`
	Method     string            `json:"method,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	RetryAt    int64             `json:"retry_at,omitempty"` // Unix timestamp in milliseconds
}

// SubmitJobRequest represents a job submission with full API support
type SubmitJobRequest struct {
	URL                 string                 `json:"url"`
	Method              string                 `json:"method"`
	Headers             map[string]string      `json:"headers,omitempty"`
	Body                string                 `json:"body,omitempty"`
	Metadata            map[string]interface{} `json:"metadata,omitempty"`
	Webhooks            []Webhook              `json:"webhooks,omitempty"`
	WebhookQuorum       int                    `json:"webhook_quorum,omitempty"`
	Regions             []string               `json:"regions,omitempty"`
	RegionPolicy        string                 `json:"region_policy,omitempty"`        // "fallback" or "strict"
	ExecutionMode       string                 `json:"execution_mode,omitempty"`       // "race" or "fanout"
	RetryPolicy         *RetryPolicy           `json:"retry_policy,omitempty"`
	Trigger             map[string]interface{} `json:"trigger,omitempty"`              // Fallback trigger (on_error, on_timeout)
	FallbackJob         *SubmitJobRequest      `json:"fallback_job,omitempty"`
	OnSuccess           *SubmitJobRequest      `json:"on_success,omitempty"`
	OnFailure           *SubmitJobRequest      `json:"on_failure,omitempty"`
	OnFailureTimeoutMs  int                    `json:"on_failure_timeout_ms,omitempty"`
	IdempotentKey       string                 `json:"idempotent_key,omitempty"`
	RetryAt             int64                  `json:"retry_at,omitempty"` // Unix timestamp in milliseconds
}

// Webhook represents a webhook configuration
type Webhook struct {
	URL            string   `json:"url"`
	Regions        []string `json:"regions,omitempty"`
	HasQuorumVote  bool     `json:"hasQuorumVote,omitempty"`
}

// RetryPolicy represents retry configuration
type RetryPolicy struct {
	MaxRetries   int   `json:"maxRetries,omitempty"`
	MaxReroutes  int   `json:"maxReroutes,omitempty"`
	RetryCodes   []int `json:"retryCodes,omitempty"`
	RerouteCodes []int `json:"rerouteCodes,omitempty"`
}

// QueueResponse represents the response from queueing a job
type QueueResponse struct {
	JobID          string `json:"job_id"`
	IdempotentKey  string `json:"idempotent_key,omitempty"`
	Status         string `json:"status"`
	QueuedAt       int64  `json:"queued_at"`
	EstimatedProcessingTime int `json:"estimated_processing_time,omitempty"`

	// FRUGAL local execution fields
	ExecutedLocally bool   `json:"executed_locally,omitempty"`
	StatusCode      int    `json:"status_code,omitempty"`
	ResponseBody    string `json:"response_body,omitempty"`
}

// ProxyResponse represents the TracktTags proxy response
type ProxyResponse struct {
	Status            string            `json:"status"`
	ForwardedResponse ForwardedResponse `json:"forwarded_response"`
	Error             string            `json:"error,omitempty"`
}

// ForwardedResponse represents the forwarded response from EZThrottle
type ForwardedResponse struct {
	StatusCode int               `json:"status_code"`
	Body       string            `json:"body"`
	Headers    map[string]string `json:"headers"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	RetryAt int64  `json:"retry_at,omitempty"`
}

// EZThrottleError represents an EZThrottle-specific error
type EZThrottleError struct {
	Message string
	RetryAt int64 // Unix timestamp in milliseconds when to retry
}

func (e *EZThrottleError) Error() string {
	if e.RetryAt > 0 {
		return fmt.Sprintf("%s (retry at: %d)", e.Message, e.RetryAt)
	}
	return e.Message
}

// ============================================================================
// WEBHOOK SECRETS MANAGEMENT
// ============================================================================

// CreateWebhookSecret creates or updates webhook HMAC secrets for signature verification.
//
// Parameters:
//   - primarySecret: Primary webhook secret (min 16 characters)
//   - secondarySecret: Optional secondary secret for rotation (min 16 characters, use empty string if not provided)
//
// Returns:
//   - map containing response with status and message
//   - error if secret creation fails
//
// Example:
//
//	// Create primary secret
//	result, err := client.CreateWebhookSecret("your_secure_secret_here_min_16_chars", "")
//
//	// Create with rotation support (primary + secondary)
//	result, err := client.CreateWebhookSecret(
//	    "new_secret_after_rotation",
//	    "old_secret_before_rotation",
//	)
func (c *Client) CreateWebhookSecret(primarySecret, secondarySecret string) (map[string]interface{}, error) {
	if len(primarySecret) < 16 {
		return nil, fmt.Errorf("primarySecret must be at least 16 characters")
	}

	if secondarySecret != "" && len(secondarySecret) < 16 {
		return nil, fmt.Errorf("secondarySecret must be at least 16 characters")
	}

	payload := map[string]interface{}{
		"primary_secret": primarySecret,
	}
	if secondarySecret != "" {
		payload["secondary_secret"] = secondarySecret
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	proxyPayload := map[string]interface{}{
		"scope":       "customer",
		"metric_name": "",
		"target_url":  "https://ezthrottle.fly.dev/api/v1/webhook-secrets",
		"method":      "POST",
		"headers": map[string]string{
			"Content-Type": "application/json",
		},
		"body": string(payloadBytes),
	}

	return c.proxyRequest(proxyPayload)
}

// GetWebhookSecret retrieves webhook secrets (masked for security).
//
// Returns:
//   - map containing customer_id, masked primary_secret, masked secondary_secret, and has_secondary boolean
//   - error if secrets not configured (404) or request fails
//
// Example:
//
//	secrets, err := client.GetWebhookSecret()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Primary: %s\n", secrets["primary_secret"]) // "your****ars"
//	fmt.Printf("Has Secondary: %v\n", secrets["has_secondary"]) // true/false
func (c *Client) GetWebhookSecret() (map[string]interface{}, error) {
	proxyPayload := map[string]interface{}{
		"scope":       "customer",
		"metric_name": "",
		"target_url":  "https://ezthrottle.fly.dev/api/v1/webhook-secrets",
		"method":      "GET",
		"headers":     map[string]string{},
		"body":        "",
	}

	result, err := c.proxyRequest(proxyPayload)
	if err != nil {
		// Check if it's a 404 (no secrets configured)
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "No webhook secrets configured") {
			return nil, fmt.Errorf("no webhook secrets configured. Call CreateWebhookSecret() first")
		}
		return nil, err
	}

	return result, nil
}

// DeleteWebhookSecret deletes webhook secrets.
//
// Returns:
//   - map containing response with status and message
//   - error if deletion fails
//
// Example:
//
//	result, err := client.DeleteWebhookSecret()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(result["message"]) // "Webhook secrets deleted"
func (c *Client) DeleteWebhookSecret() (map[string]interface{}, error) {
	proxyPayload := map[string]interface{}{
		"scope":       "customer",
		"metric_name": "",
		"target_url":  "https://ezthrottle.fly.dev/api/v1/webhook-secrets",
		"method":      "DELETE",
		"headers":     map[string]string{},
		"body":        "",
	}

	return c.proxyRequest(proxyPayload)
}

// RotateWebhookSecret rotates webhook secret safely by promoting secondary to primary.
//
// Parameters:
//   - newSecret: New webhook secret to set as primary (min 16 characters)
//
// Returns:
//   - map containing response with status and message
//   - error if rotation fails
//
// Example:
//
//	// Step 1: Rotate (keeps old secret as backup)
//	result, err := client.RotateWebhookSecret("new_secret_min_16_chars")
//
//	// Step 2: After verifying webhooks work with new secret
//	// Remove old secret by setting only new one
//	result, err = client.CreateWebhookSecret("new_secret_min_16_chars", "")
func (c *Client) RotateWebhookSecret(newSecret string) (map[string]interface{}, error) {
	if len(newSecret) < 16 {
		return nil, fmt.Errorf("newSecret must be at least 16 characters")
	}

	// Try to get current secret
	current, err := c.GetWebhookSecret()
	if err != nil {
		// No existing secret, just create new one
		if strings.Contains(err.Error(), "No webhook secrets configured") {
			return c.CreateWebhookSecret(newSecret, "")
		}
		return nil, err
	}

	// Get old primary secret
	oldPrimary, ok := current["primary_secret"].(string)
	if !ok || strings.Contains(oldPrimary, "****") {
		// Masked secret, can't use as secondary
		return c.CreateWebhookSecret(newSecret, "")
	}

	// Set new as primary, old as secondary
	return c.CreateWebhookSecret(newSecret, oldPrimary)
}

// proxyRequest is a helper method for making requests through TracktTags proxy
func (c *Client) proxyRequest(proxyPayload map[string]interface{}) (map[string]interface{}, error) {
	payloadBytes, err := json.Marshal(proxyPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", c.tracktTagsURL+"/api/v1/proxy", bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("proxy request failed: %s", string(bodyBytes))
	}

	var proxyResp ProxyResponse
	if err := json.Unmarshal(bodyBytes, &proxyResp); err != nil {
		return nil, fmt.Errorf("failed to parse proxy response: %w", err)
	}

	if proxyResp.Status != "allowed" {
		return nil, fmt.Errorf("request denied: %s", proxyResp.Error)
	}

	forwarded := proxyResp.ForwardedResponse
	if forwarded.StatusCode == 404 {
		return nil, fmt.Errorf("404: %s", forwarded.Body)
	}

	if forwarded.StatusCode < 200 || forwarded.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed: %s", forwarded.Body)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(forwarded.Body), &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}
