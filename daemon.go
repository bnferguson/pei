package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// ServiceStatus represents the current status of a service
type ServiceStatus struct {
	Name      string    `json:"name"`
	Running   bool      `json:"running"`
	PID       int       `json:"pid"`
	StartTime time.Time `json:"start_time"`
	Restarts  int       `json:"restarts"`
}

// Daemon represents the main pei daemon that manages services
type Daemon struct {
	config *Config

	// Service management
	serviceCmds    map[string]*exec.Cmd
	serviceStatus  map[string]*ServiceStatus
	serviceOutputs map[string]*ServiceOutputCapture
	restartChan    chan Service

	// Synchronization
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	// Privilege management
	appUser  string
	appGroup string
}

// NewDaemon creates a new daemon instance
func NewDaemon(config *Config, appUser, appGroup string) *Daemon {
	ctx, cancel := context.WithCancel(context.Background())

	return &Daemon{
		config:         config,
		serviceCmds:    make(map[string]*exec.Cmd),
		serviceStatus:  make(map[string]*ServiceStatus),
		serviceOutputs: make(map[string]*ServiceOutputCapture),
		restartChan:    make(chan Service, 100),
		ctx:            ctx,
		cancel:         cancel,
		appUser:        appUser,
		appGroup:       appGroup,
	}
}

// Start starts the daemon and all its services
func (d *Daemon) Start(ctx context.Context) error {
	// Start IPC server
	go startIPCServer(d)

	// Start each service
	for name, svc := range d.config.Services {
		logServiceInfo(name, "Starting service")
		if err := d.startService(svc); err != nil {
			continue
		}
	}

	// Start service manager
	go d.serviceManager(ctx)

	// Start global reaper
	go d.globalReaper(ctx)

	// Drop privileges after starting services
	if err := dropPrivileges(d.appUser, d.appGroup); err != nil {
		return err
	}
	slog.Info("Dropped privileges", "user", d.appUser, "group", d.appGroup)

	// Handle signals
	return d.handleSignals(ctx)
}

// Stop gracefully stops the daemon
func (d *Daemon) Stop() {
	d.cancel()
	d.shutdownServices()
}

// handleSignals manages signal handling for the daemon
func (d *Daemon) handleSignals(ctx context.Context) error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP,
		syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGPIPE,
		syscall.SIGQUIT, syscall.SIGCHLD)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sig := <-sigChan:
			slog.Info("Received signal", "signal", sig.String())

			switch sig {
			case syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT:
				slog.Info("Initiating graceful shutdown", "signal", sig.String())
				d.shutdownServices()
				return nil
			case syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGUSR2:
				slog.Info("Forwarding signal to all services", "signal", sig.String())
				d.forwardSignalToServices(sig.(syscall.Signal))
			case syscall.SIGCHLD:
				slog.Debug("Received SIGCHLD (handled by reaper)")
			case syscall.SIGPIPE:
				slog.Debug("Received SIGPIPE (ignored)")
			default:
				slog.Warn("Received unhandled signal", "signal", sig.String())
			}
		}
	}
}

// getServiceCmd safely gets a service command
func (d *Daemon) getServiceCmd(name string) (*exec.Cmd, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	cmd, exists := d.serviceCmds[name]
	return cmd, exists
}

// setServiceCmd safely sets a service command
func (d *Daemon) setServiceCmd(name string, cmd *exec.Cmd) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.serviceCmds[name] = cmd
}

// getServiceStatus safely gets service status
func (d *Daemon) getServiceStatus(name string) (*ServiceStatus, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	status, exists := d.serviceStatus[name]
	return status, exists
}

// setServiceStatus safely sets service status
func (d *Daemon) setServiceStatus(name string, status *ServiceStatus) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.serviceStatus[name] = status
}

// getAllServiceCmds safely gets all service commands
func (d *Daemon) getAllServiceCmds() map[string]*exec.Cmd {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make(map[string]*exec.Cmd)
	for name, cmd := range d.serviceCmds {
		result[name] = cmd
	}
	return result
}

// getAllServiceStatus safely gets all service status
func (d *Daemon) getAllServiceStatus() map[string]*ServiceStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make(map[string]*ServiceStatus)
	for name, status := range d.serviceStatus {
		result[name] = status
	}
	return result
}

