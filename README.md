# EZThrottle Go SDK

Official Go client for [EZThrottle](https://ezthrottle.network) - The World's First API Aqueduct.

## Installation

```bash
go get github.com/rjpruitt16/ezthrottle-go
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    "github.com/rjpruitt16/ezthrottle-go"
)

func main() {
    // Create client (webhook store created automatically)
    client := ezthrottle.NewClient("your_api_key")

    // Setup your server
    router := gin.Default()

    // Mount EZThrottle webhook handler on YOUR existing server
    router.POST("/webhook", gin.WrapH(client.WebhookHandler()))

    // Example: Process payment endpoint
    router.POST("/process-payment", func(c *gin.Context) {
        // Generate unique idempotent key
        idempotentKey := uuid.New().String()

        // Submit job (returns immediately)
        result, _ := ezthrottle.NewStep(client).
            URL("https://api.stripe.com/charges").
            Method("POST").
            IdempotentKey(idempotentKey).
            Webhooks([]ezthrottle.Webhook{
                {URL: "https://your-app.com/webhook"},
            }).
            Execute(context.Background())

        c.JSON(200, gin.H{
            "job_id":         result.JobID,
            "idempotent_key": idempotentKey,
        })
    })

    // Start server (single port!)
    router.Run(":8080")
}
```

## Legacy Code Integration (Error-Based Forwarding)

**The killer feature:** Integrate EZThrottle into existing code without rewriting error handling!

Go doesn't have decorators like Python, but we can achieve the same result with custom errors:

```go
import (
    "errors"
    "fmt"
    "net/http"

    "github.com/rjpruitt16/ezthrottle-go"
)

// ForwardRequest represents a request that should be forwarded to EZThrottle
type ForwardRequest struct {
    URL           string
    Method        string
    Headers       map[string]string
    Body          string
    IdempotentKey string
    Metadata      map[string]interface{}
    Webhooks      []ezthrottle.Webhook
    FallbackOnError []int
}

// Legacy payment function - just return ForwardRequest on errors!
func processPaymentLegacy(orderID string) (*http.Response, *ForwardRequest, error) {
    resp, err := http.Post(
        "https://api.stripe.com/charges",
        "application/json",
        strings.NewReader(`{"amount": 1000, "currency": "usd"}`),
    )

    if err != nil {
        // Network error - forward to EZThrottle
        return nil, &ForwardRequest{
            URL:           "https://api.stripe.com/charges",
            Method:        "POST",
            Body:          `{"amount": 1000, "currency": "usd"}`,
            IdempotentKey: fmt.Sprintf("order_%s", orderID),
            Webhooks:      []ezthrottle.Webhook{{URL: "https://app.com/payment-complete"}},
        }, nil
    }

    if resp.StatusCode == 429 {
        // Rate limited - forward to EZThrottle
        return nil, &ForwardRequest{
            URL:           "https://api.stripe.com/charges",
            Method:        "POST",
            IdempotentKey: fmt.Sprintf("order_%s", orderID),
            Metadata:      map[string]interface{}{"order_id": orderID},
            Webhooks:      []ezthrottle.Webhook{{URL: "https://app.com/payment-complete"}},
        }, nil
    }

    return resp, nil, nil
}

// Helper function to execute with auto-forwarding
func ExecuteWithForwarding(client *ezthrottle.Client, fn func() (*http.Response, *ForwardRequest, error)) (*http.Response, error) {
    resp, forwardReq, err := fn()

    if err != nil {
        return nil, err
    }

    if forwardReq != nil {
        // Auto-forward to EZThrottle
        result, err := ezthrottle.NewStep(client).
            URL(forwardReq.URL).
            Method(forwardReq.Method).
            Headers(forwardReq.Headers).
            Body(forwardReq.Body).
            IdempotentKey(forwardReq.IdempotentKey).
            Webhooks(forwardReq.Webhooks).
            Metadata(forwardReq.Metadata).
            FallbackOnError(forwardReq.FallbackOnError).
            Execute(context.Background())

        if err != nil {
            return nil, err
        }

        fmt.Printf("✅ Forwarded to EZThrottle: %s\n", result.JobID)
        return nil, nil
    }

    return resp, nil
}

// Usage
func main() {
    client := ezthrottle.NewClient("your_api_key")

    // Call legacy function with auto-forwarding
    resp, err := ExecuteWithForwarding(client, func() (*http.Response, *ForwardRequest, error) {
        return processPaymentLegacy("order_12345")
    })

    if resp != nil {
        fmt.Println("Payment succeeded immediately")
    } else if err == nil {
        fmt.Println("Payment forwarded to EZThrottle (check webhook)")
    }
}
```

**Why this is amazing:**
- ✅ No code refactoring required
- ✅ Drop-in replacement for existing error handling
- ✅ Keep your existing function signatures
- ✅ Gradual migration path
- ✅ Works with any HTTP library

## Streaming Webhook Results (Non-Blocking)

Use **channels** to watch for webhook results in real-time without blocking:

```go
import (
    "context"
    "fmt"

    "github.com/google/uuid"
    "github.com/rjpruitt16/ezthrottle-go"
)

func processPayment(client *ezthrottle.Client, amount int) {
    // Generate unique idempotent key
    idempotentKey := fmt.Sprintf("payment_%s", uuid.New().String())

    // Submit payment job
    result, _ := ezthrottle.NewStep(client).
        URL("https://api.stripe.com/charges").
        Method("POST").
        Body(fmt.Sprintf(`{"amount": %d, "currency": "usd"}`, amount)).
        IdempotentKey(idempotentKey).
        Webhooks([]ezthrottle.Webhook{
            {URL: "http://localhost:8080/webhook"},
        }).
        Execute(context.Background())

    fmt.Printf("✅ Job submitted: %s (key: %s)\n", result.JobID, idempotentKey)

    // Watch for webhook result (non-blocking channel)
    webhookChan := client.WatchWebhook(idempotentKey)

    // Do other work while waiting...
    fmt.Println("⏳ Doing other work...")

    // Receive webhook result from channel
    webhookResult := <-webhookChan  // Blocks here until webhook arrives

    fmt.Printf("📬 Webhook received! Status: %s\n", webhookResult.Status)

    // Chain next operation based on result
    if webhookResult.Status == "success" {
        // Generate receipt key
        receiptKey := fmt.Sprintf("receipt_%s", uuid.New().String())

        // Send receipt
        ezthrottle.NewStep(client).
            URL("https://api.sendgrid.com/send").
            IdempotentKey(receiptKey).
            Body(webhookResult.Response.Body).
            Execute(context.Background())

        fmt.Println("📧 Receipt sent!")
    }
}
```

### Advanced: Concurrent Webhook Watching

Process multiple jobs in parallel with goroutines:

```go
func processMultiplePayments(client *ezthrottle.Client, amounts []int) {
    results := make(chan *ezthrottle.WebhookResult, len(amounts))

    // Submit all jobs concurrently
    for i, amount := range amounts {
        go func(idx, amt int) {
            key := fmt.Sprintf("payment_%d_%s", idx, uuid.New().String())

            // Submit job
            ezthrottle.NewStep(client).
                URL("https://api.stripe.com/charges").
                Method("POST").
                Body(fmt.Sprintf(`{"amount": %d}`, amt)).
                IdempotentKey(key).
                Webhooks([]ezthrottle.Webhook{{URL: "https://app.com/webhook"}}).
                Execute(context.Background())

            // Watch for result
            webhookChan := client.WatchWebhook(key)
            result := <-webhookChan

            results <- result
        }(i, amount)
    }

    // Collect all results
    for i := 0; i < len(amounts); i++ {
        result := <-results
        fmt.Printf("Payment %d: %s\n", i, result.Status)
    }
}
```

## Webhook Fanout (Multiple Webhooks)

Send webhook results to multiple endpoints for redundancy or multi-service notifications:

```go
result, err := ezthrottle.NewStep(client).
    URL("https://api.stripe.com/charges").
    Method("POST").
    Webhooks([]ezthrottle.Webhook{
        // Primary webhook (must succeed)
        {URL: "https://app.com/payment-complete", HasQuorumVote: true},

        // Analytics webhook (optional)
        {URL: "https://analytics.com/track", HasQuorumVote: false},

        // Notification service (must succeed)
        {URL: "https://notify.com/alert", HasQuorumVote: true},

        // Multi-region webhook racing
        {
            URL: "https://backup.com/webhook",
            Regions: []string{"iad", "lax"},
            HasQuorumVote: true,
        },
    }).
    WebhookQuorum(2).  // At least 2 webhooks with HasQuorumVote=true must succeed
    Execute(context.Background())
```

**Webhook Options:**
- `URL` - Webhook endpoint URL
- `Regions` - (Optional) Deliver webhook from specific regions
- `HasQuorumVote` - (Optional) Counts toward quorum (default: true)

**Use Cases:**
- Notify multiple services (payment processor + analytics + CRM)
- Redundancy (multiple backup webhooks)
- Multi-region delivery (low latency globally)

## Handling Rate Limits

```go
resp, err := client.QueueRequest(req)
if err != nil {
    if ezErr, ok := err.(*ezthrottle.EZThrottleError); ok && ezErr.RetryAt > 0 {
        waitMs := ezErr.RetryAt - time.Now().UnixMilli()
        fmt.Printf("Rate limited, retry in %dms\n", waitMs)
        
        // Wait and retry with suggested timestamp
        time.Sleep(time.Duration(waitMs) * time.Millisecond)
        req.RetryAt = ezErr.RetryAt
        resp, err = client.QueueRequest(req)
    }
}
```

## Configuration

```go
client := ezthrottle.NewClient(
    "your_api_key",
    ezthrottle.WithTracktTagsURL("https://custom-tracktags.com"),
    ezthrottle.WithHTTPClient(&http.Client{
        Timeout: 60 * time.Second,
    }),
)
```

## API Reference

### `NewClient(apiKey string, opts ...ClientOption) *Client`

Creates a new EZThrottle client.

### `QueueRequest(req *QueueRequest) (*QueueResponse, error)`

Queue a request through EZThrottle. Returns immediately with a job ID.

### `Request(method, url string, headers map[string]string, body string) (*http.Response, error)`

Make a direct HTTP request (bypasses EZThrottle).

### `QueueAndWait(req *QueueRequest, timeout, pollInterval time.Duration) (map[string]interface{}, error)`

Queue a request and wait for completion (blocking). Note: You'll need to implement webhook polling logic.

## Types

### `QueueRequest`

```go
type QueueRequest struct {
    URL        string            // Target API URL
    WebhookURL string            // Webhook for result notification
    Method     string            // HTTP method (default: "GET")
    Headers    map[string]string // Request headers
    Body       string            // Request body
    Metadata   map[string]string // Custom metadata
    RetryAt    int64             // Unix ms timestamp for retry
}
```

### `QueueResponse`

```go
type QueueResponse struct {
    JobID     string // Unique job identifier
    Status    string // Job status
    QueuedAt  int64  // Unix ms timestamp
}
```

### `EZThrottleError`

```go
type EZThrottleError struct {
    Message string
    RetryAt int64 // Unix ms timestamp when to retry
}
```

## Step Builder API (v1.1.0+)

The modern Step builder provides a fluent interface with FRUGAL and PERFORMANCE execution modes:

### PERFORMANCE Mode (Server-side execution)

```go
import (
    "context"
    "github.com/rjpruitt16/ezthrottle-go"
)

// Multi-region racing with webhooks
result, err := ezthrottle.NewStep(client).
    URL("https://api.stripe.com/charges").
    Method("POST").
    Type(ezthrottle.StepTypePerformance).
    Webhooks([]ezthrottle.Webhook{
        {URL: "https://app.com/webhook", HasQuorumVote: true},
    }).
    Regions([]string{"iad", "lax", "ord"}).
    ExecutionMode("race").
    Execute(context.Background())
```

### FRUGAL Mode (Client-side first)

```go
// Execute locally, only queue on specific errors
result, err := ezthrottle.NewStep(client).
    URL("https://api.example.com").
    Type(ezthrottle.StepTypeFrugal).
    FallbackOnError([]int{429, 500, 503}).
    Execute(context.Background())
```

### Workflow Chaining

```go
// Analytics step (cheap)
analytics := ezthrottle.NewStep(client).
    URL("https://analytics.com/track").
    Type(ezthrottle.StepTypeFrugal)

// Notification (fast, distributed)
notification := ezthrottle.NewStep(client).
    URL("https://notify.com").
    Type(ezthrottle.StepTypePerformance).
    Webhooks([]ezthrottle.Webhook{{URL: "https://app.com/webhook"}}).
    Regions([]string{"iad", "lax"}).
    OnSuccess(analytics)

// Primary API call (cheap local execution)
result, err := ezthrottle.NewStep(client).
    URL("https://api.example.com").
    Type(ezthrottle.StepTypeFrugal).
    FallbackOnError([]int{429, 500}).
    OnSuccess(notification).
    Execute(context.Background())
```

### Fallback Chains

```go
backup := ezthrottle.NewStep(client).URL("https://backup-api.com")

result, err := ezthrottle.NewStep(client).
    URL("https://primary-api.com").
    Fallback(backup, []int{500, 502, 503}, 0).
    Execute(context.Background())
```

### Idempotent Key Strategies

```go
// HASH strategy (default) - prevents duplicates
result, err := ezthrottle.NewStep(client).
    URL("https://api.stripe.com/charges").
    IdempotentStrategy(ezthrottle.IdempotentStrategyHash).
    Execute(context.Background())

// UNIQUE strategy - allows duplicates (for polling)
result, err := ezthrottle.NewStep(client).
    URL("https://api.example.com/status").
    IdempotentStrategy(ezthrottle.IdempotentStrategyUnique).
    Execute(context.Background())
```

## Webhook Payload

When EZThrottle completes your job, it sends a POST request to your webhook URL with the following JSON payload:

```json
{
  "job_id": "job_1763674210055_853341",
  "idempotent_key": "custom_key_or_generated_hash",
  "status": "success",
  "response": {
    "status_code": 200,
    "headers": {
      "content-type": "application/json"
    },
    "body": "{\"result\": \"data\"}"
  },
  "metadata": {}
}
```

**Fields:**
- `job_id` - Unique identifier for this job
- `idempotent_key` - Your custom key or auto-generated hash
- `status` - `"success"` or `"failed"`
- `response.status_code` - HTTP status code from the target API
- `response.headers` - Response headers from the target API
- `response.body` - Response body from the target API (as string)
- `metadata` - Custom metadata you provided during job submission

**Example webhook handler (Gin):**
```go
import "github.com/gin-gonic/gin"

func handleWebhook(c *gin.Context) {
    var payload struct {
        JobID         string `json:"job_id"`
        IdempotentKey string `json:"idempotent_key"`
        Status        string `json:"status"`
        Response      struct {
            StatusCode int               `json:"status_code"`
            Headers    map[string]string `json:"headers"`
            Body       string            `json:"body"`
        } `json:"response"`
        Metadata map[string]interface{} `json:"metadata"`
    }

    if err := c.BindJSON(&payload); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    if payload.Status == "success" {
        // Process successful result
        log.Printf("Job %s succeeded: %s", payload.JobID, payload.Response.Body)
    } else {
        // Handle failure
        log.Printf("Job %s failed", payload.JobID)
    }

    c.JSON(200, gin.H{"ok": true})
}
```

## Mixed Workflow Chains (FRUGAL ↔ PERFORMANCE)

Mix FRUGAL and PERFORMANCE steps in the same workflow to optimize for both cost and speed:

### Example 1: FRUGAL → PERFORMANCE (Save money, then fast delivery)

```go
// Primary API call is cheap (local execution)
// But notification needs speed (multi-region racing)
result, err := ezthrottle.NewStep(client).
    URL("https://api.openai.com/v1/chat/completions").
    Type(ezthrottle.StepTypeFrugal).
    FallbackOnError([]int{429, 500}).
    OnSuccess(
        // Chain to PERFORMANCE for fast webhook delivery
        ezthrottle.NewStep(client).
            URL("https://api.sendgrid.com/send").
            Type(ezthrottle.StepTypePerformance).
            Webhooks([]ezthrottle.Webhook{{URL: "https://app.com/email-sent"}}).
            Regions([]string{"iad", "lax", "ord"}),
    ).
    Execute(context.Background())
```

### Example 2: PERFORMANCE → FRUGAL (Fast payment, then cheap analytics)

```go
// Critical payment needs speed (racing)
// But analytics is cheap (local execution when webhook arrives)
payment, err := ezthrottle.NewStep(client).
    URL("https://api.stripe.com/charges").
    Type(ezthrottle.StepTypePerformance).
    Webhooks([]ezthrottle.Webhook{{URL: "https://app.com/payment-complete"}}).
    Regions([]string{"iad", "lax"}).
    OnSuccess(
        // Analytics doesn't need speed - save money!
        ezthrottle.NewStep(client).
            URL("https://analytics.com/track").
            Type(ezthrottle.StepTypeFrugal),
    ).
    Execute(context.Background())
```

### Example 3: Complex Mixed Workflow

```go
// Optimize every step for its requirements
workflow, err := ezthrottle.NewStep(client).
    URL("https://cheap-api.com").
    Type(ezthrottle.StepTypeFrugal).
    FallbackOnError([]int{429, 500}).
    Fallback(
        ezthrottle.NewStep(client).URL("https://backup-api.com"),
        []int{500}, 0,
    ).
    OnSuccess(
        // Critical notification needs PERFORMANCE
        ezthrottle.NewStep(client).
            URL("https://critical-webhook.com").
            Type(ezthrottle.StepTypePerformance).
            Webhooks([]ezthrottle.Webhook{{URL: "https://app.com/webhook"}}).
            Regions([]string{"iad", "lax", "ord"}).
            OnSuccess(
                // Analytics is cheap again
                ezthrottle.NewStep(client).
                    URL("https://analytics.com/track").
                    Type(ezthrottle.StepTypeFrugal),
            ),
    ).
    OnFailure(
        // Simple Slack alert doesn't need PERFORMANCE
        ezthrottle.NewStep(client).
            URL("https://hooks.slack.com/webhook").
            Type(ezthrottle.StepTypeFrugal),
    ).
    Execute(context.Background())
```

**Why mix workflows?**
- ✅ **Cost optimization** - Only pay for what needs speed
- ✅ **Performance where it matters** - Critical paths get multi-region racing
- ✅ **Flexibility** - Every step optimized for its specific requirements

## Production Ready ✅

This SDK is production-ready with **working examples validated in CI on every push**.

### Reference Implementation: test-app/

The `test-app/` directory contains **real, working code** you can learn from. Not toy examples - this is production code we run in automated tests against live EZThrottle backend.

**Validated in CI:**
- ✅ GitHub Actions runs these examples against live backend on every push
- ✅ 7 integration tests covering all SDK features
- ✅ Proves the code actually works, not just documentation

## License

MIT

## Links

- [EZThrottle Website](https://ezthrottle.com)
- [Documentation](https://docs.ezthrottle.com)
- [GitHub](https://github.com/rjpruitt16/ezthrottle-go)
