// Package mocks — mockgen-generated doubles of the ports (see README in this folder).
//
// Regenerate from the module root:
//
//	go generate ./tests/mocks/...
package mocks

//go:generate go tool mockgen -typed -destination=mock_allocator.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/identity Allocator
//go:generate go tool mockgen -typed -destination=mock_admission.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission Admission
//go:generate go tool mockgen -typed -destination=mock_reservation.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation Reservation
//go:generate go tool mockgen -typed -destination=mock_room_registry.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry RoomRegistry
//go:generate go tool mockgen -typed -destination=mock_connection.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport Connection
//go:generate go tool mockgen -typed -destination=mock_logger.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging Logger
//go:generate go tool mockgen -typed -destination=mock_httplimits.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/httplimits Limits
//go:generate go tool mockgen -typed -destination=mock_metrics.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics Metrics
//go:generate go tool mockgen -typed -destination=mock_realtime_room.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime Room
//go:generate go tool mockgen -typed -destination=mock_realtime_peer.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime Peer
//go:generate go tool mockgen -typed -destination=mock_realtime_peer_factory.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime PeerFactory
//go:generate go tool mockgen -typed -destination=mock_room_factory.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime RoomFactory
//go:generate go tool mockgen -typed -destination=mock_room_policy.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy RoomPolicy
//go:generate go tool mockgen -typed -destination=mock_room_policy_factory.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy RoomPolicyFactory
//go:generate go tool mockgen -typed -destination=mock_ws_connection_factory.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/ws WsConnectionFactory
//go:generate go tool mockgen -typed -destination=mock_enet_connection_factory.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet EnetConnectionFactory
//go:generate go tool mockgen -typed -destination=mock_enet_host_registry.go -package=mocks github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet HostRegistry
