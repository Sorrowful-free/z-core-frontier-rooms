package domain

import "time"

type Claims struct {
	RoomID   RoomID
	PeerID   PeerID
	NickName string
	// HostID - strict binding admit (P18): назначенный ENet host.
	// HostIDAny - ticket не привязан, admit на любом host'е.
	HostID    HostID
	IssuedAt  time.Time
	ExpiresAt time.Time
}
