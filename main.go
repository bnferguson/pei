package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Global variables for daemon state
var (
	appUser       string
	appGroup      string
	currentConfig *Config
)

func main() {
	// Parse command line flags
	configPath := flag.String("c", "pei.yaml", "path to configuration file")
	listFlag := flag.Bool("list", false, "list all services and their status")
	statusFlag := flag.String("status", "", "show detailed status for a specific service (or all if empty)")
	restartFlag := flag.String("restart", "", "restart a specific service")
	signalFlag := flag.String("signal", "", "send signal to service (format: service:signal)")
	helpFlag := flag.Bool("help", false, "show help information")
	flag.Parse()

	if *helpFlag {
		fmt.Println("pei - Process management for containers")
		fmt.Println("\nUsage:")
		fmt.Println("  pei [options]                 List all services (when daemon running)")
		fmt.Println("  pei -list                     List all services and their status")
		fmt.Println("  pei -status [service]         Show detailed status for service")
		fmt.Println("  pei -restart [service]        Restart a specific service")
		fmt.Println("  pei -signal [service:signal]  Send signal to service")
		fmt.Println("  pei -help                     Show this help")
		fmt.Println("\nSignals: HUP, TERM, KILL, USR1, USR2")
		fmt.Println("\nExample:")
		fmt.Println("  pei -restart echo")
		fmt.Println("  pei -signal echo:HUP")
		return
	}

	// Handle CLI operations - returns true if any CLI command was executed
	if hasFlags := handleCLICommands(configPath, listFlag, statusFlag, restartFlag, signalFlag); hasFlags {
		return
	}

	// Check if we're running as PID 1 for daemon mode
	if os.Getpid() != 1 {
		fmt.Println("No pei daemon running. Available commands:")
		fmt.Println("  --list                    List all services and their status")
		fmt.Println("  --status [service]        Show detailed status for service")
		fmt.Println("  --restart [service]       Restart a specific service")
		fmt.Println("  --signal [service:signal] Send signal to service")
		fmt.Println("\nTo run as daemon: pei must be run as PID 1")
		os.Exit(1)
	}

	// Check if we have root privileges (effective UID)
	if os.Geteuid() != 0 {
		log.Fatal("pei must be run with root privileges")
	}

	// Load configuration for daemon
	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	currentConfig = config

	// Start IPC server for CLI communication
	startIPCServer()

	// Set up app user/group
	appUser = os.Getenv("PEI_APP_USER")
	appGroup = os.Getenv("PEI_APP_GROUP")
	if appUser == "" {
		appUser = "appuser" // default
	}
	if appGroup == "" {
		appGroup = "appuser" // default
	}

	// Basic signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Start each service as the requested user/group
	// We'll start them sequentially to ensure proper initialization
	for name, svc := range currentConfig.Services {
		log.Printf("Starting service %s...", name)
		if err := startService(svc); err != nil {
			continue
		}
	}

	// Start the service manager goroutine that handles restarts
	go serviceManager()

	// Start a global reaper goroutine to reap all orphaned/zombie child processes
	go func() {
		for {
			var ws syscall.WaitStatus
			var ru syscall.Rusage
			pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, &ru)
			if pid > 0 {
				log.Printf("[reaper] Reaped orphaned child process PID %d", pid)
			}
			if err != nil && err != syscall.ECHILD {
				log.Printf("[reaper] Error in global reaper: %v", err)
			}
			time.Sleep(1 * time.Second)
		}
	}()

	// Now that all services are started, drop privileges
	if err := dropPrivileges(appUser, appGroup); err != nil {
		log.Fatalf("Failed to drop privileges: %v", err)
	}
	log.Printf("Dropped privileges to %s:%s", appUser, appGroup)

	// Wait for signals
	<-sigChan
	log.Println("Received shutdown signal, cleaning up...")

	// Cleanup IPC socket
	os.Remove(SocketPath)

	// Re-elevate privileges for cleanup
	if err := elevatePrivileges(); err != nil {
		log.Printf("Failed to elevate privileges for cleanup: %v", err)
	} else {
		// TODO: Implement proper service cleanup
		log.Println("Cleaning up services...")
		// Drop privileges again after cleanup
		if err := dropPrivileges(appUser, appGroup); err != nil {
			log.Printf("Failed to drop privileges after cleanup: %v", err)
		}
	}
}
