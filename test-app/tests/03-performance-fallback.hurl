# Test 3: PERFORMANCE - Fallback chain

POST {{APP_URL}}/test/performance/fallback-chain
HTTP 200
[Captures]
job_id: jsonpath "$.result.job_id"
idempotent_key: jsonpath "$.idempotent_key"

# Poll for webhook
GET {{APP_URL}}/webhooks/{{idempotent_key}}
[Options]
retry: 40
retry-interval: 1000
HTTP 200
[Asserts]
jsonpath "$.found" == true
jsonpath "$.webhook.data.status" == "success"
