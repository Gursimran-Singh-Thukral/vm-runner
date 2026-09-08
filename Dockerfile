# Stage 1: Build the Go application
FROM golang:alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum first to leverage Docker cache
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o vm-runner-server ./cmd/server

# Stage 2: Create the final runtime image
FROM alpine:latest

# Install QEMU and required dependencies
RUN apk add --no-cache \
    qemu-system-x86_64 \
    qemu-img \
    bash

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/vm-runner-server .

# Copy the web directory since the server serves static files from ./web
COPY --from=builder /app/web ./web

# Copy the initial data directory (CTF JSON configs)
COPY --from=builder /app/data ./data

# Copy ISOs
COPY --from=builder /app/isos ./isos

# Expose the web server port
EXPOSE 8080

# Run the server
CMD ["./vm-runner-server"]
