# --- Build Stage ---
# Use the official Golang Alpine image as a builder for a smaller build environment.
FROM golang:1.25-alpine AS builder

# Set the working directory inside the container.
WORKDIR /app

# Copy go.mod and go.sum files to download dependencies first.
# This leverages Docker's layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code.
COPY . .

# Build the Go application as a static binary.
# CGO_ENABLED=0 is crucial for building a static binary that can run in a scratch image.
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /api-gateway .

# --- Final Stage ---
# Use a minimal 'scratch' base image for the final container. It contains nothing but your application.
FROM scratch

# Copy the static binary from the builder stage.
COPY --from=builder /api-gateway /api-gateway

# Expose the port the application runs on.
EXPOSE 8080

# Command to run the executable.
ENTRYPOINT ["/api-gateway"]

