package analyzer

import (
	"context"
	"fmt"
	"time"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk"
)

// AnomalyDetector evaluates spike conditions and dispatches alerts via SDK.
type AnomalyDetector struct {
	cfg    Config
	host   pluginsdk.HostContext
	logger pluginsdk.Logger
}

// NewAnomalyDetector creates an anomaly detector instance.
func NewAnomalyDetector(cfg Config, host pluginsdk.HostContext, logger pluginsdk.Logger) *AnomalyDetector {
	return &AnomalyDetector{
		cfg:    cfg,
		host:   host,
		logger: logger,
	}
}

// IsSpike evaluates whether a candidate rate constitutes a spike over the baseline EWMA.
func (ad *AnomalyDetector) IsSpike(rate float64, ewma float64) bool {
	return rate > (ad.cfg.TrafficSpikeMultiplier*ewma) && (rate > ad.cfg.MinTrafficSpikeMBps)
}

// Evaluate evaluates a server state update for traffic or resource spikes.
func (ad *AnomalyDetector) Evaluate(ctx context.Context, state *ServerState) bool {
	state.mu.Lock()
	defer state.mu.Unlock()

	currentRate := state.CurrentDownloadRateMB
	ewmaRate := state.EWMADownloadMB
	serverID := state.ServerID

	// Traffic Spike Rule (TestTier2_F8_04 & TestTier3_F6):
	// rate > 3.0 * EWMA && rate > 50.0 MB/s
	isTrafficSpike := ad.IsSpike(currentRate, ewmaRate)

	if isTrafficSpike {
		// Cooldown Debounce Check
		if !state.LastAlertAt.IsZero() && time.Since(state.LastAlertAt) < ad.cfg.AlertCooldown {
			return false
		}
		state.LastAlertAt = time.Now()

		if ad.host == nil {
			return true
		}

		notif := pluginsdk.Notification{
			Title:    "Traffic Spike Alert",
			Message:  fmt.Sprintf("Server %s rate exceeded threshold: %.2f MB/s", serverID, currentRate),
			ServerID: serverID,
			Details: map[string]any{
				"event":        "alert.traffic.spike",
				"server_id":    serverID,
				"current_rate": currentRate,
				"ewma_rate":    ewmaRate,
				"multiplier":   ad.cfg.TrafficSpikeMultiplier,
				"timestamp":    time.Now().Unix(),
			},
		}

		go func() {
			if err := ad.host.SendNotification(ctx, notif); err != nil {
				if ad.logger != nil {
					ad.logger.Errorf("failed to send traffic spike notification for %s: %v", serverID, err)
				}
			} else if ad.logger != nil {
				ad.logger.Infof("dispatched traffic spike alert for %s (rate: %.2f MB/s)", serverID, currentRate)
			}
		}()
		return true
	}

	return false
}
