package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

func formatUptime(startTime time.Time) string {
	uptime := time.Since(startTime)
	if uptime.Hours() >= 24 {
		days := int(uptime.Hours() / 24)
		hours := int(uptime.Hours()) % 24
		return fmt.Sprintf("%dd%dh", days, hours)
	} else if uptime.Hours() >= 1 {
		return fmt.Sprintf("%.0fh%.0fm", uptime.Hours(), uptime.Minutes())
	} else if uptime.Minutes() >= 1 {
		return fmt.Sprintf("%.0fm", uptime.Minutes())
	} else {
		return fmt.Sprintf("%.0fs", uptime.Seconds())
	}
}

func listServicesIPC() error {
	resp, err := sendIPCRequest(IPCRequest{Command: "list"})
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("daemon error: %s", resp.Message)
	}

	fmt.Printf("%-20s %-10s %-8s %-12s %-10s\n", "NAME", "STATUS", "PID", "RESTARTS", "UPTIME")
	fmt.Printf("%-20s %-10s %-8s %-12s %-10s\n", "----", "------", "---", "--------", "------")

	for name, status := range resp.Services {
		statusStr := "stopped"
		pidStr := "-"
		uptimeStr := "-"

		if status.Running {
			statusStr = "running"
			pidStr = fmt.Sprintf("%d", status.PID)
			uptimeStr = formatUptime(status.StartTime)
		}

		fmt.Printf("%-20s %-10s %-8s %-12d %-10s\n", name, statusStr, pidStr, status.Restarts, uptimeStr)
	}

	return nil
}

func listServices(config *Config) {
	fmt.Printf("%-20s %-10s %-8s %-12s %-10s\n", "NAME", "STATUS", "PID", "RESTARTS", "UPTIME")
	fmt.Printf("%-20s %-10s %-8s %-12s %-10s\n", "----", "------", "---", "--------", "------")

	for name := range config.Services {
		fmt.Printf("%-20s %-10s %-8s %-12s %-10s\n", name, "stopped", "-", "-", "-")
	}
}

func showServiceStatusIPC(serviceName string) error {
	resp, err := sendIPCRequest(IPCRequest{Command: "status", Service: serviceName})
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("daemon error: %s", resp.Message)
	}

	if serviceName == "" {
		// Show all services
		return listServicesIPC()
	}

	if resp.Service != nil {
		status := resp.Service
		fmt.Printf("Service: %s\n", status.Name)

		if status.Running {
			fmt.Printf("Status: running\n")
			fmt.Printf("PID: %d\n", status.PID)
			fmt.Printf("Started: %s\n", status.StartTime.Format(time.RFC3339))
			fmt.Printf("Uptime: %s\n", formatUptime(status.StartTime))
			fmt.Printf("Restarts: %d\n", status.Restarts)
		} else {
			fmt.Printf("Status: stopped\n")
		}
	}

	return nil
}

func showServiceStatus(config *Config, serviceName string) {
	if serviceName == "" {
		listServices(config)
		return
	}

	svc, exists := config.Services[serviceName]
	if !exists {
		fmt.Printf("Service '%s' not found\n", serviceName)
		return
	}

	fmt.Printf("Service: %s\n", serviceName)
	fmt.Printf("Command: %v\n", svc.Command)
	fmt.Printf("User: %s\n", svc.User)
	fmt.Printf("Group: %s\n", svc.Group)
	fmt.Printf("Restart Policy: %s\n", svc.Restart)
	fmt.Printf("Status: stopped\n")
}

func handleCLICommands(configPath *string, args []string) bool {
	// If no arguments provided, try to default to listing services from daemon
	if len(args) == 0 {
		if err := listServicesIPC(); err == nil {
			// Successfully connected to daemon and listed services
			return true
		}
		// Daemon not running, continue to PID 1 check below
		return false
	}

	command := args[0]

	switch command {
	case "list":
		if err := listServicesIPC(); err != nil {
			// Fallback to config-based listing if daemon is not running
			config, configErr := loadConfig(*configPath)
			if configErr != nil {
				log.Fatalf("Failed to connect to daemon and load config: %v, %v", err, configErr)
			}
			listServices(config)
		}
		return true

	case "status":
		serviceName := ""
		if len(args) > 1 {
			serviceName = args[1]
		}

		if err := showServiceStatusIPC(serviceName); err != nil {
			// For non-existent services, just show the daemon error
			if strings.Contains(err.Error(), "not found") {
				log.Fatalf("%v", err)
			}
			// Fallback to config-based status if daemon is not running
			config, configErr := loadConfig(*configPath)
			if configErr != nil {
				log.Fatalf("No pei daemon running - cannot show service status")
			}
			showServiceStatus(config, serviceName)
		}
		return true

	case "restart":
		if len(args) < 2 {
			log.Fatal("restart command requires a service name")
		}
		serviceName := args[1]

		resp, err := sendIPCRequest(IPCRequest{Command: "restart", Service: serviceName})
		if err != nil {
			log.Fatalf("No pei daemon running - cannot restart service")
		}
		if resp.Success {
			fmt.Println(resp.Message)
		} else {
			log.Fatalf("Restart failed: %s", resp.Message)
		}
		return true

	case "signal":
		if len(args) < 2 {
			log.Fatal("signal command requires service:signal format (e.g., echo:HUP)")
		}
		signalArg := args[1]

		parts := strings.Split(signalArg, ":")
		if len(parts) != 2 {
			log.Fatal("Signal format should be service:signal (e.g., echo:HUP)")
		}

		resp, err := sendIPCRequest(IPCRequest{Command: "signal", Service: parts[0], Signal: parts[1]})
		if err != nil {
			log.Fatalf("No pei daemon running - cannot send signal to service")
		}
		if resp.Success {
			fmt.Println(resp.Message)
		} else {
			log.Fatalf("Signal failed: %s", resp.Message)
		}
		return true

	default:
		fmt.Printf("Unknown command: %s\n", command)
		fmt.Println("Run 'pei help' for usage information")
		return true
	}
}
