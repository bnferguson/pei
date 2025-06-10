# pei - Privilege Escalating Init

`pei` is an init (meant to be run as PID 1), that can run multiple processes not unlike `supervisord` or `systemd` but the difference is that it is designed to be run inside of a OCI container with default capabilities (so no adding a `--privledged` to your docker run). It also does not run as `root` but instead runs as a unprivledged user and relies on `setuid` to escalate to `root` only when tasks (or processes) require it.

Each process that this init starts and manages can run as a different user. `pei` will escalate to `root` to change to this user to start the process or manage the process (including killing it, etc).

Services/processes can write their logs to a tmpfs, but also stream them to stdout showing what service is generating the log.

You can describe the processes that will be managed by `pei` in a YAML file like so:

```yaml
# pei.yaml - Example configuration for managing services
version: "1.0"

# Define the services to manage
services:
  # A web server running as www-data user
  web_server:
    command: ["nginx", "-g", "daemon off;"]
    user: www-data
    group: www-data
    working_dir: /var/www/html
    environment:
      NODE_ENV: production
      LOG_LEVEL: info
      NGINX_HOST: localhost
      NGINX_PORT: 8080
    # Requires root to bind to privileged ports
    requires_root: true
    # Auto-restart if the service dies
    restart: always
    # Maximum number of restarts before giving up
    max_restarts: 5
    # Wait 5 seconds between restarts
    restart_delay: 5s

  # A background worker running as a custom user
  worker:
    command: ["python", "worker.py"]
    user: worker
    group: worker
    working_dir: /app/worker
    environment:
      NODE_ENV: production
      LOG_LEVEL: info
      REDIS_URL: redis://localhost:6379
    # Only restart on failure
    restart: on-failure
    # Optionally log output to a file - by default all goes to stdout
    stdout: /var/log/worker.log
    stderr: /var/log/worker.error.log

  # A monitoring service that needs root access
  monitor:
    command: ["monitor", "--config", "/etc/monitor/config.yaml"]
    user: monitor
    group: monitor
    environment:
      NODE_ENV: production
      LOG_LEVEL: info
    # This service needs root capabilities
    requires_root: true
    # Only start this service after web_server is running
    depends_on: ["web_server"]
    # Don't restart automatically
    restart: never

  # A simple health check service
  healthcheck:
    command: ["/bin/healthcheck.sh"]
    user: nobody
    group: nogroup
    environment:
      NODE_ENV: production
      LOG_LEVEL: info
    # Run every 30 seconds
    interval: 30s
    # Don't keep the service running, just execute periodically
    oneshot: true
```

This configuration demonstrates several key features of `pei`:

1. **Service Management**:
   - Each service can run as a different user
   - Services can have different working directories
   - Environment variables can be set per-service
   - Services can depend on other services

2. **Restart Policies**:
   - `always`: Always restart the service if it dies
   - `on-failure`: Only restart if the service exits with non-zero status
   - `never`: Don't restart the service
   - `oneshot`: Run the service once and don't keep it running

3. **Root Access**:
   - Services can request root access via `requires_root: true`
   - `pei` will handle privilege escalation only when needed

4. **Logging**:
   - Service output can be redirected to files
   - Environment variables for logging configuration
   - Logs are streamed to stdout with service identification

5. **Scheduling**:
   - Services can be scheduled to run at intervals
   - Dependencies between services can be specified

To use this configuration, save it as `pei.yaml` and run `pei` with:

```bash
pei -c pei.yaml
```

Note: Make sure all specified users and groups exist in the container, and that the necessary directories and files are accessible to the respective users.

## Reasoning

The idea behind `pei` is that many times you need to run multiple services inside the same container but still want to have some user separation. This lets us run as multiple users while being non-root and conforming to to CIS Docker standards (non-root, readonly filesystem, etc).

