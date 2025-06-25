# syntax=docker/dockerfile:1.7-labs
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY . .
RUN go build -o pei

# Build the zombie maker service
FROM alpine:latest AS zombie-builder
WORKDIR /app
COPY example/zombie-maker/ .
RUN apk add --no-cache gcc musl-dev make && \
    make && \
    chmod +x zombie_maker

FROM alpine:latest

# Create non-root users and groups
RUN adduser -D -u 1000 appuser \
    && adduser -D -u 1001 worker \
    && adduser -D -u 1002 monitor \
    && adduser -D -u 1003 zombie

# Copy the binary from builder
COPY --from=builder /app/pei /pei
COPY --from=zombie-builder /app/zombie_maker /usr/local/bin/zombie_maker
COPY example/signal-handler.sh /example/signal-handler.sh
COPY example/json-logger.sh /example/json-logger.sh

# Make sure the binary is owned by root and has setuid
RUN chown root:root /pei && \
    chmod u+s /pei && \
    chown zombie:zombie /usr/local/bin/zombie_maker && \
    chmod +x /example/signal-handler.sh && \
    chmod +x /example/json-logger.sh

# Switch to non-root user
USER appuser

# Use pei as the init system
ENTRYPOINT ["/pei"]
