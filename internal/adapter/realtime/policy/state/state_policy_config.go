package state

// StatePolicyConfig — конфигурация state room-policy (P33): режим
// master-миграции задаётся глобально на деплой (env
// STATE_POLICY_ALLOW_MASTER_MIGRATION), не per-room.
type StatePolicyConfig struct {
	// AllowMasterMigration — true (дефолт): уход master'а ведёт к
	// перевыборам среди оставшихся peer'ов; false — комната умирает с
	// уходом master'а (OnLeave отвечает
	// domain.ErrRoomShouldClose, teardown — в usecase).
	AllowMasterMigration bool
}

// Validate: полей для проверки нет — метод для единообразия ModuleConfig.
func (c StatePolicyConfig) Validate() error {
	return nil
}
