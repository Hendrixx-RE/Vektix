package metrics

import (
	"sync"
	"time"
)

// MetricType classifies metric types.
type MetricType string

const (
	TypeCounter   MetricType = "counter"
	TypeGauge     MetricType = "gauge"
	TypeHistogram MetricType = "histogram"
)

// Metric represents a recorded data point.
type Metric struct {
	Name      string
	Type      MetricType
	Value     float64
	Timestamp time.Time
}

// Collector manages metric registries and snapshot exports.
type Collector struct {
	counters   map[string]int64
	gauges     map[string]float64
	histograms map[string][]float64
	mu         sync.RWMutex
}

// NewCollector initializes an empty metric collector.
func NewCollector() *Collector {
	return &Collector{
		counters:   make(map[string]int64),
		gauges:     make(map[string]float64),
		histograms: make(map[string][]float64),
	}
}

// IncCounter increments a named counter metric by 1.
func (c *Collector) IncCounter(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counters[name]++
}

// SetGauge updates a named gauge value.
func (c *Collector) SetGauge(name string, val float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gauges[name] = val
}

// ObserveHistogram records a sample duration or quantity in a histogram bucket.
func (c *Collector) ObserveHistogram(name string, val float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.histograms[name] = append(c.histograms[name], val)
}
