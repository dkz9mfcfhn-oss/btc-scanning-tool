package miner

import (
	"math"
	"time"
)

// Bound the value so the UI can convert the reported seconds to a duration.
func elapsedSeconds(v *float64) *float64 {
	if v == nil || *v < 0 || *v >= float64(math.MaxInt64/int64(time.Second)) {
		return nil
	}
	return v
}
