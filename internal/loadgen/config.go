package loadgen

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

// RoomMixEntry — an element of LOADGEN_ROOM_MIX: a capacity/occupancy room
// with a weight.
type RoomMixEntry struct {
	Capacity  int
	Occupancy int
	Weight    int
}

// weightedOption — an element of a weighted mix ("name:weight"):
// transport/netem.
type weightedOption struct {
	Value  string
	Weight int
}

// LOADGEN_MODE modes.
const (
	ModeState     = "state"     // clients send PatchInput/PatchEntities
	ModeOccupancy = "occupancy" // clients only hold the connection
)

// Config — run configuration, assembled from env (LOADGEN_*).
type Config struct {
	// External server addresses (no scheme).
	HTTPAddr  string // REST control plane
	WSAddr    string // WS data plane (/ws?token=)
	AdminAddr string // admin listener (GET /metrics), for drain polling
	// EnetAddr — host (no port) of the ENet listeners; the port comes from
	// the ticket response (enet_port, strict binding). Default — the host
	// part of WSAddr.
	EnetAddr string

	Rooms        int
	RoomMix      []RoomMixEntry
	TransportMix []weightedOption // "ws"|"enet" (enet — only in a -tags enet build)
	Mode         string

	InputHz    float64 // PatchInput per client (mode=state)
	EntitiesHz float64 // PatchEntities per master client (mode=state)
	Ramp       float64 // rooms/sec during ramp-up
	Duration   time.Duration
	// DrainTimeout — deadline for waiting until rooms_active_rooms +
	// rooms_reservation_active_slots reach zero. Default ~2×max(idle=30s,
	// sweep=10s).
	DrainTimeout time.Duration

	ReconnectRate  float64       // fraction of clients doing abrupt close → resume
	ReconnectEvery time.Duration // drop period for the selected clients

	// NetemShare — distribution of clients across netem profiles (inc2,
	// LOADGEN_NETEM_SHARE="clean:30,bad:50,ugly:20"); empty — dials without
	// SO_MARK (host mode, tc untouched).
	NetemShare []weightedOption

	ReportPath string
	LogEvery   time.Duration
}

// LoadConfigFromEnv — single load point of the loadgen env config.
func LoadConfigFromEnv() (*Config, error) {
	cfg := &Config{
		HTTPAddr:  envOr("LOADGEN_HTTP_ADDR", "127.0.0.1:3000"),
		AdminAddr: envOr("LOADGEN_ADMIN_ADDR", "127.0.0.1:9091"),

		Mode:         envOr("LOADGEN_MODE", ModeState),
		ReportPath:   envOr("LOADGEN_REPORT", "./report.json"),
		Rooms:        1000,
		InputHz:      20,
		EntitiesHz:   20,
		Ramp:         10,
		Duration:     60 * time.Second,
		DrainTimeout: 60 * time.Second,

		ReconnectEvery: 60 * time.Second,
		LogEvery:       10 * time.Second,
	}
	cfg.WSAddr = envOr("LOADGEN_WS_ADDR", cfg.HTTPAddr)
	cfg.EnetAddr = envOr("LOADGEN_ENET_ADDR", enetHost(cfg.WSAddr))

	var err error
	if cfg.Rooms, err = envInt("LOADGEN_ROOMS", cfg.Rooms); err != nil {
		return nil, err
	}
	if cfg.RoomMix, err = parseRoomMix(envOr("LOADGEN_ROOM_MIX", "4/4:70,8/6:20,16/4:10")); err != nil {
		return nil, fmt.Errorf("LOADGEN_ROOM_MIX: %w", err)
	}
	if cfg.TransportMix, err = parseMix(envOr("LOADGEN_TRANSPORT_MIX", "ws:100"), mapTransport); err != nil {
		return nil, fmt.Errorf("LOADGEN_TRANSPORT_MIX: %w", err)
	}
	if cfg.InputHz, err = envFloat("LOADGEN_INPUT_HZ", cfg.InputHz); err != nil {
		return nil, err
	}
	if cfg.EntitiesHz, err = envFloat("LOADGEN_ENTITIES_HZ", cfg.EntitiesHz); err != nil {
		return nil, err
	}
	if cfg.Ramp, err = envFloat("LOADGEN_RAMP", cfg.Ramp); err != nil {
		return nil, err
	}
	if cfg.Duration, err = envDuration("LOADGEN_DURATION", cfg.Duration); err != nil {
		return nil, err
	}
	if cfg.DrainTimeout, err = envDuration("LOADGEN_DRAIN_TIMEOUT", cfg.DrainTimeout); err != nil {
		return nil, err
	}
	if cfg.ReconnectRate, err = envFloat("LOADGEN_RECONNECT_RATE", 0); err != nil {
		return nil, err
	}
	if cfg.ReconnectEvery, err = envDuration("LOADGEN_RECONNECT_EVERY", cfg.ReconnectEvery); err != nil {
		return nil, err
	}
	if raw := envOr("LOADGEN_NETEM_SHARE", ""); raw != "" {
		if cfg.NetemShare, err = parseMix(raw, mapNetem); err != nil {
			return nil, fmt.Errorf("LOADGEN_NETEM_SHARE: %w", err)
		}
	}
	if cfg.LogEvery, err = envDuration("LOADGEN_LOG_EVERY", cfg.LogEvery); err != nil {
		return nil, err
	}
	return cfg, cfg.Validate()
}

