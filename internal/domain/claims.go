package domain

import "time"

type Claims struct {
	RoomID    RoomID
	PeerID    PeerID
	NickName  string
	IssuedAt  time.Time
	ExpiresAt time.Time
}
