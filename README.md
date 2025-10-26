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
    "fmt"
    "log"
    
    "github.com/rjpruitt16/ezthrottle-go"
)

func main() {
    // Create client
    client := ezthrottle.NewClient("your_api_key")
    
    // Queue a request
    req := &ezthrottle.QueueRequest{
        URL:        "https://api.example.com/data",
        WebhookURL: "https://your-webhook.com",
        Method:     "GET",
    }
    
    resp, err := client.QueueRequest(req)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Job queued: %s\n", resp.JobID)
}
```

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

## License

MIT

## Links

- [EZThrottle Website](https://ezthrottle.com)
- [Documentation](https://docs.ezthrottle.com)
- [GitHub](https://github.com/rjpruitt16/ezthrottle-go)
