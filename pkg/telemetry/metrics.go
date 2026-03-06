package telemetry

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	worldTickDurationMs = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "lived_world_tick_duration_milliseconds",
		Help:    "Duration of world tick processing per realm in milliseconds.",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000, 10000},
	}, []string{"realm_id"})

	worldTickAdvanceMinutes = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "lived_world_tick_advance_minutes",
		Help:    "Simulated game minutes advanced per world tick and realm.",
		Buckets: []float64{0, 1, 5, 10, 30, 60, 120, 240, 480, 960},
	}, []string{"realm_id"})

	worldTickRuns = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lived_world_tick_runs_total",
		Help: "Total number of world tick runs per realm.",
	}, []string{"realm_id"})

	worldTickErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lived_world_tick_errors_total",
		Help: "Total number of failed world tick runs per realm.",
	}, []string{"realm_id"})

	streamActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "lived_stream_connections_active",
		Help: "Number of currently active stream connections.",
	})

	streamRejectedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lived_stream_connections_rejected_total",
		Help: "Total number of rejected stream connection attempts by reason.",
	}, []string{"reason"})

	worldTickAggregateMu sync.RWMutex
	worldTickAggregate   = worldTickAggregateState{}
)

type worldTickAggregateState struct {
	totalRuns           int64
	totalFailures       int64
	totalDurationMS     float64
	totalAdvanceMinutes float64
	lastDurationMS      float64
	lastAdvanceMinutes  int64
	lastRealmID         uint
	lastUpdatedAt       time.Time
}

type WorldTickAggregateSnapshot struct {
	TotalRuns           int64
	TotalFailures       int64
	FailureRate         float64
	AvgDurationMS       float64
	AvgAdvanceMinutes   float64
	LastDurationMS      float64
	LastAdvanceMinutes  int64
	LastRealmID         uint
	LastUpdatedAt       time.Time
}

func RecordWorldTick(ctx context.Context, realmID uint, advanceMinutes int64, duration time.Duration, failed bool) {
	_ = ctx
	realmLabel := prometheus.Labels{"realm_id": realmIDLabel(realmID)}
	worldTickRuns.With(realmLabel).Inc()
	worldTickDurationMs.With(realmLabel).Observe(duration.Seconds() * 1000)
	worldTickAdvanceMinutes.With(realmLabel).Observe(float64(advanceMinutes))
	if failed {
		worldTickErrors.With(realmLabel).Inc()
	}

	worldTickAggregateMu.Lock()
	worldTickAggregate.totalRuns += 1
	if failed {
		worldTickAggregate.totalFailures += 1
	}
	worldTickAggregate.totalDurationMS += duration.Seconds() * 1000
	worldTickAggregate.totalAdvanceMinutes += float64(advanceMinutes)
	worldTickAggregate.lastDurationMS = duration.Seconds() * 1000
	worldTickAggregate.lastAdvanceMinutes = advanceMinutes
	worldTickAggregate.lastRealmID = realmID
	worldTickAggregate.lastUpdatedAt = time.Now().UTC()
	worldTickAggregateMu.Unlock()
}

func SnapshotWorldTickAggregate() WorldTickAggregateSnapshot {
	worldTickAggregateMu.RLock()
	state := worldTickAggregate
	worldTickAggregateMu.RUnlock()

	avgDuration := 0.0
	avgAdvance := 0.0
	failureRate := 0.0
	if state.totalRuns > 0 {
		avgDuration = state.totalDurationMS / float64(state.totalRuns)
		avgAdvance = state.totalAdvanceMinutes / float64(state.totalRuns)
		failureRate = float64(state.totalFailures) / float64(state.totalRuns)
	}

	return WorldTickAggregateSnapshot{
		TotalRuns:          state.totalRuns,
		TotalFailures:      state.totalFailures,
		FailureRate:        failureRate,
		AvgDurationMS:      avgDuration,
		AvgAdvanceMinutes:  avgAdvance,
		LastDurationMS:     state.lastDurationMS,
		LastAdvanceMinutes: state.lastAdvanceMinutes,
		LastRealmID:        state.lastRealmID,
		LastUpdatedAt:      state.lastUpdatedAt,
	}
}

func StreamConnectionOpened(ctx context.Context) {
	_ = ctx
	streamActiveConnections.Inc()
}

func StreamConnectionClosed(ctx context.Context) {
	_ = ctx
	streamActiveConnections.Dec()
}

func StreamConnectionRejected(ctx context.Context, reason string) {
	_ = ctx
	streamRejectedTotal.WithLabelValues(reason).Inc()
}

func realmIDLabel(realmID uint) string {
	if realmID == 0 {
		return "0"
	}
	return strconv.FormatUint(uint64(realmID), 10)
}
