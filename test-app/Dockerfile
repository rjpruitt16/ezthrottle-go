# EZThrottle Go SDK Test App
# Uses local SDK (not published to pkg.go.dev yet)

FROM golang:1.21-alpine AS builder

LABEL fly_launch_runtime="Go"

WORKDIR /build

# Copy SDK source code from parent directory
COPY go.mod /build/sdk/go.mod
COPY go.sum /build/sdk/go.sum
COPY *.go /build/sdk/

# Copy test app code
COPY test-app/go.mod /build/test-app/go.mod
COPY test-app/main.go /build/test-app/main.go

# Build test app (replace directive will use local SDK)
WORKDIR /build/test-app
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o app main.go

# Final stage - minimal runtime image
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/test-app/app /app/app

# Expose port
EXPOSE 8080

# Start the server
CMD ["/app/app"]
