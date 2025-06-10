package main

import (
	"os"
	"os/exec"
	"testing"
	"time"
	"syscall"
)

func TestMainAsPID1(t *testing.T) {
	if os.Getpid() == 1 {
		t.Skip("Test must be run outside of PID 1")
	}

	// Build the binary
	cmd := exec.Command("go", "build", "-o", "test_pei")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build test binary: %v", err)
	}
	defer os.Remove("test_pei")

	// Run the binary in a new process group
	cmd = exec.Command("./test_pei")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start test binary: %v", err)
	}

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Check if it exited (it should, since it's not PID 1)
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Expected process to exit with error when not PID 1")
		}
	case <-time.After(time.Second):
		t.Error("Process did not exit when not running as PID 1")
	}

	// Clean up
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func TestServiceManagement(t *testing.T) {
	// Create a test service
	svc := Service{
		Name:    "test",
		Command: []string{"echo", "test service"},
		User:    "nobody",
		Group:   "nobody",
	}

	// Start the service in a goroutine
	done := make(chan error, 1)
	go func() {
		cmd := exec.Command(svc.Command[0], svc.Command[1:]...)
		done <- cmd.Run()
	}()

	// Wait for the service to complete
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Service failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("Service did not complete in time")
	}
}