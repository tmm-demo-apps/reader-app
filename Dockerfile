# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install git for go mod download (use HTTP for apk to avoid TLS issues on corporate networks)
RUN sed -i 's/https/http/' /etc/apk/repositories && \
    apk add --no-cache git && \
    sed -i 's/http/https/' /etc/apk/repositories

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -o reader ./cmd/web

# Final stage
FROM alpine:3.19

WORKDIR /app

# Install ca-certificates for HTTPS requests (use HTTP for apk to avoid TLS issues on corporate networks)
RUN sed -i 's/https/http/' /etc/apk/repositories && \
    apk add --no-cache ca-certificates tzdata && \
    sed -i 's/http/https/' /etc/apk/repositories

# Copy binary from builder
COPY --from=builder /app/reader .

# Copy templates and static files
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static ./static
COPY --from=builder /app/migrations ./migrations

# Create non-root user
RUN adduser -D -u 1000 appuser
USER appuser

EXPOSE 8081

CMD ["./reader"]