// Validate — config consistency checks.
func (c *Config) Validate() error {
	if c.Rooms <= 0 {
		return fmt.Errorf("LOADGEN_ROOMS must be > 0, got %d", c.Rooms)
	}
	for _, e := range c.RoomMix {
		if e.Capacity <= 0 || e.Capacity > domain.MaxRoomCapacity {
			return fmt.Errorf("room mix capacity %d out of 1..%d", e.Capacity, domain.MaxRoomCapacity)
		}
		if e.Occupancy <= 0 || e.Occupancy > e.Capacity {
			return fmt.Errorf("room mix occupancy %d out of 1..capacity %d", e.Occupancy, e.Capacity)
		}
	}
	switch c.Mode {
	case ModeState, ModeOccupancy:
	default:
		return fmt.Errorf("LOADGEN_MODE %q: want %q|%q", c.Mode, ModeState, ModeOccupancy)
	}
	if c.ReconnectRate < 0 || c.ReconnectRate > 1 {
		return fmt.Errorf("LOADGEN_RECONNECT_RATE %v out of 0..1", c.ReconnectRate)
	}
	if c.Ramp <= 0 {
		return fmt.Errorf("LOADGEN_RAMP must be > 0")
	}
	return nil
}

// mapTransport — transport name in a mix.
func mapTransport(name string) (string, error) {
	switch name {
	case "ws", "enet":
		return name, nil
	default:
		return "", fmt.Errorf("unknown transport %q", name)
	}
}

// parseRoomMix — "capacity/occupancy:weight,..." → []RoomMixEntry.
func parseRoomMix(s string) ([]RoomMixEntry, error) {
	var out []RoomMixEntry
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		co, w, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("entry %q: want capacity/occupancy:weight", part)
		}
		capStr, occStr, ok := strings.Cut(co, "/")
		if !ok {
			return nil, fmt.Errorf("entry %q: want capacity/occupancy", part)
		}
		var e RoomMixEntry
		var err error
		if e.Capacity, err = strconv.Atoi(capStr); err != nil {
			return nil, fmt.Errorf("entry %q: %w", part, err)
		}
		if e.Occupancy, err = strconv.Atoi(occStr); err != nil {
			return nil, fmt.Errorf("entry %q: %w", part, err)
		}
		if e.Weight, err = strconv.Atoi(w); err != nil {
			return nil, fmt.Errorf("entry %q: %w", part, err)
		}
		if e.Weight <= 0 {
			return nil, fmt.Errorf("entry %q: weight must be > 0", part)
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty mix")
	}
	return out, nil
}

// parseMix — "name:weight,..." → []weightedOption, names via mapFn.
func parseMix(s string, mapFn func(string) (string, error)) ([]weightedOption, error) {
	var out []weightedOption
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, w, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("entry %q: want name:weight", part)
		}
		value, err := mapFn(strings.TrimSpace(name))
		if err != nil {
			return nil, err
		}
		weight, err := strconv.Atoi(strings.TrimSpace(w))
		if err != nil {
			return nil, fmt.Errorf("entry %q: %w", part, err)
		}
		if weight <= 0 {
			return nil, fmt.Errorf("entry %q: weight must be > 0", part)
		}
		out = append(out, weightedOption{Value: value, Weight: weight})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty mix")
	}
	return out, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func envFloat(key string, def float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return f, nil
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}
