# --- Build Stage ---
# Use the official Golang Alpine image as a builder for a smaller build environment.
FROM golang:1.25-alpine AS builder

# Set the working directory inside the container.
WORKDIR /app

# Copy go.mod and go.sum files to download dependencies first.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code.
COPY . .

# Build the Go application as a static binary.
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /notification-ms .

# --- Final Stage ---
# Use the minimal 'scratch' base image for the final container.
FROM scratch

# Copy the static binary from the builder stage.
COPY --from=builder /notification-ms /notification-ms

# Expose the port for gRPC communication.
EXPOSE 50053

# Command to run the executable.
ENTRYPOINT ["/notification-ms"]

