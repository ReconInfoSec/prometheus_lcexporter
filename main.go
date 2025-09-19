package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ReconInfoSec/prometheus_lcexporter/collector"
	"github.com/ReconInfoSec/prometheus_lcexporter/config"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	if len(os.Args) < 3 {
		log.Fatal("Usage: exporter <config.yaml> <organizations.json>")
	}

	configPath := os.Args[1]
	orgsPath := os.Args[2]

	// Load configuration
	cfg, err := config.Load(configPath, orgsPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Validate configuration/set defaults
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}

	// Create and register collector
	lcCollector, err := collector.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}

	prometheus.MustRegister(lcCollector)

	// Start collector
	if err := lcCollector.Start(); err != nil {
		log.Fatalf("Failed to start collector: %v", err)
	}

	// Start HTTP server
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("Starting server on %s", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}
