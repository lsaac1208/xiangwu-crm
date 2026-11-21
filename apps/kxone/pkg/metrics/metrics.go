// Package metrics provides prometheus-compatible application instrumentation.
//
// Architecture:
//   - Counters backed by sync/atomic.Int64 — no lock contention on hot paths
//   - Latency samples stored in a ring buffer (capped at maxLatencySamples)
//   - HTTPHandler() emits valid Prometheus text exposition format (RFC 4.0.0.4)
//   - GetStats() provides a JSON-serialisable snapshot for the dashboard API
//
// Migration note: when prometheus/client_golang is available, replace the body
// of HTTPHandler() with promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// latencyBuckets are histogram upper bounds in seconds, covering the 50ms–10s
// range typical of WeCom card API calls.
var latencyBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// maxLatencySamples is the ring-buffer cap per histogram; keeps heap bounded.
const maxLatencySamples = 4096

// Metrics holds prometheus-compatible counters and latency histograms.
// Always access via GetGlobalMetrics(); do not copy this struct.
type Metrics struct {
	// Card send — counter pair + histogram
	cardSendTotal   atomic.Int64
	cardSendSuccess atomic.Int64
	cardSendFailed  atomic.Int64
	cardSendMu      sync.Mutex
	cardSendSamples []float64 // in seconds

	// Card update — counter pair + histogram
	cardUpdateTotal   atomic.Int64
	cardUpdateSuccess atomic.Int64
	cardUpdateFailed  atomic.Int64
	cardUpdateMu      sync.Mutex
	cardUpdateSamples []float64

	// Lead state-machine transitions
	stateTxTotal   atomic.Int64
	stateTxSuccess atomic.Int64
	stateTxFailed  atomic.Int64

	// Business events
	leadAssignTotal atomic.Int64
	leadClaimTotal  atomic.Int64
	leadClaimFailed atomic.Int64

	staleCardUpdating atomic.Int64
}

var globalMetrics = &Metrics{}

// GetGlobalMetrics returns the process-wide singleton.
func GetGlobalMetrics() *Metrics { return globalMetrics }

// ── Record methods ────────────────────────────────────────────────────────────

func (m *Metrics) RecordCardSend(success bool, latency time.Duration) {
	m.cardSendTotal.Add(1)
	if success {
		m.cardSendSuccess.Add(1)
	} else {
		m.cardSendFailed.Add(1)
	}
	appendSample(&m.cardSendMu, &m.cardSendSamples, latency.Seconds())
}

func (m *Metrics) RecordCardUpdate(success bool, latency time.Duration) {
	m.cardUpdateTotal.Add(1)
	if success {
		m.cardUpdateSuccess.Add(1)
	} else {
		m.cardUpdateFailed.Add(1)
	}
	appendSample(&m.cardUpdateMu, &m.cardUpdateSamples, latency.Seconds())
}

func (m *Metrics) RecordStateTransition(success bool) {
	m.stateTxTotal.Add(1)
	if success {
		m.stateTxSuccess.Add(1)
	} else {
		m.stateTxFailed.Add(1)
	}
}

func (m *Metrics) RecordLeadAssign() { m.leadAssignTotal.Add(1) }

func (m *Metrics) RecordLeadClaim(success bool) {
	m.leadClaimTotal.Add(1)
	if !success {
		m.leadClaimFailed.Add(1)
	}
}

func (m *Metrics) SetStaleCardUpdating(count int64) {
	if count < 0 {
		count = 0
	}
	m.staleCardUpdating.Store(count)
}

// ── Read-back for dashboard JSON ──────────────────────────────────────────────

