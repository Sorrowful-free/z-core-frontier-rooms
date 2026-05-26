package http

// JSON DTO control plane (REST).

type createRoomRequest struct {
	ID       int64  `json:"id"`
	Capacity int    `json:"capacity"`
	Password string `json:"password,omitempty"`
}

type deleteRoomRequest struct {
	Password string `json:"password,omitempty"`
}

type issueTicketRequest struct {
	PeerID   int64  `json:"peer_id"`
	Password string `json:"password"`
}

type issueTicketResponse struct {
	Token string `json:"token"`
}

type roomResponse struct {
	ID    int64           `json:"id"`
	Peers []peerResponse  `json:"peers"`
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
