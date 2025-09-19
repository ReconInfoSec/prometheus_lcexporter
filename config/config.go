package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the main configuration structure
type Config struct {
	Auth struct {
		URL     string            `yaml:"url"`
		Method  string            `yaml:"method"`
		Headers map[string]string `yaml:"headers"`
		UID     string            `yaml:"uid"`
		Secret  string            `yaml:"secret"`
	} `yaml:"auth"`

	Endpoints []struct {
		Name    string            `yaml:"name"`
		URL     string            `yaml:"url"`
		Method  string            `yaml:"method"`
		Headers map[string]string `yaml:"headers"`
		Metrics []MetricConfig    `yaml:"metrics"`
	} `yaml:"endpoints"`

	Server struct {
		Port     int `yaml:"port"`
		Interval int `yaml:"interval"` // seconds
	} `yaml:"server"`

	Organizations []Organization // will hold the org configs
}

// MetricConfig defines how to extract and create metrics from JSON responses
type MetricConfig struct {
	Name        string            `yaml:"name"`
	Help        string            `yaml:"help"`
	Type        string            `yaml:"type"` // gauge, counter, histogram, summary
	JSONPath    string            `yaml:"json_path"`
	Labels      map[string]string `yaml:"labels"`       // static labels
	LabelFields map[string]string `yaml:"label_fields"` // dynamic labels from JSON
}

// Organizations represents the organizations configuration
type Organization struct {
	ID                string `json:"oid"`
	Name              string `json:"name"`
	Jwt               string
	JwtMutex          sync.RWMutex
}

func Load(configPath, orgsPath string) (*Config, error) {
	var config Config
	var orgs []Organization

	// Load main configuration
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Load organizations configuration
	orgsData, err := os.ReadFile(orgsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read organizations file: %w", err)
	}

	if err := json.Unmarshal(orgsData, &orgs); err != nil {
		return nil, fmt.Errorf("failed to parse organizations: %w", err)
	}

	config.Organizations = orgs

	return &config, nil
}

func (c *Config) Validate() error {

	// validate all the authentication params and set defaults
	if c.Auth.URL == "" {
		c.Auth.URL = "https://jwt.limacharlie.io"
	}

	if c.Auth.Method == "" {
		c.Auth.Method = "POST"
	}

	if _, exists := c.Auth.Headers["Content-Type"]; !exists {
		c.Auth.Headers["Content-Type"] = "application/x-www-form-urlencoded"
	}

	if _, exists := c.Auth.Headers["Accept"]; !exists {
		c.Auth.Headers["Accept"] = "application/json"
	}

	if _, exists := c.Auth.Headers["User-Agent"]; !exists {
		c.Auth.Headers["User-Agent"] = "lcexporter"
	}

	if c.Auth.UID == "" {
		return fmt.Errorf("A LimaCharlie UID is required")
	}

	if c.Auth.Secret == "" {
		return fmt.Errorf("A LimaCharlie secret is required")
	}

	// set server defaults
	if c.Server.Port == 0 {
		c.Server.Port = 31337 // of course :-)
	}

	if c.Server.Interval == 0 {
		c.Server.Interval = 600
	}

	//validate orgs
	if len(c.Organizations) == 0 {
		return fmt.Errorf("At least one organization must be defined")
	}

	for o, organization := range c.Organizations {
		if organization.ID == "" {
			return fmt.Errorf("organization %i: oid is requred", o)
		}
		if organization.Name == "" {
			return fmt.Errorf("organization %i: name is requred", o)
		}
	}
	

	// validate endpoints
	if len(c.Endpoints) == 0 {
		return fmt.Errorf("At least one endpoint must be defined")
	}

	for e, endpoint := range c.Endpoints {
		if endpoint.URL == "" {
			return fmt.Errorf("endpoint %i: URL is required", e)
		}
		if endpoint.Name == "" {
			return fmt.Errorf("endpoint %i: Name is required", e)
		}
		if endpoint.Method == "" {
			endpoint.Method = "GET"
		}

		for m, metric := range endpoint.Metrics {
			if metric.Name == "" {
				return fmt.Errorf("endpoint %s, metric %d: name is required", endpoint.Name, m)
			}
			if metric.JSONPath == "" {
				return fmt.Errorf("endpoint %s, metric %s: json_path is required", endpoint.Name, metric.Name)
			}
			if strings.ToLower(metric.Type) != "gauge" && strings.ToLower(metric.Type) != "counter" {
				return fmt.Errorf("endpoint %s, metric %s: only gauge and counter types are supported", endpoint.Name, metric.Name)
			}
		}
	}

	// we made it
	return nil
}




