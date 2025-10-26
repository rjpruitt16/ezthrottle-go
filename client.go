package main

import (
	"fmt"
	"log"
	"time"

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
		Headers: map[string]string{
			"X-Custom-Header": "value",
		},
	}

	resp, err := client.QueueRequest(req)
	if err != nil {
		// Handle rate limiting
		if ezErr, ok := err.(*ezthrottle.EZThrottleError); ok && ezErr.RetryAt > 0 {
			waitMs := ezErr.RetryAt - time.Now().UnixMilli()
			fmt.Printf("Rate limited, retry in %dms\n", waitMs)
			
			// Wait and retry
			time.Sleep(time.Duration(waitMs) * time.Millisecond)
			
			// Retry with suggested timestamp
			req.RetryAt = ezErr.RetryAt
			resp, err = client.QueueRequest(req)
			if err != nil {
				log.Fatal(err)
			}
		} else {
			log.Fatal(err)
		}
	}

	fmt.Printf("Job queued: %s\n", resp.JobID)
}
