// +build js

package goa

import (
	"time"
)

// IncrCounter increments the counter named by `key`
// Usage:
//     IncrCounter([]key{"my","namespace","counter"}, 1.0)
func IncrCounter(key []string, val float32) {
	//Do nothing
}

// MeasureSince creates a timing metric that records
// the duration of elapsed time since `start`
// Usage:
//     MeasureSince([]string{"my","namespace","action}, time.Now())
// Frequently used in a defer:
//    defer MeasureSince([]string{"my","namespace","action}, time.Now())
func MeasureSince(key []string, start time.Time) {

}
