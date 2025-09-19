package collector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/ReconInfoSec/prometheus_lcexporter/config"
)

// Exporter holds the exporter state
type LCCollector struct {
	config        *config.Config
	client        *http.Client
	gauges        map[string]*prometheus.GaugeVec
	counters      map[string]*prometheus.CounterVec
	counterValues map[string]map[string]float64
	counterMutex  sync.RWMutex
	tokenMutex sync.RWMutex
}

func New(cfg *config.Config) (*LCCollector, error) {

	collector := &LCCollector{
		config:        cfg,
		gauges:        make(map[string]*prometheus.GaugeVec),
		counters:      make(map[string]*prometheus.CounterVec),
		counterValues: make(map[string]map[string]float64),
		client:        &http.Client{Timeout: 30 * time.Second},
	}

	// Initialize metrics
	if err := collector.initMetrics(); err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	return collector, nil
}

// implements prometheus.Collector
func (c *LCCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, gauge := range c.gauges {
		gauge.Describe(ch)
	}
	for _, counter := range c.counters {
		counter.Describe(ch)
	}
}

// implements prometheus.Collector
func (c *LCCollector) Collect(ch chan<- prometheus.Metric) {
	for _, gauge := range c.gauges {
		gauge.Collect(ch)
	}
	for _, counter := range c.counters {
		counter.Collect(ch)
	}
}

// initMetrics initializes Prometheus metrics based on configuration
func (c *LCCollector) initMetrics() error {
	for _, endpoint := range c.config.Endpoints {
		for _, metricConfig := range endpoint.Metrics {
			// Create label names (static + dynamic)
			labelNames := make([]string, 0)

			// Add organization labels
			labelNames = append(labelNames, "org_id", "org_name")

			// Add static label names
			for key := range metricConfig.Labels {
				labelNames = append(labelNames, key)
			}

			// Add dynamic label names
			for key := range metricConfig.LabelFields {
				labelNames = append(labelNames, key)
			}

			switch strings.ToLower(metricConfig.Type) {
			case "gauge":
				metric := prometheus.NewGaugeVec(
					prometheus.GaugeOpts{
						Name: metricConfig.Name,
						Help: metricConfig.Help,
					},
					labelNames,
				)
				c.gauges[metricConfig.Name] = metric
			case "counter":
				metric := prometheus.NewCounterVec(
					prometheus.CounterOpts{
						Name: metricConfig.Name,
						Help: metricConfig.Help,
					},
					labelNames,
				)
				c.counters[metricConfig.Name] = metric
			default:
				return fmt.Errorf("Metric type %s not supported, currently only gauge and counter may be used", metricConfig.Type)
			}
		}
	}

	return nil
}

// getJWT obtains a JWT token from the auth endpoint
func (c *LCCollector) getJWT(org *config.Organization) error {
	// Prepare request body
	var bodyReader io.Reader
	vals := url.Values{}

	vals.Add("uid", c.config.Auth.UID)
	vals.Add("secret", c.config.Auth.Secret)
	vals.Add("oid", org.ID)

	bodyBytes := []byte(vals.Encode())
	bodyReader = bytes.NewReader(bodyBytes)

	// Create request
	req, err := http.NewRequest(c.config.Auth.Method, c.config.Auth.URL, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}

	// Set headers
	for key, value := range c.config.Auth.Headers {
		req.Header.Set(key, value)
	}

	// Make request
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth request failed with status: %d.", resp.StatusCode)
	}

	// Parse response
	var authResp map[string]interface{}

	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&authResp)
	if err != nil {
		return fmt.Errorf("failed to decode auth response: %w", err)
	}

	if decoder.More() {
		log.Printf("More fields detected in the auth response json")
	}

	token, ok := authResp["jwt"].(string)

	if !ok {
		return fmt.Errorf("token not found in response or not a string: %s", authResp)
	}
	
	// store the token
	org.JwtMutex.Lock()
	org.Jwt = token
	org.JwtMutex.Unlock()

	log.Printf("Successfully obtained JWT token for org %s, len: %d", org.ID, len(token))
	return nil
}

// makeAPICall makes an API call to an endpoint with JWT authentication
func (c *LCCollector) makeAPICall(endpoint string, org *config.Organization) (map[string]interface{}, error) {
	org.JwtMutex.RLock()
	jwt := org.Jwt
	org.JwtMutex.RUnlock()

	if jwt == "" {
		return nil, fmt.Errorf("no JWT token available, org: %s", org.ID)
	}

	// Replace {org_id} placeholder in URL
	url := endpoint
	if org.ID != "" {
		// Simple replacement
		url = fmt.Sprintf(endpoint, org.ID)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authorization header
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// Token might be expired, try to refresh
		log.Println("Received 401, attempting to refresh token")
		if err := c.getJWT(org); err != nil {
			return nil, fmt.Errorf("failed to refresh JWT, org %s: %w", org.ID, err)
		}

		// Retry the request with new token
		org.JwtMutex.RLock()
		jwt = org.Jwt
		org.JwtMutex.RUnlock()

		req.Header.Set("Authorization", "Bearer "+jwt)
		resp, err = c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to retry API request: %w", err)
		}
		defer resp.Body.Close()

	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	return result, nil
}

