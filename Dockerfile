FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY . .
RUN go build -o pei

FROM alpine:latest

# Create a non-root user
RUN adduser -D -u 1000 appuser

# Copy the binary from builder
COPY --from=builder /app/pei /pei

# Make sure the binary is owned by root and has setuid
RUN chown root:root /pei && \
    chmod u+s /pei

# Switch to non-root user
USER appuser

# Use pei as the init system
ENTRYPOINT ["/pei"]