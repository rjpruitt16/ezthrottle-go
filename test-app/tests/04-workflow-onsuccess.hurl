# Test 4: Workflow - on_success chaining

POST {{APP_URL}}/test/workflow/on-success
HTTP 200
[Captures]
parent_job_id: jsonpath "$.result.job_id"
parent_key: jsonpath "$.parent_key"
child_key: jsonpath "$.child_key"

# Poll for parent webhook
GET {{APP_URL}}/webhooks/{{parent_key}}
[Options]
retry: 30
retry-interval: 1000
HTTP 200
[Asserts]
jsonpath "$.found" == true
jsonpath "$.webhook.data.status" == "success"
jsonpath "$.webhook.data.idempotent_key" == "{{parent_key}}"