// GetStats returns a JSON-serialisable snapshot for the dashboard API health
// field.  Counter values are read via atomic.Load; histogram samples are
// snapshot-copied under a brief lock.
func (m *Metrics) GetStats() map[string]interface{} {
	cSendTotal := m.cardSendTotal.Load()
	cSendOK := m.cardSendSuccess.Load()
	cSendFail := m.cardSendFailed.Load()

	cUpdTotal := m.cardUpdateTotal.Load()
	cUpdOK := m.cardUpdateSuccess.Load()
	cUpdFail := m.cardUpdateFailed.Load()

	sTxTotal := m.stateTxTotal.Load()
	sTxOK := m.stateTxSuccess.Load()
	sTxFail := m.stateTxFailed.Load()

	lAssign := m.leadAssignTotal.Load()
	lClaimTotal := m.leadClaimTotal.Load()
	lClaimFail := m.leadClaimFailed.Load()
	staleUpdating := m.staleCardUpdating.Load()

	// snapshot histogram samples under their own locks
	sendMs := snapshotMs(&m.cardSendMu, &m.cardSendSamples)
	updMs := snapshotMs(&m.cardUpdateMu, &m.cardUpdateSamples)

	return map[string]interface{}{
		"card_send": map[string]interface{}{
			"total":          cSendTotal,
			"success":        cSendOK,
			"failed":         cSendFail,
			"success_rate":   successRate(cSendOK, cSendTotal),
			"avg_latency_ms": calculateAverage(sendMs),
			"p95_latency_ms": calculateP95(sendMs),
		},
		"card_update": map[string]interface{}{
			"total":          cUpdTotal,
			"success":        cUpdOK,
			"failed":         cUpdFail,
			"success_rate":   successRate(cUpdOK, cUpdTotal),
			"avg_latency_ms": calculateAverage(updMs),
			"p95_latency_ms": calculateP95(updMs),
		},
		"state_transition": map[string]interface{}{
			"total":        sTxTotal,
			"success":      sTxOK,
			"failed":       sTxFail,
			"success_rate": successRate(sTxOK, sTxTotal),
		},
		"business": map[string]interface{}{
			"lead_assign_total":       lAssign,
			"lead_claim_total":        lClaimTotal,
			"lead_claim_failed":       lClaimFail,
			"lead_claim_success_rate": successRate(lClaimTotal-lClaimFail, lClaimTotal),
		},
		"worker": map[string]interface{}{
			"stale_card_updating": staleUpdating,
		},
	}
}

// ── Prometheus exposition ─────────────────────────────────────────────────────

// HTTPHandler returns an http.Handler that serves Prometheus text exposition
// format (Content-Type: text/plain; version=0.0.4).
//
// Wire it to /metrics behind AuthMiddleware so the route is already protected.
//
// Migration: once prometheus/client_golang is available, replace this body with
//
//	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
func (m *Metrics) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprint(w, m.prometheusText())
	})
}