// setServiceOutput safely sets service output capture
func (d *Daemon) setServiceOutput(name string, output *ServiceOutputCapture) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.serviceOutputs[name] = output
}

// startServiceOutputCapture sets up output capture for a service
func (d *Daemon) startServiceOutputCapture(service Service, stdoutPipe, stderrPipe io.ReadCloser, pid int) *ServiceOutputCapture {
	capture := NewServiceOutputCapture(service, stdoutPipe, stderrPipe, pid)
	d.setServiceOutput(service.Name, capture)
	capture.Start()
	return capture
}

// stopServiceOutputCapture stops output capture for a service
func (d *Daemon) stopServiceOutputCapture(serviceName string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if capture, exists := d.serviceOutputs[serviceName]; exists {
		capture.Stop()
		delete(d.serviceOutputs, serviceName)
	}
}

// stopAllServiceOutputCaptures stops all service output captures
func (d *Daemon) stopAllServiceOutputCaptures() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for serviceName, capture := range d.serviceOutputs {
		capture.Stop()
		delete(d.serviceOutputs, serviceName)
	}
}

// startService starts a single service with proper privilege management
func (d *Daemon) startService(svc Service) error {
	uid, gid, err := lookupUIDGID(svc.User, svc.Group)
	if err != nil {
		logServiceError(svc.Name, "Failed to look up user/group", "error", err)
		return err
	}

	cmd := exec.Command(svc.Command[0], svc.Command[1:]...)

	// Set working directory if specified
	if svc.WorkingDir != "" {
		cmd.Dir = svc.WorkingDir
	}

	// Set environment variables
	if len(svc.Environment) > 0 {
		env := os.Environ()
		for k, v := range svc.Environment {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}

	// Set up pipes to capture service output
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		logServiceError(svc.Name, "Failed to create stdout pipe", "error", err)
		return err
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		logServiceError(svc.Name, "Failed to create stderr pipe", "error", err)
		return err
	}

	// Set process credentials
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
	}

	serviceLogger := getLogger("service")
	serviceLogger.Info("Starting service",
		"service", svc.Name,
		"user", svc.User,
		"group", svc.Group,
		"uid", uid,
		"gid", gid)

	if err := cmd.Start(); err != nil {
		logServiceError(svc.Name, "Failed to start", "error", err)
		return err
	}

	d.setServiceCmd(svc.Name, cmd)

	// Start capturing service output
	d.startServiceOutputCapture(svc, stdoutPipe, stderrPipe, cmd.Process.Pid)

	// Initialize service status
	d.setServiceStatus(svc.Name, &ServiceStatus{
		Name:      svc.Name,
		Running:   true,
		PID:       cmd.Process.Pid,
		StartTime: time.Now(),
		Restarts:  0,
	})

	// Start the service monitor goroutine
	go d.monitorService(svc, cmd)

	return nil
}

// monitorService monitors a service and requests restarts when needed
func (d *Daemon) monitorService(svc Service, cmd *exec.Cmd) {
	// Wait for the service to exit
	err := cmd.Wait()

	// Stop capturing output for this service
	d.stopServiceOutputCapture(svc.Name)

	// Update service status to not running
	if status, exists := d.getServiceStatus(svc.Name); exists {
		status.Running = false
	}

	// For oneshot services, handle differently
	if svc.Oneshot {
		if svc.Interval > 0 {
			monitorLogger := getLogger("monitor")
			monitorLogger.Info("Oneshot service completed, scheduling next run",
				"service", svc.Name,
				"interval", svc.Interval.String())
			time.Sleep(svc.Interval)
			// Request a restart through the service manager
			select {
			case d.restartChan <- svc:
			case <-d.ctx.Done():
				return // Daemon is shutting down
			}
		} else {
			logServiceInfo(svc.Name, "Oneshot service completed, no interval specified")
		}
		return
	}

	// For non-oneshot services, check if we should restart
	shouldRestart := false
	monitorLogger := getLogger("monitor")
	if err != nil {
		monitorLogger.Info("Service exited with error",
			"service", svc.Name,
			"error", err)
		if svc.Restart == RestartAlways || svc.Restart == RestartOnFailure {
			shouldRestart = true
		}
	} else {
		logServiceInfo(svc.Name, "Service exited successfully")
		if svc.Restart == RestartAlways {
			shouldRestart = true
		}
	}

	// Check if we should restart and haven't exceeded limits
	if shouldRestart {
		// Update restart count in status
		if status, exists := d.getServiceStatus(svc.Name); exists {
			if svc.MaxRestarts > 0 && status.Restarts >= svc.MaxRestarts {
				monitorLogger.Info("Service exceeded max restarts, giving up",
					"service", svc.Name,
					"max_restarts", svc.MaxRestarts,
					"restart_count", status.Restarts)
				return
			}
			status.Restarts++
		}

		// Wait for restart delay
		time.Sleep(svc.RestartDelay)
		// Request a restart through the service manager
		select {
		case d.restartChan <- svc:
		case <-d.ctx.Done():
			return // Daemon is shutting down
		}
	}
}

