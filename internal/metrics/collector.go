package metrics

import (
	"fmt"
	"sync"
	"time"
)

// Metric represents a single metric value.
type Metric struct {
	Name      string
	Value     float64
	Timestamp time.Time
	Tags      map[string]string
}

// Collector collects system metrics.
type Collector struct {
	mu      sync.RWMutex
	metrics map[string]*Metric

	counters map[string]float64
	gauges   map[string]float64
	timings  map[string][]time.Duration
}

// New creates a new metrics collector.
func New() *Collector {
	return &Collector{
		metrics:  make(map[string]*Metric),
		counters: make(map[string]float64),
		gauges:   make(map[string]float64),
		timings:  make(map[string][]time.Duration),
	}
}

// IncrementCounter increments a counter metric.
func (c *Collector) IncrementCounter(name string, value float64, tags map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.metricKey(name, tags)
	c.counters[key] += value
}

// SetGauge sets a gauge metric value.
func (c *Collector) SetGauge(name string, value float64, tags map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.metricKey(name, tags)
	c.gauges[key] = value
}

// RecordTiming records a timing measurement.
func (c *Collector) RecordTiming(name string, duration time.Duration, tags map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.metricKey(name, tags)
	c.timings[key] = append(c.timings[key], duration)
}

// RecordTaskDuration records task execution duration.
func (c *Collector) RecordTaskDuration(taskID string, duration time.Duration) {
	c.IncrementCounter("task.duration.total", duration.Seconds(), map[string]string{"task": taskID})
	c.RecordTiming("task.duration", duration, map[string]string{"task": taskID})
}

// RecordTaskSuccess records a successful task.
func (c *Collector) RecordTaskSuccess(taskID string) {
	c.IncrementCounter("task.success", 1, map[string]string{"task": taskID})
}

// RecordTaskFailure records a failed task.
func (c *Collector) RecordTaskFailure(taskID string, reason string) {
	c.IncrementCounter("task.failure", 1, map[string]string{"task": taskID, "reason": reason})
}

// RecordQuotaUsage records Claude API quota usage.
func (c *Collector) RecordQuotaUsage(quotaUsed float64) {
	c.IncrementCounter("claude.quota.used", quotaUsed, nil)
}

// GetCounter returns a counter value.
func (c *Collector) GetCounter(name string, tags map[string]string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := c.metricKey(name, tags)
	return c.counters[key]
}

// GetGauge returns a gauge value.
func (c *Collector) GetGauge(name string, tags map[string]string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := c.metricKey(name, tags)
	return c.gauges[key]
}

// GetStats returns statistics for a timing metric.
func (c *Collector) GetStats(name string, tags map[string]string) *Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := c.metricKey(name, tags)
	timings := c.timings[key]

	if len(timings) == 0 {
		return &Stats{}
	}

	stats := &Stats{
		Count: int64(len(timings)),
	}

	total := time.Duration(0)
	min := timings[0]
	max := timings[0]

	for _, d := range timings {
		total += d
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
	}

	stats.Sum = total
	stats.Min = min
	stats.Max = max
	stats.Avg = time.Duration(int64(total) / int64(len(timings)))

	return stats
}

// Stats represents statistical information about a metric.
type Stats struct {
	Count int64
	Sum   time.Duration
	Min   time.Duration
	Max   time.Duration
	Avg   time.Duration
}

// ExportPrometheus exports metrics in Prometheus format.
func (c *Collector) ExportPrometheus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	output := ""

	// Export counters
	for key, value := range c.counters {
		output += fmt.Sprintf("# HELP %s Counter metric\n", key)
		output += fmt.Sprintf("# TYPE %s counter\n", key)
		output += fmt.Sprintf("%s %f\n", key, value)
	}

	// Export gauges
	for key, value := range c.gauges {
		output += fmt.Sprintf("# HELP %s Gauge metric\n", key)
		output += fmt.Sprintf("# TYPE %s gauge\n", key)
		output += fmt.Sprintf("%s %f\n", key, value)
	}

	// Export timing stats
	for key, timings := range c.timings {
		if len(timings) == 0 {
			continue
		}

		total := time.Duration(0)
		for _, d := range timings {
			total += d
		}

		output += fmt.Sprintf("# HELP %s_total Total duration in seconds\n", key)
		output += fmt.Sprintf("# TYPE %s_total counter\n", key)
		output += fmt.Sprintf("%s_total %f\n", key, total.Seconds())

		output += fmt.Sprintf("# HELP %s_count Count of measurements\n", key)
		output += fmt.Sprintf("# TYPE %s_count counter\n", key)
		output += fmt.Sprintf("%s_count %d\n", key, len(timings))
	}

	return output
}

// Reset clears all metrics.
func (c *Collector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metrics = make(map[string]*Metric)
	c.counters = make(map[string]float64)
	c.gauges = make(map[string]float64)
	c.timings = make(map[string][]time.Duration)
}

// metricKey creates a metric key from name and tags.
func (c *Collector) metricKey(name string, tags map[string]string) string {
	key := name
	for k, v := range tags {
		key += fmt.Sprintf("__%s_%s", k, v)
	}
	return key
}
