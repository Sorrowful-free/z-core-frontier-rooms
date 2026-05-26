// Package mocks — сгенерированные mockgen-дублёры портов (см. README в этой папке).
//
// Перегенерация из корня модуля:
//
//	go generate ./tests/mocks/...
package mocks

//go:generate go tool mockgen -typed -destination=mock_admission.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission Admission
//go:generate go tool mockgen -typed -destination=mock_reservation.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation Reservation
//go:generate go tool mockgen -typed -destination=mock_room_registry.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry RoomRegistry
//go:generate go tool mockgen -typed -destination=mock_connection.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport Connection
//go:generate go tool mockgen -typed -destination=mock_logger.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging Logger
//go:generate go tool mockgen -typed -destination=mock_realtime_room.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime Room
//go:generate go tool mockgen -typed -destination=mock_realtime_peer.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime Peer
//go:generate go tool mockgen -typed -destination=mock_realtime_peer_factory.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime PeerFactory
