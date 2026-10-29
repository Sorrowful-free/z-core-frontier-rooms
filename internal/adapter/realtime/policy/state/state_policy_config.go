package state

import (
	"fmt"
	"time"
)

// StatePolicyConfig — configuration of the state room policy (P33, P36):
// the master-migration mode and the full/patch tick intervals are set
// globally per deploy (env STATE_POLICY_*), not per-room.
type StatePolicyConfig struct {
	// AllowMasterMigration — true (default): the master's departure
	// leads to re-election among the remaining peers; false — the room
	// dies with the master's departure (OnLeave answers
	// domain.ErrRoomShouldClose, teardown is in the usecase).
	AllowMasterMigration bool
	// FullStateInterval — the full-state broadcast tick (P36);
	// default DefaultFullStateInterval (4s).
	FullStateInterval time.Duration
	// PatchStateInterval — the delta broadcast tick (P36);
	// default DefaultPatchStateInterval (50ms).
	PatchStateInterval time.Duration
}

// Validate: both tick intervals must be > 0 — they reach
// NewStateRoomPolicy/normalizeTickInterval; fail loudly at load instead
// of silently defaulting a misconfigured deploy.
func (c StatePolicyConfig) Validate() error {
	if c.FullStateInterval <= 0 {
		return fmt.Errorf("state policy config: full state interval must be > 0")
	}
	if c.PatchStateInterval <= 0 {
		return fmt.Errorf("state policy config: patch state interval must be > 0")
	}
	return nil
}
