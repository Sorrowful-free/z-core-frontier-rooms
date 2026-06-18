package bench

import "fmt"

// PeerCountLabel formats sub-bench names like peers=8.
func PeerCountLabel(n int) string {
	return fmt.Sprintf("peers=%d", n)
}

// RoomCountLabel formats sub-bench names like rooms=32.
func RoomCountLabel(n int) string {
	return fmt.Sprintf("rooms=%d", n)
}