// prometheusText renders valid Prometheus text exposition format.
// Counters carry TYPE counter; histograms carry TYPE histogram with buckets.
func (m *Metrics) prometheusText() string {
	// snapshot counters
	cSendTotal := m.cardSendTotal.Load()
	cSendOK := m.cardSendSuccess.Load()
	cSendFail := m.cardSendFailed.Load()
	cUpdTotal := m.cardUpdateTotal.Load()
	cUpdOK := m.cardUpdateSuccess.Load()
	cUpdFail := m.cardUpdateFailed.Load()
	sTxTotal := m.stateTxTotal.Load()
	sTxOK := m.stateTxSuccess.Load()
	sTxFail := m.stateTxFailed.Load()
	lAssign := m.leadAssignTotal.Load()
	lClaimTotal := m.leadClaimTotal.Load()
	lClaimFail := m.leadClaimFailed.Load()
	staleUpdating := m.staleCardUpdating.Load()

	// snapshot histogram samples
	m.cardSendMu.Lock()
	sendSamples := append([]float64{}, m.cardSendSamples...)
	m.cardSendMu.Unlock()
	m.cardUpdateMu.Lock()
	updSamples := append([]float64{}, m.cardUpdateSamples...)
	m.cardUpdateMu.Unlock()

	var b strings.Builder

	// ── counter family helpers ─────────────────────────────────────────────
	writeCounter := func(name, help string, value int64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n%s %d\n",
			name, help, name, name, value)
	}

	// ── histogram helper ──────────────────────────────────────────────────
	writeHistogram := func(name, help string, samples []float64) {
		sort.Float64s(samples)
		total := int64(len(samples))
		var sum float64
		for _, s := range samples {
			sum += s
		}
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s histogram\n", name, help, name)
		for _, le := range latencyBuckets {
			var count int64
			for _, s := range samples {
				if s <= le {
					count++
				}
			}
			fmt.Fprintf(&b, "%s_bucket{le=\"%g\"} %d\n", name, le, count)
		}
		fmt.Fprintf(&b, "%s_bucket{le=\"+Inf\"} %d\n", name, total)
		fmt.Fprintf(&b, "%s_sum %g\n", name, sum)
		fmt.Fprintf(&b, "%s_count %d\n", name, total)
	}

	// ── card send ─────────────────────────────────────────────────────────
	writeCounter("kxone_card_send_total", "Total card send attempts.", cSendTotal)
	writeCounter("kxone_card_send_success_total", "Successful card sends.", cSendOK)
	writeCounter("kxone_card_send_failed_total", "Failed card sends.", cSendFail)
	writeHistogram("kxone_card_send_duration_seconds",
		"Card send request latency in seconds.", sendSamples)

	// ── card update ───────────────────────────────────────────────────────
	writeCounter("kxone_card_update_total", "Total card update attempts.", cUpdTotal)
	writeCounter("kxone_card_update_success_total", "Successful card updates.", cUpdOK)
	writeCounter("kxone_card_update_failed_total", "Failed card updates.", cUpdFail)
	writeHistogram("kxone_card_update_duration_seconds",
		"Card update request latency in seconds.", updSamples)

	// ── state transitions ─────────────────────────────────────────────────
	writeCounter("kxone_state_transition_total", "Total lead state transition attempts.", sTxTotal)
	writeCounter("kxone_state_transition_success_total", "Successful lead state transitions.", sTxOK)
	writeCounter("kxone_state_transition_failed_total", "Failed lead state transitions.", sTxFail)

	// ── business events ───────────────────────────────────────────────────
	writeCounter("kxone_lead_assign_total", "Total lead assignments.", lAssign)
	writeCounter("kxone_lead_claim_total", "Total lead claim attempts.", lClaimTotal)
	writeCounter("kxone_lead_claim_failed_total", "Failed lead claims.", lClaimFail)
	fmt.Fprintf(&b, "# HELP kxone_card_updating_stale Stale card operations with an ambiguous external result.\n"+
		"# TYPE kxone_card_updating_stale gauge\n"+
		"kxone_card_updating_stale %d\n", staleUpdating)

	return b.String()
}

// ── Test helper ───────────────────────────────────────────────────────────────

// Reset zeros all counters and clears histogram samples. Only for tests.
func (m *Metrics) Reset() {
	m.cardSendTotal.Store(0)
	m.cardSendSuccess.Store(0)
	m.cardSendFailed.Store(0)
	m.cardUpdateTotal.Store(0)
	m.cardUpdateSuccess.Store(0)
	m.cardUpdateFailed.Store(0)
	m.stateTxTotal.Store(0)
	m.stateTxSuccess.Store(0)
	m.stateTxFailed.Store(0)
	m.leadAssignTotal.Store(0)
	m.leadClaimTotal.Store(0)
	m.leadClaimFailed.Store(0)
	m.staleCardUpdating.Store(0)

	m.cardSendMu.Lock()
	m.cardSendSamples = m.cardSendSamples[:0]
	m.cardSendMu.Unlock()

	m.cardUpdateMu.Lock()
	m.cardUpdateSamples = m.cardUpdateSamples[:0]
	m.cardUpdateMu.Unlock()
}

// ── internal helpers ──────────────────────────────────────────────────────────

// appendSample adds v to the ring buffer; drops the oldest entry when full.
func appendSample(mu *sync.Mutex, samples *[]float64, v float64) {
	mu.Lock()
	if len(*samples) >= maxLatencySamples {
		copy(*samples, (*samples)[1:])
		*samples = (*samples)[:len(*samples)-1]
	}
	*samples = append(*samples, v)
	mu.Unlock()
}

// snapshotMs returns a millisecond copy of the sample slice under its lock.
func snapshotMs(mu *sync.Mutex, samples *[]float64) []float64 {
	mu.Lock()
	out := make([]float64, len(*samples))
	for i, s := range *samples {
		out[i] = s * 1000
	}
	mu.Unlock()
	return out
}

func successRate(ok, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(ok) / float64(total) * 100
}

func calculateAverage(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	var sum float64
	for _, v := range data {
		sum += v
	}
	return sum / float64(len(data))
}

func calculateP95(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	sorted := make([]float64, len(data))
	copy(sorted, data)
	sort.Float64s(sorted)
	idx := int(float64(len(sorted)) * 0.95)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
