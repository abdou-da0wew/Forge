package watchdog

import (
	"context"
	"sync"
	"time"

	"github.com/abdou/forge/internal/alarm"
	"github.com/abdou/forge/internal/config"
	"github.com/abdou/forge/internal/container"
	"github.com/rs/zerolog/log"
)

// Watchdog monitors a container for resource abuse
type Watchdog struct {
	sessionID   string
	containerID string
	manager     *container.Manager
	alarm       *alarm.Dispatcher
	cfg         *config.WatchdogConfig

	stats      []Stats
	statsMutex sync.RWMutex

	done     chan struct{}
	stopOnce sync.Once
}

// Stats holds a single stats sample
type Stats struct {
	CPUPercent float64
	RAMMB      int64
	PIDs       uint64
	DiskWriteMB float64
	Timestamp  time.Time
}

// New creates a new watchdog for a session
func New(sessionID, containerID string, manager *container.Manager, alarmDisp *alarm.Dispatcher, cfg *config.WatchdogConfig) *Watchdog {
	return &Watchdog{
		sessionID:   sessionID,
		containerID: containerID,
		manager:     manager,
		alarm:       alarmDisp,
		cfg:         cfg,
		stats:       make([]Stats, 0, 100),
		done:        make(chan struct{}),
	}
}

// Start begins the watchdog monitoring loop
func (w *Watchdog) Start() {
	go w.loop()
}

// Stop halts the watchdog
func (w *Watchdog) Stop() {
	w.stopOnce.Do(func() {
		close(w.done)
	})
}

func (w *Watchdog) loop() {
	ticker := time.NewTicker(time.Duration(w.cfg.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			if err := w.collectAndCheck(); err != nil {
				log.Warn().Err(err).Str("session", w.sessionID).Msg("Watchdog error")
			}
		}
	}
}

func (w *Watchdog) collectAndCheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	statsChan, err := w.manager.Stats(ctx, w.containerID)
	if err != nil {
		return err
	}

	select {
	case stats := <-statsChan:
		w.recordStats(stats)
		w.checkBreakers(stats)
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (w *Watchdog) recordStats(stats *container.ContainerStats) {
	sample := Stats{
		CPUPercent:  stats.CPUPercent,
		RAMMB:       int64(stats.RAMBytes / 1024 / 1024),
		PIDs:        stats.PIDs,
		DiskWriteMB: float64(stats.DiskWrite) / 1024 / 1024,
		Timestamp:   stats.Timestamp,
	}

	w.statsMutex.Lock()
	w.stats = append(w.stats, sample)
	// Keep only last 100 samples
	if len(w.stats) > 100 {
		w.stats = w.stats[len(w.stats)-100:]
	}
	w.statsMutex.Unlock()

	log.Debug().
		Str("session", w.sessionID).
		Float64("cpu_pct", sample.CPUPercent).
		Int64("ram_mb", sample.RAMMB).
		Uint64("pids", sample.PIDs).
		Msg("Watchdog stats")
}

// GetStats returns a copy of the stats history
func (w *Watchdog) GetStats() []Stats {
	w.statsMutex.RLock()
	defer w.statsMutex.RUnlock()

	result := make([]Stats, len(w.stats))
	copy(result, w.stats)
	return result
}
