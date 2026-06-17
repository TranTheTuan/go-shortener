# Build Stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy dependency manifests and install (go.sum is optional for std-lib-only builds)
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o ./build/main ./cmd/server

# Run Stage
FROM alpine:latest

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/build/main .

EXPOSE 8080

CMD ["./main"]