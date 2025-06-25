package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"syscall"
)

// IPCRequest represents a request sent to the daemon
type IPCRequest struct {
	Command string `json:"command"`
	Service string `json:"service,omitempty"`
	Signal  string `json:"signal,omitempty"`
}

// IPCResponse represents a response from the daemon
type IPCResponse struct {
	Success  bool                      `json:"success"`
	Message  string                    `json:"message,omitempty"`
	Services map[string]*ServiceStatus `json:"services,omitempty"`
	Service  *ServiceStatus            `json:"service,omitempty"`
}

const (
	SocketPath = "/tmp/pei.sock"
)

func handleIPCRequest(conn net.Conn, daemon *Daemon) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var req IPCRequest
	if err := decoder.Decode(&req); err != nil {
		response := IPCResponse{Success: false, Message: "Invalid request format"}
		if err := encoder.Encode(response); err != nil {
			slog.Error("Failed to encode IPC response", "error", err)
		}
		return
	}

	var response IPCResponse

	switch req.Command {
	case "list":
		response = IPCResponse{
			Success:  true,
			Services: daemon.getAllServiceStatus(),
		}
	case "status":
		if req.Service == "" {
			response = IPCResponse{
				Success:  true,
				Services: daemon.getAllServiceStatus(),
			}
		} else {
			if status, exists := daemon.getServiceStatus(req.Service); exists {
				response = IPCResponse{
					Success: true,
					Service: status,
				}
			} else {
				response = IPCResponse{
					Success: false,
					Message: fmt.Sprintf("Service '%s' not found", req.Service),
				}
			}
		}
	case "restart":
		if req.Service == "" {
			response = IPCResponse{Success: false, Message: "Service name required"}
		} else if svc, exists := daemon.config.Services[req.Service]; exists {
			// Send restart request
			select {
			case daemon.restartChan <- svc:
				response = IPCResponse{
					Success: true,
					Message: fmt.Sprintf("Restart requested for service '%s'", req.Service),
				}
			default:
				response = IPCResponse{
					Success: false,
					Message: "Restart channel is full, try again later",
				}
			}
		} else {
			response = IPCResponse{
				Success: false,
				Message: fmt.Sprintf("Service '%s' not found", req.Service),
			}
		}
	case "signal":
		if req.Service == "" || req.Signal == "" {
			response = IPCResponse{Success: false, Message: "Service name and signal required"}
		} else if cmd, exists := daemon.getServiceCmd(req.Service); exists && cmd.Process != nil {
			// Parse signal
			var sig syscall.Signal
			var validSignal bool = true
			switch req.Signal {
			case "HUP", "SIGHUP":
				sig = syscall.SIGHUP
			case "TERM", "SIGTERM":
				sig = syscall.SIGTERM
			case "KILL", "SIGKILL":
				sig = syscall.SIGKILL
			case "USR1", "SIGUSR1":
				sig = syscall.SIGUSR1
			case "USR2", "SIGUSR2":
				sig = syscall.SIGUSR2
			default:
				response = IPCResponse{
					Success: false,
					Message: fmt.Sprintf("Unsupported signal: %s", req.Signal),
				}
				validSignal = false
			}

			if validSignal {
				// Elevate privileges to send signal to process running as different user
				if err := elevatePrivileges(); err != nil {
					response = IPCResponse{
						Success: false,
						Message: fmt.Sprintf("Failed to elevate privileges for signal: %v", err),
					}
				} else {
					// Send the signal
					signalErr := cmd.Process.Signal(sig)

					// Drop privileges back down
					if dropErr := dropPrivileges(daemon.appUser, daemon.appGroup); dropErr != nil {
						log.Printf("Failed to drop privileges after signal: %v", dropErr)
					}

					if signalErr != nil {
						response = IPCResponse{
							Success: false,
							Message: fmt.Sprintf("Failed to send signal: %v", signalErr),
						}
					} else {
						response = IPCResponse{
							Success: true,
							Message: fmt.Sprintf("Signal %s sent to service '%s'", req.Signal, req.Service),
						}
					}
				}
			}
		} else {
			response = IPCResponse{
				Success: false,
				Message: fmt.Sprintf("Service '%s' not running", req.Service),
			}
		}
	default:
		response = IPCResponse{
			Success: false,
			Message: fmt.Sprintf("Unknown command: %s", req.Command),
		}
	}

	if err := encoder.Encode(response); err != nil {
		slog.Error("Failed to encode IPC response", "error", err)
	}
}

func startIPCServer(daemon *Daemon) {
	// Remove existing socket if it exists
	os.Remove(SocketPath)

	listener, err := net.Listen("unix", SocketPath)
	if err != nil {
		log.Printf("Failed to create IPC socket: %v", err)
		return
	}

	log.Printf("IPC server listening on %s", SocketPath)

	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("IPC accept error: %v", err)
				continue
			}
			go handleIPCRequest(conn, daemon)
		}
	}()
}

func sendIPCRequest(req IPCRequest) (*IPCResponse, error) {
	conn, err := net.Dial("unix", SocketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to pei daemon: %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}

	var response IPCResponse
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return &response, nil
}
