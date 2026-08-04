package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestCollectorNew(t *testing.T) {
	c := New()
	if c == nil {
		t.Fatal("New returned nil")
	}
}

func TestCollectorIncrementCounter(t *testing.T) {
	c := New()

	c.IncrementCounter("test.counter", 1, nil)
	value := c.GetCounter("test.counter", nil)
	if value != 1 {
		t.Errorf("got %f, want 1", value)
	}

	c.IncrementCounter("test.counter", 2, nil)
	value = c.GetCounter("test.counter", nil)
	if value != 3 {
		t.Errorf("got %f, want 3", value)
	}
}

func TestCollectorSetGauge(t *testing.T) {
	c := New()

	c.SetGauge("test.gauge", 42.5, nil)
	value := c.GetGauge("test.gauge", nil)
	if value != 42.5 {
		t.Errorf("got %f, want 42.5", value)
	}

	c.SetGauge("test.gauge", 100, nil)
	value = c.GetGauge("test.gauge", nil)
	if value != 100 {
		t.Errorf("got %f, want 100", value)
	}
}

func TestCollectorRecordTiming(t *testing.T) {
	c := New()

	duration := 100 * time.Millisecond
	c.RecordTiming("test.timing", duration, nil)

	stats := c.GetStats("test.timing", nil)
	if stats.Count != 1 {
		t.Errorf("Count: got %d, want 1", stats.Count)
	}
	if stats.Sum != duration {
		t.Errorf("Sum: got %v, want %v", stats.Sum, duration)
	}
	if stats.Min != duration {
		t.Errorf("Min: got %v, want %v", stats.Min, duration)
	}
	if stats.Max != duration {
		t.Errorf("Max: got %v, want %v", stats.Max, duration)
	}
}

func TestCollectorRecordTaskDuration(t *testing.T) {
	c := New()

	duration := 500 * time.Millisecond
	c.RecordTaskDuration("task-123", duration)

	stats := c.GetStats("task.duration", map[string]string{"task": "task-123"})
	if stats.Count != 1 {
		t.Errorf("Count: got %d, want 1", stats.Count)
	}

	totalCounter := c.GetCounter("task.duration.total", map[string]string{"task": "task-123"})
	if totalCounter != duration.Seconds() {
		t.Errorf("Counter: got %f, want %f", totalCounter, duration.Seconds())
	}
}

func TestCollectorRecordTaskSuccess(t *testing.T) {
	c := New()

	c.RecordTaskSuccess("task-123")
	c.RecordTaskSuccess("task-123")

	counter := c.GetCounter("task.success", map[string]string{"task": "task-123"})
	if counter != 2 {
		t.Errorf("got %f, want 2", counter)
	}
}

func TestCollectorRecordTaskFailure(t *testing.T) {
	c := New()

	c.RecordTaskFailure("task-123", "timeout")
	c.RecordTaskFailure("task-456", "error")

	timeoutCounter := c.GetCounter("task.failure", map[string]string{"task": "task-123", "reason": "timeout"})
	if timeoutCounter != 1 {
		t.Errorf("timeout counter: got %f, want 1", timeoutCounter)
	}

	errorCounter := c.GetCounter("task.failure", map[string]string{"task": "task-456", "reason": "error"})
	if errorCounter != 1 {
		t.Errorf("error counter: got %f, want 1", errorCounter)
	}
}

func TestCollectorRecordQuotaUsage(t *testing.T) {
	c := New()

	c.RecordQuotaUsage(0.25)
	c.RecordQuotaUsage(0.15)

	usage := c.GetCounter("claude.quota.used", nil)
	if usage != 0.40 {
		t.Errorf("got %f, want 0.4", usage)
	}
}

func TestCollectorTimingStats(t *testing.T) {
	c := New()

	durations := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		150 * time.Millisecond,
	}

	for _, d := range durations {
		c.RecordTiming("test.timing", d, nil)
	}

	stats := c.GetStats("test.timing", nil)
	if stats.Count != 3 {
		t.Errorf("Count: got %d, want 3", stats.Count)
	}

	expectedSum := 450 * time.Millisecond
	if stats.Sum != expectedSum {
		t.Errorf("Sum: got %v, want %v", stats.Sum, expectedSum)
	}

	if stats.Min != 100*time.Millisecond {
		t.Errorf("Min: got %v, want 100ms", stats.Min)
	}

	if stats.Max != 200*time.Millisecond {
		t.Errorf("Max: got %v, want 200ms", stats.Max)
	}

	expectedAvg := 150 * time.Millisecond
	if stats.Avg != expectedAvg {
		t.Errorf("Avg: got %v, want %v", stats.Avg, expectedAvg)
	}
}

func TestCollectorWithTags(t *testing.T) {
	c := New()

	tags1 := map[string]string{"task": "task-1", "status": "success"}
	tags2 := map[string]string{"task": "task-2", "status": "failure"}

	c.IncrementCounter("task.result", 1, tags1)
	c.IncrementCounter("task.result", 1, tags2)

	value1 := c.GetCounter("task.result", tags1)
	value2 := c.GetCounter("task.result", tags2)

	if value1 != 1 {
		t.Errorf("counter 1: got %f, want 1", value1)
	}
	if value2 != 1 {
		t.Errorf("counter 2: got %f, want 1", value2)
	}
}

func TestCollectorExportPrometheus(t *testing.T) {
	c := New()

	c.IncrementCounter("test.counter", 5, nil)
	c.SetGauge("test.gauge", 42, nil)
	c.RecordTiming("test.timing", 100*time.Millisecond, nil)

	output := c.ExportPrometheus()

	if !strings.Contains(output, "test.counter") {
		t.Error("Prometheus output missing counter")
	}
	if !strings.Contains(output, "test.gauge") {
		t.Error("Prometheus output missing gauge")
	}
	if !strings.Contains(output, "test.timing") {
		t.Error("Prometheus output missing timing")
	}
	if !strings.Contains(output, "HELP") {
		t.Error("Prometheus output missing HELP")
	}
	if !strings.Contains(output, "TYPE") {
		t.Error("Prometheus output missing TYPE")
	}
}

func TestCollectorReset(t *testing.T) {
	c := New()

	c.IncrementCounter("test.counter", 5, nil)
	c.SetGauge("test.gauge", 42, nil)

	c.Reset()

	counter := c.GetCounter("test.counter", nil)
	gauge := c.GetGauge("test.gauge", nil)

	if counter != 0 {
		t.Errorf("counter after reset: got %f, want 0", counter)
	}
	if gauge != 0 {
		t.Errorf("gauge after reset: got %f, want 0", gauge)
	}
}

func TestCollectorEmptyStats(t *testing.T) {
	c := New()

	stats := c.GetStats("nonexistent", nil)
	if stats.Count != 0 {
		t.Errorf("Empty stats count: got %d, want 0", stats.Count)
	}
}

func TestCollectorConcurrency(t *testing.T) {
	c := New()

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				c.IncrementCounter("concurrent.counter", 1, map[string]string{"id": "test"})
				c.RecordTiming("concurrent.timing", 10*time.Millisecond, nil)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	counter := c.GetCounter("concurrent.counter", map[string]string{"id": "test"})
	if counter != 1000 {
		t.Errorf("Concurrent counter: got %f, want 1000", counter)
	}

	stats := c.GetStats("concurrent.timing", nil)
	if stats.Count != 1000 {
		t.Errorf("Concurrent timing count: got %d, want 1000", stats.Count)
	}
}
