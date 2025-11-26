.PHONY: integration deploy test teardown clean help secrets

# Variables
APP_NAME := ezthrottle-sdk-go
APP_URL := https://$(APP_NAME).fly.dev

# EZThrottle environment (prod, staging, or custom app name)
EZTHROTTLE_ENV ?= prod
EZTHROTTLE_URL := $(if $(filter prod,$(EZTHROTTLE_ENV)),https://ezthrottle.fly.dev,$(if $(filter staging,$(EZTHROTTLE_ENV)),https://ezthrottle-staging.fly.dev,https://$(EZTHROTTLE_ENV).fly.dev))

help:
	@echo "EZThrottle Go SDK Integration Tests"
	@echo ""
	@echo "Usage:"
	@echo "  make integration                      # Test against prod (default)"
	@echo "  make integration EZTHROTTLE_ENV=staging  # Test against staging"
	@echo "  make integration EZTHROTTLE_ENV=my-app   # Test against custom app"
	@echo ""
	@echo "Individual targets:"
	@echo "  make secrets     # Set Fly secrets from .env.test"
	@echo "  make deploy      # Deploy app to Fly"
	@echo "  make test        # Run hurl tests"
	@echo "  make teardown    # Scale app to zero"

secrets:
	@echo "🔒 Setting Fly secrets (EZThrottle: $(EZTHROTTLE_ENV) → $(EZTHROTTLE_URL))..."
	@if [ -f .env.test ]; then \
		fly secrets set --app $(APP_NAME) EZTHROTTLE_URL=$(EZTHROTTLE_URL) APP_URL=$(APP_URL) $$(grep -v '^#' .env.test | grep -v '^$$' | xargs) 2>/dev/null || echo "⚠️  Could not set secrets (app may not exist yet)"; \
	else \
		fly secrets set --app $(APP_NAME) EZTHROTTLE_URL=$(EZTHROTTLE_URL) APP_URL=$(APP_URL) 2>/dev/null || echo "⚠️  Could not set secrets"; \
	fi
	@echo "✅ Secrets set (or skipped)"

# ONE COMMAND: Deploy → Test → Teardown (only if tests pass)
integration:
	@echo "🚀 Starting integration test cycle (EZThrottle: $(EZTHROTTLE_ENV))..."
	@echo ""

	@# Step 1: Deploy
	@echo "📦 STEP 1/4: Deploying to Fly..."
	@$(MAKE) deploy
	@echo ""
	@# Step 2: Scale up (ensure app is running)
	@echo "⬆️  STEP 2/4: Scaling app up..."
	@fly scale count 1 --app $(APP_NAME) --yes 2>&1 || echo "⚠️  Scale up failed"
	@echo "   Waiting 10s for app to start..."
	@sleep 10
	@echo "✅ App is running"
	@echo ""

	@# Step 3: Test (if this fails, don't teardown)
	@echo "🧪 STEP 3/4: Running integration tests..."
	@$(MAKE) test || (echo "❌ Tests failed! App left running for debugging: $(APP_URL)" && exit 1)
	@echo ""

	@# Step 4: Scale to zero (only reached if tests pass)
	@echo "🔻 STEP 4/4: Scaling app to zero..."
	@$(MAKE) teardown
	@echo "✅ Integration tests complete! App scaled to zero (no cost)"

deploy:
	@echo "   Listing Fly organizations..."
	@fly orgs list || echo "⚠️  Could not list orgs"

	@echo "   Listing Fly apps..."
	@fly apps list || echo "⚠️  Could not list apps"

	@echo "   Creating Fly app (if needed)..."
	@fly apps create $(APP_NAME) --org ezthrottle 2>&1 || echo "⚠️  App create failed (might already exist)"

	@echo "   Setting secrets..."
	@$(MAKE) secrets

	@echo "   Deploying to Fly..."
	@cd .. && fly deploy \
		--config test-app/fly.toml \
		--dockerfile test-app/Dockerfile \
		--app $(APP_NAME) \
		--ha=false

	@echo "   Waiting for deployment..."
	@sleep 10
	@echo "✅ Deployed: $(APP_URL)"

test:
	@echo "   Resetting webhooks..."
	@curl -X POST $(APP_URL)/webhooks/reset -s > /dev/null
	@echo "   Running hurl tests..."
	@hurl --test \
		--variable TEST_APP_URL=$(APP_URL) \
		--color \
		tests/*.hurl
	@echo "✅ All tests passed"

teardown:
	@fly scale count 0 --app $(APP_NAME) --yes 2>&1 || echo "⚠️  Scale to zero failed (app might not exist)"
	@echo "✅ App scaled to zero (costs nothing when idle)"

clean:
	@curl -X POST $(APP_URL)/webhooks/reset
	@echo "✅ Webhooks cleared"
