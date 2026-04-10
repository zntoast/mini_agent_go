package llm

import (
	"math"
	"time"
)

type RetryConfig struct {
	Enabled         bool
	MaxRetries      int
	InitialDelay    float64
	MaxDelay        float64
	ExponentialBase float64
}

type RetryExhaustedError struct {
	LastException error
	Attempts      int
}

func (e *RetryExhaustedError) Error() string {
	return "Retry failed after " + itoa(e.Attempts) + " attempts. Last error: " + e.LastException.Error()
}

func (c *RetryConfig) CalculateDelay(attempt int) float64 {
	delay := c.InitialDelay * math.Pow(c.ExponentialBase, float64(attempt))
	return math.Min(delay, c.MaxDelay)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	result := ""
	for i > 0 {
		result = string(rune('0'+i%10)) + result
		i /= 10
	}
	return result
}

func AsyncSleep(duration time.Duration) {
	time.Sleep(duration)
}
