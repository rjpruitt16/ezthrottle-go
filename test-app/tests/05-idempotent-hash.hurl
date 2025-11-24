# Test 5: Idempotent HASH - Deduplication

POST {{APP_URL}}/test/idempotent/hash
HTTP 200
[Asserts]
jsonpath "$.deduped" == true

[Captures]
job_id: jsonpath "$.result1.job_id"
idempotent_key: jsonpath "$.idempotent_key"

# Poll for webhook
GET {{APP_URL}}/webhooks/{{idempotent_key}}
[Options]
retry: 30
retry-interval: 1000
HTTP 200
[Asserts]
jsonpath "$.found" == true
