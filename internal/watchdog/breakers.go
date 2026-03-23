package watchdog

import (
	"fmt"
	"time"

	"github.com/abdou/forge/internal/alarm"
	"github.com/abdou/forge/internal/container"
	"github.com/rs/zerolog/log"
)

// BreakerType identifies which circuit breaker triggered
type BreakerType string

const (
	BreakerCPUSustained BreakerType = "cpu-sustained"
	BreakerRAMPressure  BreakerType = "ram-pressure"
	BreakerPIDFlood     BreakerType = "pid-flood"
	BreakerDiskStorm    BreakerType = "disk-storm"
	BreakerNetworkScan  BreakerType = "network-scan"
)

// checkBreakers evaluates all circuit breakers and kills container if triggered
func (w *Watchdog) checkBreakers(stats *container.ContainerStats) {
	// Check CPU sustained
	if w.checkCPUSustained(stats) {
		w.trigger(BreakerCPUSustained, "CPU > %d%% for %d seconds",
			w.cfg.CPUSustainedThresholdPct, w.cfg.CPUSustainedDurationSeconds)
		return
	}

	// Check RAM pressure
	if w.checkRAMPressure(stats) {
		w.trigger(BreakerRAMPressure, "RAM > %.1f%% of limit",
			float64(w.cfg.RAMThresholdPct))
		return
	}

	// Check PID flood
	if w.checkPIDFlood(stats) {
		w.trigger(BreakerPIDFlood, "PIDs > %d", w.cfg.PIDLimit)
		return
	}

	// Check disk storm
	if w.checkDiskStorm(stats) {
		w.trigger(BreakerDiskStorm, "Disk writes > %d MB in session",
			w.cfg.DiskWriteLimitMB)
		return
	}

	// Check network scan
	if w.checkNetworkScan(stats) {
		w.trigger(BreakerNetworkScan, "Network scan detected")
		return
	}
}

// checkCPUSustained checks if CPU has been high for sustained period
func (w *Watchdog) checkCPUSustained(stats *container.ContainerStats) bool {
	// Need enough history
	requiredSamples := w.cfg.CPUSustainedDurationSeconds / w.cfg.PollIntervalSeconds

	w.statsMutex.RLock()
	defer w.statsMutex.RUnlock()

	if len(w.stats) < requiredSamples {
		return false
	}

	// Check last N samples
	recent := w.stats[len(w.stats)-requiredSamples:]
	for _, s := range recent {
		if s.CPUPercent < float64(w.cfg.CPUSustainedThresholdPct) {
			return false
		}
	}

	return true
}

// checkRAMPressure checks if RAM usage is too high
func (w *Watchdog) checkRAMPressure(stats *container.ContainerStats) bool {
	return stats.RAMPercent >= float64(w.cfg.RAMThresholdPct)
}

// checkPIDFlood checks if too many processes
func (w *Watchdog) checkPIDFlood(stats *container.ContainerStats) bool {
	return stats.PIDs >= uint64(w.cfg.PIDLimit)
}

// checkDiskStorm checks if disk writes exceed limit
func (w *Watchdog) checkDiskStorm(stats *container.ContainerStats) bool {
	// Sum all disk writes from stats history
	w.statsMutex.RLock()
	defer w.statsMutex.RUnlock()

	var totalMB float64
	for _, s := range w.stats {
		totalMB += s.DiskWriteMB
	}

	return totalMB > float64(w.cfg.DiskWriteLimitMB)
}

// checkNetworkScan checks for network scanning patterns (placeholder)
func (w *Watchdog) checkNetworkScan(stats *container.ContainerStats) bool {
	// This would require parsing /proc/net/tcp inside container
	// For now, we skip this check
	return false
}

// trigger handles a circuit breaker firing
func (w *Watchdog) trigger(breaker BreakerType, format string, args ...interface{}) {
	reason := fmt.Sprintf(format, args...)

	log.Warn().
		Str("session", w.sessionID).
		Str("breaker", string(breaker)).
		Str("reason", reason).
		Msg("Circuit breaker triggered")

	// Dispatch alarm
	w.alarm.Dispatch(alarm.Event{
		Type:      alarm.EventTypeBreaker,
		SessionID: w.sessionID,
		Breaker:   string(breaker),
		Reason:    reason,
		Timestamp: time.Now(),
	})

	// Kill container
	if err := w.manager.Kill(w.containerID); err != nil {
		log.Error().Err(err).Msg("Failed to kill container after breaker trigger")
	}
}
