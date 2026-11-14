package analyzer

import (
	"math"
	"sync"
	"time"
)

// ServerState tracks per-server moving averages and analytical telemetry state.
type ServerState struct {
	mu            sync.RWMutex
	ServerID      string
	LastUpdated   time.Time
	LastRecvBytes uint64
	LastSentBytes uint64

	EWMADownloadMB float64 // Smoothed incoming rate (MB/s)
	EWMAUploadMB   float64 // Smoothed outgoing rate (MB/s)
	EWMACPU        float64 // Smoothed CPU percent (0.0 - 100.0)
	EWMAMemory     float64 // Smoothed Mem percent (0.0 - 100.0)

	CurrentDownloadRateMB float64
	CurrentUploadRateMB   float64

	Initialized bool
	LastAlertAt time.Time
}

// EWMACalculator computes thread-safe Exponential Weighted Moving Averages across servers.
type EWMACalculator struct {
	alpha   float64
	servers sync.Map // map[string]*ServerState
}

// NewEWMACalculator creates an EWMA calculator with the specified alpha factor.
func NewEWMACalculator(alpha float64) *EWMACalculator {
	if alpha <= 0 || alpha > 1.0 {
		alpha = 0.2 // Default fallback
	}
	return &EWMACalculator{alpha: alpha}
}

// GetOrCreateState returns the thread-safe state for the given server ID.
func (c *EWMACalculator) GetOrCreateState(serverID string) *ServerState {
	val, loaded := c.servers.Load(serverID)
	if loaded {
		return val.(*ServerState)
	}
	newState := &ServerState{
		ServerID: serverID,
	}
	actual, _ := c.servers.LoadOrStore(serverID, newState)
	return actual.(*ServerState)
}

// SetBaseline sets an initial baseline rate for testing or calibration.
func (c *EWMACalculator) SetBaseline(serverID string, baseline float64) {
	state := c.GetOrCreateState(serverID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.EWMADownloadMB = baseline
	state.CurrentDownloadRateMB = baseline
	state.Initialized = true
}

// UpdateRate computes the new EWMA for a single rate scalar and returns the smoothed value.
func (c *EWMACalculator) UpdateRate(serverID string, rate float64) float64 {
	state := c.GetOrCreateState(serverID)
	state.mu.Lock()
	defer state.mu.Unlock()

	rate = sanitizeRate(rate)
	state.CurrentDownloadRateMB = rate

	if !state.Initialized {
		state.EWMADownloadMB = rate
		state.Initialized = true
	} else {
		state.EWMADownloadMB = c.alpha*rate + (1.0-c.alpha)*state.EWMADownloadMB
	}

	return state.EWMADownloadMB
}

// Update computes the new EWMA based on an incoming observation.
func (c *EWMACalculator) Update(metric *NormalizedMetric) *ServerState {
	state := c.GetOrCreateState(metric.ServerID)
	state.mu.Lock()
	defer state.mu.Unlock()

	now := metric.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	// Calculate instantaneous rates
	inRate := metric.DownloadRateMB
	outRate := metric.UploadRateMB

	// Fallback to cumulative delta computation if instantaneous rates are 0 but totals are present
	if inRate == 0 && metric.TotalRecvBytes > 0 && state.LastRecvBytes > 0 && !state.LastUpdated.IsZero() {
		dt := now.Sub(state.LastUpdated).Seconds()
		if dt > 0 && metric.TotalRecvBytes >= state.LastRecvBytes {
			delta := float64(metric.TotalRecvBytes - state.LastRecvBytes)
			inRate = (delta / (1024.0 * 1024.0)) / dt
		}
	}
	if outRate == 0 && metric.TotalSentBytes > 0 && state.LastSentBytes > 0 && !state.LastUpdated.IsZero() {
		dt := now.Sub(state.LastUpdated).Seconds()
		if dt > 0 && metric.TotalSentBytes >= state.LastSentBytes {
			delta := float64(metric.TotalSentBytes - state.LastSentBytes)
			outRate = (delta / (1024.0 * 1024.0)) / dt
		}
	}

	// Float overflow & extreme values guard (TestTier2_F8_03)
	inRate = sanitizeRate(inRate)
	outRate = sanitizeRate(outRate)

	state.CurrentDownloadRateMB = inRate
	state.CurrentUploadRateMB = outRate

	// EWMA computation: EWMA_t = alpha * Rate_t + (1 - alpha) * EWMA_{t-1}
	if !state.Initialized {
		state.EWMADownloadMB = inRate
		state.EWMAUploadMB = outRate
		state.EWMACPU = metric.CPUPercent
		state.EWMAMemory = metric.MemPercent
		state.Initialized = true
	} else {
		state.EWMADownloadMB = c.alpha*inRate + (1.0-c.alpha)*state.EWMADownloadMB
		state.EWMAUploadMB = c.alpha*outRate + (1.0-c.alpha)*state.EWMAUploadMB
		if metric.CPUPercent > 0 {
			state.EWMACPU = c.alpha*metric.CPUPercent + (1.0-c.alpha)*state.EWMACPU
		}
		if metric.MemPercent > 0 {
			state.EWMAMemory = c.alpha*metric.MemPercent + (1.0-c.alpha)*state.EWMAMemory
		}
	}

	state.LastRecvBytes = metric.TotalRecvBytes
	state.LastSentBytes = metric.TotalSentBytes
	state.LastUpdated = now

	return state
}

// DecayMissing decays idle server rates towards zero (TestTier2_F8_05).
func (c *EWMACalculator) DecayMissing(serverID string) float64 {
	val, ok := c.servers.Load(serverID)
	if !ok {
		return 0
	}
	state := val.(*ServerState)
	state.mu.Lock()
	defer state.mu.Unlock()

	// Rate decays by (1 - alpha)
	state.EWMADownloadMB = (1.0 - c.alpha) * state.EWMADownloadMB
	state.EWMAUploadMB = (1.0 - c.alpha) * state.EWMAUploadMB
	state.CurrentDownloadRateMB = 0
	state.CurrentUploadRateMB = 0

	// Prevent floating point underflow to negative zero or subnormals
	if state.EWMADownloadMB < 1e-9 {
		state.EWMADownloadMB = 0
	}
	if state.EWMAUploadMB < 1e-9 {
		state.EWMAUploadMB = 0
	}

	return state.EWMADownloadMB
}

// GetAllStates returns a snapshot of all active server states.
func (c *EWMACalculator) GetAllStates() map[string]*ServerState {
	res := make(map[string]*ServerState)
	c.servers.Range(func(key, value any) bool {
		res[key.(string)] = value.(*ServerState)
		return true
	})
	return res
}

func sanitizeRate(rate float64) float64 {
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
		return 0
	}
	if rate > float64(math.MaxUint64)/1024.0 {
		return float64(math.MaxUint64) / 1024.0
	}
	return rate
}
