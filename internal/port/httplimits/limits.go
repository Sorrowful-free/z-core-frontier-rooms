package httplimits

// Limits — control plane: ёмкость инстанса и rate limit REST (per client IP).
type Limits interface {
	AllowCreateRoom(currentRoomCount int) error
	AllowHTTPCreate(clientIP string) bool
	AllowHTTPIssueTicket(clientIP string) bool
}
