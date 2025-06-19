#!/bin/sh

# Signal handler service that logs received signals
# This demonstrates signal handling in pei-managed services

echo "Signal handler service starting (PID: $$)"

# Signal handlers
handle_hup() {
    echo "$(date): Received SIGHUP"
}

handle_usr1() {
    echo "$(date): Received SIGUSR1"
}

handle_usr2() {
    echo "$(date): Received SIGUSR2"
}

handle_term() {
    echo "$(date): Received SIGTERM, shutting down gracefully"
    exit 0
}

# Trap signals
trap handle_hup HUP
trap handle_usr1 USR1
trap handle_usr2 USR2
trap handle_term TERM

echo "$(date): Signal handlers installed, waiting for signals..."

# Keep the service running
counter=0
while true; do
    echo "$(date): Heartbeat $counter (PID: $$)"
    counter=$((counter + 1))
    sleep 10
done