// serviceManager handles service restarts with proper privilege management
func (d *Daemon) serviceManager(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case svc := <-d.restartChan:
			// Elevate privileges before starting the service
			if err := elevatePrivileges(); err != nil {
				logServiceError(svc.Name, "Failed to elevate privileges for restart", "error", err)
				continue
			}

			uid, gid, err := lookupUIDGID(svc.User, svc.Group)
			if err != nil {
				logServiceError(svc.Name, "Failed to look up user/group for restart", "error", err)
				if dropErr := dropPrivileges(d.appUser, d.appGroup); dropErr != nil {
					logServiceError(svc.Name, "Failed to drop privileges after error", "error", dropErr)
				}
				continue
			}

			cmd := exec.Command(svc.Command[0], svc.Command[1:]...)

			// Set working directory if specified
			if svc.WorkingDir != "" {
				cmd.Dir = svc.WorkingDir
			}

			// Set environment variables
			if len(svc.Environment) > 0 {
				env := os.Environ()
				for k, v := range svc.Environment {
					env = append(env, k+"="+v)
				}
				cmd.Env = env
			}

			// Set up pipes to capture service output for restarted services
			stdoutPipe, err := cmd.StdoutPipe()
			if err != nil {
				logServiceError(svc.Name, "Failed to create stdout pipe for restart", "error", err)
				if dropErr := dropPrivileges(d.appUser, d.appGroup); dropErr != nil {
					logServiceError(svc.Name, "Failed to drop privileges after error", "error", dropErr)
				}
				continue
			}

			stderrPipe, err := cmd.StderrPipe()
			if err != nil {
				logServiceError(svc.Name, "Failed to create stderr pipe for restart", "error", err)
				if dropErr := dropPrivileges(d.appUser, d.appGroup); dropErr != nil {
					logServiceError(svc.Name, "Failed to drop privileges after error", "error", dropErr)
				}
				continue
			}

			// Set process credentials
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Credential: &syscall.Credential{
					Uid: uint32(uid),
					Gid: uint32(gid),
				},
			}

			serviceLogger := getLogger("service")
			serviceLogger.Info("Restarting service",
				"service", svc.Name,
				"user", svc.User,
				"group", svc.Group,
				"uid", uid,
				"gid", gid)

			if err := cmd.Start(); err != nil {
				logServiceError(svc.Name, "Failed to restart", "error", err)
				if dropErr := dropPrivileges(d.appUser, d.appGroup); dropErr != nil {
					logServiceError(svc.Name, "Failed to drop privileges after error", "error", dropErr)
				}
				continue
			}

			// Update the service command in our tracking map
			d.setServiceCmd(svc.Name, cmd)

			// Start capturing service output for restarted service
			d.startServiceOutputCapture(svc, stdoutPipe, stderrPipe, cmd.Process.Pid)

			// Update service status
			if status, exists := d.getServiceStatus(svc.Name); exists {
				status.Running = true
				status.PID = cmd.Process.Pid
				status.StartTime = time.Now()
			} else {
				d.setServiceStatus(svc.Name, &ServiceStatus{
					Name:      svc.Name,
					Running:   true,
					PID:       cmd.Process.Pid,
					StartTime: time.Now(),
					Restarts:  0,
				})
			}

			// Drop privileges after starting the service
			if err := dropPrivileges(d.appUser, d.appGroup); err != nil {
				logServiceError(svc.Name, "Failed to drop privileges after restart", "error", err)
				continue
			}

			// Start monitoring the new process
			go d.monitorService(svc, cmd)
		}
	}
}

