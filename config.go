package main

import (
	"gopkg.in/yaml.v3"
	"os"
	"time"
)

// RestartPolicy defines how a service should be restarted
type RestartPolicy string

const (
	RestartAlways    RestartPolicy = "always"
	RestartOnFailure RestartPolicy = "on-failure"
	RestartNever     RestartPolicy = "never"
)

// Service represents a managed service
type Service struct {
	Name         string            `yaml:"name"`
	Command      []string          `yaml:"command"`
	User         string            `yaml:"user"`
	Group        string            `yaml:"group"`
	WorkingDir   string            `yaml:"working_dir"`
	Environment  map[string]string `yaml:"environment"`
	RequiresRoot bool              `yaml:"requires_root"`
	Restart      RestartPolicy     `yaml:"restart"`
	MaxRestarts  int               `yaml:"max_restarts"`
	RestartDelay time.Duration     `yaml:"restart_delay"`
	DependsOn    []string          `yaml:"depends_on"`
	Stdout       string            `yaml:"stdout"`
	Stderr       string            `yaml:"stderr"`
	Interval     time.Duration     `yaml:"interval"`
	Oneshot      bool              `yaml:"oneshot"`
}

// Config represents the pei configuration
type Config struct {
	Version  string             `yaml:"version"`
	Services map[string]Service `yaml:"services"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// Set service names from map keys
	for name, svc := range config.Services {
		svc.Name = name
		config.Services[name] = svc
	}

	return &config, nil
}
