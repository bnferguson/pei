package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
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

func main() {
	// Initialize logging first
	initLogger()

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
		slog.Error("pei must be run with root privileges")
		os.Exit(1)
	}

	// Load configuration for daemon
	config, err := loadConfig(*configPath)
	if err != nil {
		slog.Error("Failed to load configuration",
			"config_path", *configPath,
			"error", err)
		os.Exit(1)
	}

	// Set up app user/group
	appUser := os.Getenv("PEI_APP_USER")
	appGroup := os.Getenv("PEI_APP_GROUP")
	if appUser == "" {
		appUser = "appuser" // default
	}
	if appGroup == "" {
		appGroup = "appuser" // default
	}

	// Create and start the daemon
	daemon := NewDaemon(config, appUser, appGroup)
	ctx := context.Background()
	if err := daemon.Start(ctx); err != nil {
		slog.Error("Daemon failed to start", "error", err)
		os.Exit(1)
	}
}
