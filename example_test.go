package retry_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jacob2161/retry"
)

// statusError is an HTTP response status returned as an error.
type statusError struct{ statusCode int }

func (e *statusError) Error() string { return http.StatusText(e.statusCode) }

func ExampleBackoff() {
	statusCodes := []int{http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusOK}
	attempts := 0
	err := retry.Backoff(context.Background(), &retry.BackoffArgs{
		Operation: func() error {
			statusCode := statusCodes[attempts]
			attempts++
			if statusCode != http.StatusOK {
				return &statusError{statusCode: statusCode}
			}
			return nil
		},
		Transient: func(err error) bool {
			var status *statusError
			return errors.As(err, &status) && retry.TransientHTTP(status.statusCode)
		},
		InitialDelay: time.Millisecond,
		OnRetry: func(attempt int, err error, delay time.Duration) {
			fmt.Printf("attempt %d failed: %v\n", attempt, err)
		},
	})
	fmt.Println("attempts:", attempts, "error:", err)
	// Output:
	// attempt 1 failed: Service Unavailable
	// attempt 2 failed: Bad Gateway
	// attempts: 3 error: <nil>
}

func ExampleJitter() {
	delay := retry.Jitter(time.Second)
	fmt.Println(delay >= time.Second && delay < 1500*time.Millisecond)
	// Output: true
}
