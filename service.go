package main

import (
	"log"
	"os"
	"os/exec"
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

// Global variables for service management
var (
	// Channel to coordinate service restarts
	restartChan = make(chan Service, 100)
	// Map to track all service commands
	serviceCmds = make(map[string]*exec.Cmd)
	// Map to track service status
	serviceStatus = make(map[string]*ServiceStatus)
)

func startService(svc Service) error {
	uid, gid, err := lookupUIDGID(svc.User, svc.Group)
	if err != nil {
		log.Printf("Failed to look up user/group for service %s: %v", svc.Name, err)
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

	// Handle stdout/stderr redirection
	if svc.Stdout != "" {
		stdout, err := os.OpenFile(svc.Stdout, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("Failed to open stdout file for service %s: %v", svc.Name, err)
			return err
		}
		cmd.Stdout = stdout
	} else {
		cmd.Stdout = os.Stdout
	}

	if svc.Stderr != "" {
		stderr, err := os.OpenFile(svc.Stderr, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("Failed to open stderr file for service %s: %v", svc.Name, err)
			return err
		}
		cmd.Stderr = stderr
	} else {
		cmd.Stderr = os.Stderr
	}

	// Set process credentials
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
	}

	log.Printf("Starting service %s as %s:%s (uid=%d, gid=%d)", svc.Name, svc.User, svc.Group, uid, gid)

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start service %s: %v", svc.Name, err)
		return err
	}

	serviceCmds[svc.Name] = cmd

	// Initialize service status
	serviceStatus[svc.Name] = &ServiceStatus{
		Name:      svc.Name,
		Running:   true,
		PID:       cmd.Process.Pid,
		StartTime: time.Now(),
		Restarts:  0,
	}

	// Start the service monitor goroutine
	go monitorService(svc, cmd)

	return nil
}

// monitorService monitors a service and requests restarts when needed
func monitorService(svc Service, cmd *exec.Cmd) {
	restartCount := 0
	for {
		// Wait for the service to exit
		err := cmd.Wait()

		// Update service status to not running
		if status, exists := serviceStatus[svc.Name]; exists {
			status.Running = false
		}

		// For oneshot services, handle differently
		if svc.Oneshot {
			if svc.Interval > 0 {
				log.Printf("Oneshot service %s completed, waiting %v before next run", svc.Name, svc.Interval)
				time.Sleep(svc.Interval)
				// Request a restart through the service manager
				restartChan <- svc
				return // Return after requesting restart, the new process will be monitored by a new goroutine
			}
			log.Printf("Oneshot service %s completed, no interval specified", svc.Name)
			return
		}

		// For non-oneshot services, check if we should restart
		shouldRestart := false
		if err != nil {
			log.Printf("Service %s exited with error: %v", svc.Name, err)
			if svc.Restart == RestartAlways || svc.Restart == RestartOnFailure {
				shouldRestart = true
			}
		} else {
			log.Printf("Service %s exited successfully", svc.Name)
			if svc.Restart == RestartAlways {
				shouldRestart = true
			}
		}

		// Check max restarts
		if shouldRestart {
			if svc.MaxRestarts > 0 && restartCount >= svc.MaxRestarts {
				log.Printf("Service %s exceeded max restarts (%d), giving up", svc.Name, svc.MaxRestarts)
				return
			}
			restartCount++

			// Update restart count in status
			if status, exists := serviceStatus[svc.Name]; exists {
				status.Restarts++
			}

			// Wait for restart delay
			time.Sleep(svc.RestartDelay)
			// Request a restart through the service manager
			restartChan <- svc
			return
		}
		return
	}
}

// serviceManager handles service restarts with proper privilege management
func serviceManager() {
	for svc := range restartChan {
		// Elevate privileges before starting the service
		if err := elevatePrivileges(); err != nil {
			log.Printf("Failed to elevate privileges for service %s: %v", svc.Name, err)
			continue
		}

		uid, gid, err := lookupUIDGID(svc.User, svc.Group)
		if err != nil {
			log.Printf("Failed to look up user/group for service %s: %v", svc.Name, err)
			dropPrivileges(appUser, appGroup)
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

		// Handle stdout/stderr redirection
		if svc.Stdout != "" {
			stdout, err := os.OpenFile(svc.Stdout, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				log.Printf("Failed to open stdout file for service %s: %v", svc.Name, err)
				dropPrivileges(appUser, appGroup)
				continue
			}
			cmd.Stdout = stdout
		} else {
			cmd.Stdout = os.Stdout
		}

		if svc.Stderr != "" {
			stderr, err := os.OpenFile(svc.Stderr, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				log.Printf("Failed to open stderr file for service %s: %v", svc.Name, err)
				dropPrivileges(appUser, appGroup)
				continue
			}
			cmd.Stderr = stderr
		} else {
			cmd.Stderr = os.Stderr
		}

		// Set process credentials
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: uint32(uid),
				Gid: uint32(gid),
			},
		}

		log.Printf("Restarting service %s as %s:%s (uid=%d, gid=%d)", svc.Name, svc.User, svc.Group, uid, gid)

		if err := cmd.Start(); err != nil {
			log.Printf("Failed to restart service %s: %v", svc.Name, err)
			dropPrivileges(appUser, appGroup)
			continue
		}

		// Update the service command in our tracking map
		serviceCmds[svc.Name] = cmd

		// Update service status
		if status, exists := serviceStatus[svc.Name]; exists {
			status.Running = true
			status.PID = cmd.Process.Pid
			status.StartTime = time.Now()
		} else {
			serviceStatus[svc.Name] = &ServiceStatus{
				Name:      svc.Name,
				Running:   true,
				PID:       cmd.Process.Pid,
				StartTime: time.Now(),
				Restarts:  0,
			}
		}

		// Drop privileges after starting the service
		if err := dropPrivileges(appUser, appGroup); err != nil {
			log.Printf("Failed to drop privileges after restarting service %s: %v", svc.Name, err)
			continue
		}

		// Start monitoring the new process
		go monitorService(svc, cmd)
	}
}
