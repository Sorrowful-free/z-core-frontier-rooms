// Package metrics — prometheus implementation of `port/metrics.Metrics` (P19).
//
// Its own `prometheus.Registry` (not the default): tests and embedders do
// not inherit foreign collectors; in addition to application signals the
// standard Go and process collectors are registered. `Gatherer()` exposes
// the registry for exposition (`delivery/admin` serves the metrics text at
// `GET /metrics`).
package metrics

import (
	"fmt"
	"strconv"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

var _ portmetrics.Metrics = (*Metrics)(nil)

// application metrics namespace: `rooms_*`.
const namespace = "rooms"

// Bounded label values (a fixed set — cardinality does not grow with
// input data).
const (
	labelOpcode = "opcode"
	labelQueue  = "queue"
	labelOp     = "op"
	labelSweep  = "sweep"
	labelKind   = "kind"
	labelHost   = "host"
	labelRoute  = "route"
	labelMethod = "method"
	labelStatus = "status"

	queueRoomIncoming = "room_incoming"
	queuePeerOutbound = "peer_outbound"
	queueEnetIncoming = "enet_incoming"

	opReserve = "reserve"
	opAdmit   = "admit"
	opRevoke  = "revoke"

	sweepOrphanAdmitted = "orphan_admitted"
	sweepIdlePeers      = "idle_peers"
)

// Summary objectives for duration signals (tick, host loop queue latency).
var durationObjectives = map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001}

type Metrics struct {
	registry *prometheus.Registry

	activeRooms     prometheus.Gauge
	activePeers     prometheus.Gauge
	peerJoins       prometheus.Counter
	peerLeaves      prometheus.Counter
	peerDisconnects prometheus.Counter
	peerReconnects  prometheus.Counter
	joinRejects     *prometheus.CounterVec
	queueDrops      *prometheus.CounterVec
	masterElection  prometheus.Counter
	reservationOps  *prometheus.CounterVec
	sweepProcessed  *prometheus.CounterVec

	roomIncomingDepth        prometheus.Gauge
	peerOutboundDepth        prometheus.Gauge
	roomTickDuration         *prometheus.SummaryVec
	peerSendErrors           prometheus.Counter
	enetHostPeers            *prometheus.GaugeVec
	enetHostLoopQueueLatency prometheus.Summary
	reservationActiveSlots   prometheus.Gauge
	sweepRuns                *prometheus.CounterVec
	httpRequestDuration      *prometheus.HistogramVec
}

// New builds the registry with all P19 signals + Go/process collectors.
// cfg.Validate is called before registration (adapter/config rule).
func New(cfg MetricsConfig) (*Metrics, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	reg := prometheus.NewRegistry()
	m := &Metrics{
		registry: reg,
		activeRooms: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Name: "active_rooms",
			Help: "Live rooms in the registry.",
		}),
		activePeers: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Name: "active_peers",
			Help: "Live peers across all rooms.",
		}),
		peerJoins: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Name: "peer_joins_total",
			Help: "Peer joins (room-loop membership transitions; Replace = leave+join pair).",
		}),
		peerLeaves: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Name: "peer_leaves_total",
			Help: "Peer leaves (room-loop membership transitions).",
		}),
		peerDisconnects: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Name: "peer_disconnects_total",
			Help: "Peers marked disconnected into reconnect grace (P20).",
		}),
		peerReconnects: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Name: "peer_reconnects_total",
			Help: "Successful reconnects (resume under the same peerID, P20).",
		}),
		joinRejects: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "join_rejects_total",
			Help: "Join/admit reject frames sent, by wire opcode.",
		}, []string{labelOpcode}),
		queueDrops: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "queue_drops_total",
			Help: "Frames dropped on full queues (room incoming / peer outbound / enet session).",
		}, []string{labelQueue}),
		masterElection: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Name: "master_elections_total",
			Help: "Successful master elections (first election and re-elections).",
		}),
		reservationOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "reservation_ops_total",
			Help: "Reservation slot transitions (reserve/admit/revoke).",
		}, []string{labelOp}),
		sweepProcessed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "sweep_processed_total",
			Help: "Entries processed per background sweep run.",
		}, []string{labelSweep}),
		roomIncomingDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Name: "room_incoming_depth",
			Help: "Room incoming queue depth (len(chan) sampled at enqueue/dequeue; last-write-wins approximation).",
		}),
		peerOutboundDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Name: "peer_outbound_depth",
			Help: "Peer outbound queue depth (len(chan) sampled at enqueue/dequeue; last-write-wins approximation).",
		}),
		roomTickDuration: prometheus.NewSummaryVec(prometheus.SummaryOpts{
			Namespace: namespace, Name: "room_tick_seconds",
			Help:       "Room loop tick duration by kind (full/patch).",
			Objectives: durationObjectives,
		}, []string{labelKind}),
		peerSendErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Name: "peer_send_errors_total",
			Help: "Send errors on live peer connections (expected teardown excluded).",
		}),
		enetHostPeers: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace, Name: "enet_host_peers",
			Help: "Live ENet sessions per host (host label = domain.HostID, bounded by ENET_LISTEN_PORTS).",
		}, []string{labelHost}),
		enetHostLoopQueueLatency: prometheus.NewSummary(prometheus.SummaryOpts{
			Namespace: namespace, Name: "enet_host_loop_queue_latency_seconds",
			Help:       "HostLoop command queue wait (enqueue -> exec), aggregated across hosts.",
			Objectives: durationObjectives,
		}),
		reservationActiveSlots: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Name: "reservation_active_slots",
			Help: "Live reservation slots across all rooms.",
		}),
		sweepRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "sweep_runs_total",
			Help: "Background sweep passes (separate from processed entries).",
		}, []string{labelSweep}),
		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "http_request_duration_seconds",
			Help:    "Public HTTP API request duration (route template, method, status).",
			Buckets: prometheus.DefBuckets,
		}, []string{labelRoute, labelMethod, labelStatus}),
	}

	reg.MustRegister(
		m.activeRooms,
		m.activePeers,
		m.peerJoins,
		m.peerLeaves,
		m.peerDisconnects,
		m.peerReconnects,
		m.joinRejects,
		m.queueDrops,
		m.masterElection,
		m.reservationOps,
		m.sweepProcessed,
		m.roomIncomingDepth,
		m.peerOutboundDepth,
		m.roomTickDuration,
		m.peerSendErrors,
		m.enetHostPeers,
		m.enetHostLoopQueueLatency,
		m.reservationActiveSlots,
		m.sweepRuns,
		m.httpRequestDuration,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return m, nil
}