// extractValueFromJSON extracts a value from JSON using a simple dot-notation path
func extractValueFromJSON(data map[string]interface{}, path string) (interface{}, error) {
	if strings.Contains(path, "usage") {
		today := time.Now().Format(time.DateOnly)
		path = fmt.Sprintf(path, today) // simple substitution to add the date in for the usage endpoint.  TODO this is weak sauce.  fix.
	}

	keys := strings.Split(path, ".")
	var current interface{} = data

	for i, key := range keys {
		//log.Printf("Looking at key %s", key)
		switch v := current.(type) {
		case map[string]interface{}:
			//log.Printf("looking for key %s in %s",key, v[key])
			if val, ok := v[key]; ok {
				//log.Printf("found key %s in %s", key, v[key])
				current = val
			} else {
				return nil, fmt.Errorf("key not found: %s.  Path segment %d:%s", key, i, keys[:i+1])
			}
		case []interface{}:
			if indx, err := strconv.Atoi(key); err == nil && indx >= 0 && indx < len(v) {
				current = v[indx]
			} else {
				return nil, fmt.Errorf("bad array index %s  Path segment %d", key, i)
			}
		default:
			return nil, fmt.Errorf("can't navigate json path, expected object or array but got %T at path %d", current, i)
		}
	}

	return current, nil
}

// collectMetrics collects metrics from all configured endpoints for all organizations
func (c *LCCollector) collectMetrics() {
	for i := range c.config.Organizations{
		org := &c.config.Organizations[i] // use pointer so we don't copy mutexes

		for _, endpoint := range c.config.Endpoints {
			log.Printf("Collecting metrics from %s for org %s", endpoint.Name, org.ID)

			data, err := c.makeAPICall(endpoint.URL, org)

			if err != nil {
				log.Printf("Failed to call endpoint %s for org %s: %v, skipping", endpoint.Name, org.ID, err)
				continue
			}

			// Process metrics for this endpoint
			for _, metricConfig := range endpoint.Metrics {
				value, err := extractValueFromJSON(data, metricConfig.JSONPath)
				if err != nil {
					log.Printf("Failed to extract value for metric %s: %v", metricConfig.Name, err)
					continue
				}

				// Convert value to float64
				var floatValue float64
				switch v := value.(type) {
				case float64:
					floatValue = v
				case int:
					floatValue = float64(v)
				case string:
					// TODO: add some logic to convert numeric strings to numbers
					log.Printf("Cannot convert string value to number for metric %s", metricConfig.Name)
					continue
				default:
					log.Printf("Unsupported value type for metric %s", metricConfig.Name)
					continue
				}

				// Prepare labels
				labels := prometheus.Labels{
					"org_id":   org.ID,
					"org_name": org.Name,
				}

				// Add static labels
				for key, value := range metricConfig.Labels {
					labels[key] = value
				}

				// Add dynamic labels from JSON
				for labelKey, jsonPath := range metricConfig.LabelFields {
					if labelValue, err := extractValueFromJSON(data, jsonPath); err == nil {
						if strValue, ok := labelValue.(string); ok {
							labels[labelKey] = strValue
						}
					}
				}

				// Create label hash for counter tracking
				labelHash := ""
				labelKeys := make([]string, 0, len(labels))
				for k := range labels {
					labelKeys = append(labelKeys, k)
				}
				// Sort keys for consistent hashing
				for i := 0; i < len(labelKeys)-1; i++ {
					for j := i + 1; j < len(labelKeys); j++ {
						if labelKeys[i] > labelKeys[j] {
							labelKeys[i], labelKeys[j] = labelKeys[j], labelKeys[i]
						}
					}
				}
				for _, k := range labelKeys {
					labelHash += k + "=" + labels[k] + ","
				}

				// Set metric value
				if gauge, ok := c.gauges[metricConfig.Name]; ok {
					gauge.With(labels).Set(floatValue)
				} else if counter, ok := c.counters[metricConfig.Name]; ok {
					c.counterMutex.Lock()

					// Initialize counter values map for this metric if needed
					if c.counterValues[metricConfig.Name] == nil {
						c.counterValues[metricConfig.Name] = make(map[string]float64)
					}

					// Get the last known value for this label combination
					lastValue, exists := c.counterValues[metricConfig.Name][labelHash]

					if !exists { //first time seeing this counter
						c.counterValues[metricConfig.Name][labelHash] = floatValue
						log.Printf("init counter %s with value %f", metricConfig.Name, floatValue)
					} else if floatValue >= lastValue { // Value is increasing - normal case
						delta := floatValue - lastValue
						counter.With(labels).Add(delta)
						c.counterValues[metricConfig.Name][labelHash] = floatValue
						log.Printf("counter increased by %f", delta)
					} else { //counter must have done the end of day reset.  FIXME: need to figure a way to get the previous day's max val here
						counter.With(labels).Add(floatValue)
						c.counterValues[metricConfig.Name][labelHash] = floatValue
						log.Printf("counter reset, increasing by new base value %f", floatValue)
					}

					c.counterMutex.Unlock()
				} else {
					log.Printf("Metric %s not found in gauge or counter list?", metricConfig.Name)
				}
			}
		}
	}
}

// Start starts the collector
func (c *LCCollector) Start() error {
	// Get initial JWT tokens for all orgs
	for i := range c.config.Organizations{
		org := &c.config.Organizations[i]
		if err := c.getJWT(org); err != nil {
			log.Printf("Couldn't get initial JWT for %s: %v", org.ID, err)
		}
	}

	// Start metrics collection goroutine
	go func() {
		ticker := time.NewTicker(time.Duration(c.config.Server.Interval) * time.Second)
		defer ticker.Stop()

		// Collect metrics immediately
		c.collectMetrics()

		for range ticker.C {
			c.collectMetrics()
		}
	}()
	return nil
}
