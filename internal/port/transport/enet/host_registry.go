package enet

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

// HostAssignment - выбор ENet host'а для ticket'а (P18): HostID идёт в claims
// (strict binding admit), Port - в ответ issue ticket, клиент подключается на него.
type HostAssignment struct {
	HostID domain.HostID
	Port   uint16
}

// HostRegistry - каталог ENet host'ов (shard'ов) процесса для балансировки
// issue ticket'ов. Реализуется delivery/enet (RoomsHandler знает свои host'ы
// и число живых peer'ов на каждом); usecase/room вызывает AssignHost при Issue.
// nil-registry в usecase - ticket'ы без привязки (admit на любом host'е).
type HostRegistry interface {
	// AssignHost выбирает host для нового ticket'а: least-loaded по живым
	// peer'ам host'а, round-robin tie-break при равенстве.
	AssignHost(ctx context.Context) (HostAssignment, error)
}
