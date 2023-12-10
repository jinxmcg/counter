# Start from the official Golang image
FROM golang:1.21 as base

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy the go.mod and go.sum files first to leverage Docker cache.
# This layer will only be rebuilt when go.mod or go.sum files change.
COPY go.mod go.sum ./

# Download dependencies - they will be cached if the go.mod and go.sum files do not change.
RUN go mod download

# Copy the rest of the source code.
COPY . .

# Test stage
FROM base as tester
RUN CGO_ENABLED=0 GOOS=linux go test ./...

# Build stage
FROM base as builder
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest  
RUN apk --no-cache add ca-certificates

# Create a non-root user and switch to it
RUN adduser -D nonroot
USER nonroot

WORKDIR /home/nonroot

# Copy the Pre-built binary file from the build stage and rename it to 'counter'
COPY --from=builder /app/main ./counter

# Expose port 8080 to the outside world
EXPOSE 8080

# Command to run the executable
CMD ["./counter"]