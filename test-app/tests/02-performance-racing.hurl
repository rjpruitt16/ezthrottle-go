# Test 2: PERFORMANCE - Multi-region racing

POST {{APP_URL}}/test/performance/racing
HTTP 200
[Captures]
job_id: jsonpath "$.result.job_id"
idempotent_key: jsonpath "$.idempotent_key"

# Poll for webhook
GET {{APP_URL}}/webhooks/{{idempotent_key}}
[Options]
retry: 30
retry-interval: 1000
HTTP 200
[Asserts]
jsonpath "$.found" == true
jsonpath "$.webhook.data.status" == "success"
