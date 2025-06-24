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

func showHelp() {
	fmt.Println("pei - Process management for containers")
	fmt.Println("\nUsage:")
	fmt.Println("  pei [command] [options]")
	fmt.Println("\nCommands:")
	fmt.Println("  list                      List all services and their status")
	fmt.Println("  status [service]          Show detailed status for service (or all if no service specified)")
	fmt.Println("  restart <service>         Restart a specific service")
	fmt.Println("  signal <service:signal>   Send signal to service")
	fmt.Println("  help                      Show this help")
	fmt.Println("\nGlobal Options:")
	fmt.Println("  -c <config>               Path to configuration file (default: pei.yaml)")
	fmt.Println("  -help                     Show this help")
	fmt.Println("\nSignals: HUP, TERM, KILL, USR1, USR2")
	fmt.Println("\nExamples:")
	fmt.Println("  pei list")
	fmt.Println("  pei status echo")
	fmt.Println("  pei restart echo")
	fmt.Println("  pei signal echo:HUP")
	fmt.Println("  pei -c /etc/pei.yaml list")
}

// Global variables for daemon state
var (
	appUser       string
	appGroup      string
	currentConfig *Config
)

func main() {
	// Parse global flags first
	configPath := flag.String("c", "pei.yaml", "path to configuration file")
	helpFlag := flag.Bool("help", false, "show help information")
	flag.Parse()

	// Get remaining arguments after flags
	args := flag.Args()

	if *helpFlag || (len(args) > 0 && args[0] == "help") {
		showHelp()
		return
	}

	// Handle CLI operations - returns true if any CLI command was executed
	if hasCommand := handleCLICommands(configPath, args); hasCommand {
		return
	}

	// Check if we're running as PID 1 for daemon mode
	if os.Getpid() != 1 {
		fmt.Println("No pei daemon running. Available commands:")
		fmt.Println("  pei list                    List all services and their status")
		fmt.Println("  pei status [service]        Show detailed status for service")
		fmt.Println("  pei restart <service>       Restart a specific service")
		fmt.Println("  pei signal <service:signal> Send signal to service")
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

	// Enhanced signal handling for init process
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, 
		syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP,
		syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGPIPE,
		syscall.SIGQUIT, syscall.SIGCHLD)

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

	// Signal handling loop
	for {
		sig := <-sigChan
		log.Printf("Received signal: %s", sig)
		
		switch sig {
		case syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT:
			log.Printf("Received shutdown signal %s, initiating graceful shutdown...", sig)
			shutdownServices()
			return
		case syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGUSR2:
			log.Printf("Forwarding signal %s to all services", sig)
			forwardSignalToServices(sig.(syscall.Signal))
		case syscall.SIGCHLD:
			// SIGCHLD is handled by the global reaper, just log it
			log.Printf("Received SIGCHLD (handled by reaper)")
		case syscall.SIGPIPE:
			// Ignore SIGPIPE - common in containers
			log.Printf("Received SIGPIPE (ignored)")
		default:
			log.Printf("Received unhandled signal: %s", sig)
		}
	}
}
