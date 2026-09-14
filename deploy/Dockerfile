# Multi-stage build for minimal image size.
# Stage 1: Build the Go binary.
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

# Cache dependency downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o /benchmark ./cmd/benchmark

# Stage 2: Minimal runtime image.
FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /benchmark /usr/local/bin/benchmark

# Default config location.
COPY configs/ /etc/benchmark/

ENTRYPOINT ["benchmark"]
CMD ["run", "--config", "/etc/benchmark/example.yaml"]
