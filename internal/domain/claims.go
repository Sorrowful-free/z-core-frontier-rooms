package domain

import "time"

type Claims struct {
	RoomID    RoomID
	PeerID    PeerID
	IssuedAt  time.Time
	ExpiresAt time.Time
}
