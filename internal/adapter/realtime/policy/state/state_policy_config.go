package state

// StatePolicyConfig — configuration of the state room policy (P33):
// the master-migration mode is set globally per deploy (env
// STATE_POLICY_ALLOW_MASTER_MIGRATION), not per-room.
type StatePolicyConfig struct {
	// AllowMasterMigration — true (default): the master's departure
	// leads to re-election among the remaining peers; false — the room
	// dies with the master's departure (OnLeave answers
	// domain.ErrRoomShouldClose, teardown is in the usecase).
	AllowMasterMigration bool
}

// Validate: there are no fields to check — the method exists for ModuleConfig uniformity.
func (c StatePolicyConfig) Validate() error {
	return nil
}