// Gatherer — registry for exposition (`delivery/admin` `GET /metrics`).
func (m *Metrics) Gatherer() prometheus.Gatherer {
	return m.registry
}

func (m *Metrics) SetActiveRooms(rooms int) {
	m.activeRooms.Set(float64(rooms))
}

func (m *Metrics) AddActivePeers(delta int) {
	m.activePeers.Add(float64(delta))
}

func (m *Metrics) PeerJoined() {
	m.peerJoins.Inc()
}

func (m *Metrics) PeerLeft() {
	m.peerLeaves.Inc()
}

func (m *Metrics) PeerDisconnected() {
	m.peerDisconnects.Inc()
}

func (m *Metrics) ReconnectSucceeded() {
	m.peerReconnects.Inc()
}

func (m *Metrics) JoinRejected(op domain.OpCode) {
	m.joinRejects.WithLabelValues(fmt.Sprintf("%#x", op)).Inc()
}

func (m *Metrics) RoomIncomingDropped() {
	m.queueDrops.WithLabelValues(queueRoomIncoming).Inc()
}

func (m *Metrics) PeerOutboundDropped() {
	m.queueDrops.WithLabelValues(queuePeerOutbound).Inc()
}

func (m *Metrics) EnetIncomingDropped() {
	m.queueDrops.WithLabelValues(queueEnetIncoming).Inc()
}

func (m *Metrics) RoomIncomingDepth(n int) {
	m.roomIncomingDepth.Set(float64(n))
}

func (m *Metrics) PeerOutboundDepth(n int) {
	m.peerOutboundDepth.Set(float64(n))
}

func (m *Metrics) RoomTickObserved(kind string, d time.Duration) {
	m.roomTickDuration.WithLabelValues(kind).Observe(d.Seconds())
}

func (m *Metrics) PeerSendError() {
	m.peerSendErrors.Inc()
}

func (m *Metrics) EnetHostPeers(hostID domain.HostID, peers int) {
	m.enetHostPeers.WithLabelValues(strconv.Itoa(int(hostID))).Set(float64(peers))
}

func (m *Metrics) EnetHostLoopQueueLatency(d time.Duration) {
	m.enetHostLoopQueueLatency.Observe(d.Seconds())
}

func (m *Metrics) MasterElected() {
	m.masterElection.Inc()
}

func (m *Metrics) ReservationReserved() {
	m.reservationOps.WithLabelValues(opReserve).Inc()
}

func (m *Metrics) ReservationAdmitted() {
	m.reservationOps.WithLabelValues(opAdmit).Inc()
}

func (m *Metrics) ReservationRevoked() {
	m.reservationOps.WithLabelValues(opRevoke).Inc()
}

func (m *Metrics) ReservationActiveSlots(delta int) {
	m.reservationActiveSlots.Add(float64(delta))
}

func (m *Metrics) OrphanAdmittedRevoked(n int) {
	m.sweepProcessed.WithLabelValues(sweepOrphanAdmitted).Add(float64(n))
}

func (m *Metrics) IdlePeersEvicted(n int) {
	m.sweepProcessed.WithLabelValues(sweepIdlePeers).Add(float64(n))
}

func (m *Metrics) SweepRan(sweep string) {
	m.sweepRuns.WithLabelValues(sweep).Inc()
}

func (m *Metrics) HTTPRequestObserved(route, method string, status int, d time.Duration) {
	m.httpRequestDuration.WithLabelValues(route, method, strconv.Itoa(status)).Observe(d.Seconds())
}
