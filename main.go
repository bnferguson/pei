package main

import (
	"log"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"
)

// Service represents a managed service
type Service struct {
	Name    string   `yaml:"name"`
	Command []string `yaml:"command"`
	User    string   `yaml:"user"`
	Group   string   `yaml:"group"`
}

// Config represents the pei configuration
type Config struct {
	Version  string             `yaml:"version"`
	Services map[string]Service `yaml:"services"`
}

func lookupUIDGID(username, groupname string) (uid, gid int, err error) {
	u, err := user.Lookup(username)
	if err != nil {
		return 0, 0, err
	}
	g, err := user.LookupGroup(groupname)
	if err != nil {
		return 0, 0, err
	}
	uid64, err := strconv.ParseInt(u.Uid, 10, 32)
	if err != nil {
		return 0, 0, err
	}
	gid64, err := strconv.ParseInt(g.Gid, 10, 32)
	if err != nil {
		return 0, 0, err
	}
	return int(uid64), int(gid64), nil
}

func dropPrivileges(appUser, appGroup string) error {
	uid, gid, err := lookupUIDGID(appUser, appGroup)
	if err != nil {
		return err
	}
	if err := syscall.Setgid(gid); err != nil {
		return err
	}
	if err := syscall.Setuid(uid); err != nil {
		return err
	}
	return nil
}

func main() {
	// Check if we're running as PID 1
	if os.Getpid() != 1 {
		log.Fatal("pei must be run as PID 1")
	}

	// Basic signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// For now, we'll just start two test services
	services := []Service{
		{
			Name:    "echo",
			Command: []string{"sh", "-c", "while true; do echo 'echo service running'; sleep 5; done"},
			User:    "nobody",
			Group:   "nobody",
		},
		{
			Name:    "counter",
			Command: []string{"sh", "-c", "i=0; while true; do echo 'counter: $i'; i=$((i+1)); sleep 2; done"},
			User:    "nobody",
			Group:   "nobody",
		},
	}

	// Start each service as the requested user/group
	for _, svc := range services {
		go startServiceWithCreds(svc)
	}

	// Drop privileges in the main process to a safe user (e.g., appuser)
	appUser := os.Getenv("PEI_APP_USER")
	appGroup := os.Getenv("PEI_APP_GROUP")
	if appUser == "" {
		appUser = "appuser" // default
	}
	if appGroup == "" {
		appGroup = "appuser" // default
	}
	if err := dropPrivileges(appUser, appGroup); err != nil {
		log.Fatalf("Failed to drop privileges: %v", err)
	}
	log.Printf("Dropped privileges to %s:%s", appUser, appGroup)

	// Wait for signals
	<-sigChan
	log.Println("Received shutdown signal, cleaning up...")
	// TODO: Implement proper service cleanup
}

func startServiceWithCreds(svc Service) {
	for {
		uid, gid, err := lookupUIDGID(svc.User, svc.Group)
		if err != nil {
			log.Printf("Failed to look up user/group for service %s: %v", svc.Name, err)
			return
		}
		cmd := exec.Command(svc.Command[0], svc.Command[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: uint32(uid),
				Gid: uint32(gid),
			},
		}
		log.Printf("Starting service %s as %s:%s (uid=%d, gid=%d)", svc.Name, svc.User, svc.Group, uid, gid)
		if err := cmd.Start(); err != nil {
			log.Printf("Failed to start service %s: %v", svc.Name, err)
			return
		}
		if err := cmd.Wait(); err != nil {
			log.Printf("Service %s exited: %v", svc.Name, err)
			// TODO: Implement restart policy
			return
		}
	}
}