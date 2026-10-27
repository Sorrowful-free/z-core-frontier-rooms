package http

// JSON DTO control plane (REST).

type createRoomRequest struct {
	Capacity   int            `json:"capacity"`
	NickName   string         `json:"nick_name"`
	Password   string         `json:"password,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
	// Policy — room policy (P29): "state" (дефолт, master-миграция) или
	// "authoritative-host" (уход хоста = смерть комнаты). Пусто → дефолт.
	Policy string `json:"policy,omitempty"`
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
	// ReconnectToken - reconnect-токен (P20): resume под тем же peerID
	// в пределах grace-окна после разрыва.
	ReconnectToken string `json:"reconnect_token"`
	// EnetPort - UDP-порт назначенного ENet host'а (strict binding, P18);
	// отсутствует, когда привязки нет (ENet listener не поднят).
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

// leaveRoomRequest — POST /rooms/:id/leave (P32): self-leave по
// reconnect-токену (base64url, как в ticket-ответе); peerID+roomID
// берутся из claims токена.
type leaveRoomRequest struct {
	ReconnectToken string `json:"reconnect_token"`
}
