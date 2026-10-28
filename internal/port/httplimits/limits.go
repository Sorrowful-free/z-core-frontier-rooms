package httplimits

// Limits — control plane: instance capacity and REST rate limit (per client IP).
type Limits interface {
	AllowCreateRoom(currentRoomCount int) error
	AllowHTTPCreate(clientIP string) bool
	AllowHTTPIssueTicket(clientIP string) bool
	AllowHTTPDelete(clientIP string) bool
	AllowHTTPList(clientIP string) bool
}
