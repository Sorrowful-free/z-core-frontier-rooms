package http

// JSON DTO control plane (REST).

type createRoomRequest struct {
	Capacity   int            `json:"capacity"`
	NickName   string         `json:"nick_name"`
	Password   string         `json:"password,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type deleteRoomRequest struct {
	Password string `json:"password,omitempty"`
}

type issueTicketRequest struct {
	NickName string `json:"nick_name"`
	Password string `json:"password"`
}

type issueTicketResponse struct {
	Token string `json:"token"`
	// ReconnectToken - reconnect token (P20): resume under the same peerID
	// within the grace window after a disconnect.
	ReconnectToken string `json:"reconnect_token"`
	// EnetPort - UDP port of the assigned ENet host (strict binding, P18);
	// absent when there is no binding (ENet listener is not up).
	EnetPort uint16       `json:"enet_port,omitempty"`
	Room     roomResponse `json:"room"`
}

type roomResponse struct {
	ID         int64          `json:"id"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Peers      []peerResponse `json:"peers"`
}

type peerResponse struct {
	PeerID   int64  `json:"peer_id"`
	NickName string `json:"nick_name"`
	Ping     int64  `json:"ping"`
}

type roomListResponse struct {
	Rooms []roomResponse `json:"rooms"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// leaveRoomRequest — POST /rooms/:id/leave (P32): self-leave via
// reconnect token (base64url, same as in the ticket response); peerID+roomID
// are taken from the token claims.
type leaveRoomRequest struct {
	ReconnectToken string `json:"reconnect_token"`
}