// globalReaper reaps orphaned/zombie child processes
func (d *Daemon) globalReaper(ctx context.Context) {
	reaperLogger := getLogger("reaper")
	for {
		select {
		case <-ctx.Done():
			return
		default:
			var ws syscall.WaitStatus
			var ru syscall.Rusage
			pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, &ru)
			if pid > 0 {
				reaperLogger.Info("Reaped orphaned child process", "pid", pid)
			}
			if err != nil && err != syscall.ECHILD {
				reaperLogger.Error("Error in global reaper", "error", err)
			}
			time.Sleep(1 * time.Second)
		}
	}
}

// forwardSignalToServices forwards a signal to all running services
func (d *Daemon) forwardSignalToServices(signal syscall.Signal) {
	signalLogger := getLogger("signal")

	// Elevate privileges to send signals to processes running as different users
	if err := elevatePrivileges(); err != nil {
		signalLogger.Error("Failed to elevate privileges for signal forwarding", "error", err)
		return
	}
	defer func() {
		if err := dropPrivileges(d.appUser, d.appGroup); err != nil {
			signalLogger.Error("Failed to drop privileges after signal forwarding", "error", err)
		}
	}()

	for name, cmd := range d.getAllServiceCmds() {
		if cmd != nil && cmd.Process != nil {
			signalLogger.Info("Forwarding signal to service",
				"signal", signal.String(),
				"service", name,
				"pid", cmd.Process.Pid)
			if err := cmd.Process.Signal(signal); err != nil {
				signalLogger.Error("Failed to send signal to service",
					"signal", signal.String(),
					"service", name,
					"error", err)
			}
		}
	}
}

// shutdownServices implements graceful shutdown of all services
func (d *Daemon) shutdownServices() {
	shutdownLogger := getLogger("shutdown")
	shutdownLogger.Info("Starting graceful shutdown of all services")

	// Cleanup IPC socket first
	os.Remove(SocketPath)

	// Stop all service output captures
	d.stopAllServiceOutputCaptures()

	// Elevate privileges for service management
	if err := elevatePrivileges(); err != nil {
		shutdownLogger.Error("Failed to elevate privileges for shutdown", "error", err)
		return
	}

	// First, send SIGTERM to all services
	var runningServices []*exec.Cmd
	for name, cmd := range d.getAllServiceCmds() {
		if cmd != nil && cmd.Process != nil {
			shutdownLogger.Info("Sending SIGTERM to service", "service", name, "pid", cmd.Process.Pid)
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				shutdownLogger.Error("Failed to send SIGTERM to service", "service", name, "error", err)
			} else {
				runningServices = append(runningServices, cmd)
			}
		}
	}

	// Wait for services to shutdown gracefully (with timeout)
	shutdownTimeout := 30 * time.Second
	shutdownLogger.Info("Waiting for services to shutdown gracefully", "timeout_seconds", int(shutdownTimeout.Seconds()))

	done := make(chan bool, 1)
	go func() {
		var wg sync.WaitGroup
		for _, cmd := range runningServices {
			wg.Add(1)
			go func(c *exec.Cmd) {
				defer wg.Done()
				_ = c.Wait() // Ignore error since we're shutting down
			}(cmd)
		}
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		shutdownLogger.Info("All services shutdown gracefully")
	case <-time.After(shutdownTimeout):
		shutdownLogger.Warn("Timeout reached, force killing remaining services")

		// Force kill any remaining services
		for name, cmd := range d.getAllServiceCmds() {
			if cmd != nil && cmd.Process != nil {
				shutdownLogger.Info("Force killing service", "service", name, "pid", cmd.Process.Pid)
				if err := cmd.Process.Kill(); err != nil {
					shutdownLogger.Error("Failed to force kill service", "service", name, "error", err)
				}
			}
		}

		// Give a short time for force kills to complete
		time.Sleep(2 * time.Second)
	}

	shutdownLogger.Info("Service shutdown complete")
}
