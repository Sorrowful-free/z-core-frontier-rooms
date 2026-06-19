package http

// JSON DTO control plane (REST).

type createRoomRequest struct {
	ID         int64          `json:"id"`
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
	Token string       `json:"token"`
	Room  roomResponse `json:"room"`
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
