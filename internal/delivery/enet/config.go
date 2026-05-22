package enet

// Config — параметры ENet host (отдельный слушатель, не Fiber).
type Config struct {
	ListenPort       uint16
	PeerLimit        uint64
	ChannelLimit     uint64
	ServiceTimeoutMs uint32
}

func DefaultConfig() Config {
	return Config{
		ListenPort:       7777,
		PeerLimit:        64,
		ChannelLimit:     2,
		ServiceTimeoutMs: 10,
	}
}
