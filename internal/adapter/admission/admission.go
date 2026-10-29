package admission

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	portadmission "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
)

var (
	ErrInvalidCredentials = domain.ErrInvalidCredentials
	ErrInvalidToken       = domain.ErrInvalidToken
	ErrExpiredToken       = domain.ErrExpiredToken
)

// First byte of the credential is the kind:
//   - join-ticket (kind=1): kind | roomID | peerID | issuedAt | expiresAt |
//     hostID u16 | nickLen | nick | MAC; hostID=0 (domain.HostIDUnbound) -
//     ticket not bound to a host (no ENet host assigned, e.g. WS-only deploy).
//   - reconnect token (kind=2, P20): kind | roomID | peerID | hostID u16 |
//     MAC — fixed-length credential (51 bytes), no nickName and no
//     timestamps.
//
// Verify dispatches on the kind byte: for join, claims.HostID is read from
// the payload and expiry is checked; for reconnect, claims.Kind =
// ClaimsKindReconnect and there is no expiry (grace is server-side slot
// state).
const (
	tokenKindJoin      = 1
	tokenKindReconnect = 2

	fixedPayloadSizeJoin      = 1 + 8 + 8 + 8 + 8 + 2 + 1 // kind | roomID | peerID | issuedAt | expiresAt | hostID | nickLen
	fixedPayloadSizeReconnect = 1 + 8 + 8 + 2             // kind | roomID | peerID | hostID
	macSize                   = sha256.Size
	tokenSizeReconnect        = fixedPayloadSizeReconnect + macSize
	minTokenSizeJoin          = fixedPayloadSizeJoin + macSize + 1 // + at least 1 byte of nick
	hostIDOffset              = 33
	nickLenOffset             = 35
	nickOffset                = 36
	hostIDOffsetReconnect     = 17
)

// Admission — minimal port/admission implementation: HMAC-SHA256 ticket with TTL.
type Admission struct {
	secret []byte
	ttl    time.Duration
}

func NewAdmission(cfg AdmissionConfig) *Admission {
	return &Admission{
		secret: append([]byte(nil), cfg.Secret...),
		ttl:    cfg.TTL,
	}
}

func (a *Admission) TTL() time.Duration {
	return a.ttl
}

func (a *Admission) Issue(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, nickName string, hostID domain.HostID) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := domain.ValidateNickName(nickName); err != nil {
		return nil, err
	}
	if !roomID.IsValid() || !peerID.IsValid() {
		return nil, fmt.Errorf("admission: %w", ErrInvalidCredentials)
	}
	now := time.Now()
	return a.sign(roomID, peerID, hostID, now, now.Add(a.ttl), nickName), nil
}

// IssueReconnect — reconnect token (kind=2, P20): credential for reviving a
// peer under the same peerID. No nickName (the nick is taken from the
// reservation slot) and no expiry — the server counts the grace window from
// the slot's lastSeen.
func (a *Admission) IssueReconnect(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, hostID domain.HostID) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !roomID.IsValid() || !peerID.IsValid() {
		return nil, fmt.Errorf("admission: %w", ErrInvalidCredentials)
	}
	buf := make([]byte, tokenSizeReconnect)
	buf[0] = tokenKindReconnect
	binary.BigEndian.PutUint64(buf[1:9], uint64(roomID))
	binary.BigEndian.PutUint64(buf[9:17], uint64(peerID))
	binary.BigEndian.PutUint16(buf[hostIDOffsetReconnect:hostIDOffsetReconnect+2], uint16(hostID))

	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(buf[:fixedPayloadSizeReconnect])
	copy(buf[fixedPayloadSizeReconnect:], mac.Sum(nil))
	return buf, nil
}

func (a *Admission) Validate(ctx context.Context, token []byte) (domain.Claims, error) {
	if err := ctx.Err(); err != nil {
		return domain.Claims{}, err
	}
	claims, err := a.verify(token)
	if err != nil {
		return domain.Claims{}, err
	}
	// Only the join-ticket carries expiry; the reconnect token lives as long
	// as the peerID lives — grace is computed by the server from the slot's
	// lastSeen (P20, Q8).
	if claims.Kind == domain.ClaimsKindJoin && time.Now().After(claims.ExpiresAt) {
		return domain.Claims{}, ErrExpiredToken
	}
	return claims, nil
}

func (a *Admission) sign(roomID domain.RoomID, peerID domain.PeerID, hostID domain.HostID, issuedAt, expiresAt time.Time, nickName string) []byte {
	nickLen := len(nickName)
	payloadSize := fixedPayloadSizeJoin + nickLen
	buf := make([]byte, payloadSize+macSize)
	buf[0] = tokenKindJoin
	binary.BigEndian.PutUint64(buf[1:9], uint64(roomID))
	binary.BigEndian.PutUint64(buf[9:17], uint64(peerID))
	binary.BigEndian.PutUint64(buf[17:25], uint64(issuedAt.Unix()))
	binary.BigEndian.PutUint64(buf[25:33], uint64(expiresAt.Unix()))
	binary.BigEndian.PutUint16(buf[hostIDOffset:hostIDOffset+2], uint16(hostID))
	buf[nickLenOffset] = byte(nickLen)
	copy(buf[nickOffset:nickOffset+nickLen], nickName)

	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(buf[:payloadSize])
	copy(buf[payloadSize:], mac.Sum(nil))
	return buf
}

// verify dispatches on the kind byte (the first byte of the credential):
// verifyJoin parses a join-ticket, verifyReconnect — a reconnect token,
// any other kind — ErrInvalidToken. Kind dispatch happens before the length
// check: a reconnect token is shorter than minTokenSizeJoin.
func (a *Admission) verify(token []byte) (domain.Claims, error) {
	if len(token) == 0 {
		return domain.Claims{}, ErrInvalidToken
	}
	switch token[0] {
	case tokenKindJoin:
		return a.verifyJoin(token)
	case tokenKindReconnect:
		return a.verifyReconnect(token)
	default:
		return domain.Claims{}, ErrInvalidToken
	}
}

// verifyJoin parses a join-ticket (kind=1) at fixed offsets and verifies
// the HMAC: kind | roomID | peerID | issuedAt | expiresAt | hostID |
// nickLen | nick | MAC.
func (a *Admission) verifyJoin(token []byte) (domain.Claims, error) {
	if len(token) < minTokenSizeJoin {
		return domain.Claims{}, ErrInvalidToken
	}

	nickLen := int(token[nickLenOffset])
	if nickLen == 0 || nickLen > domain.MaxNickNameLen {
		return domain.Claims{}, ErrInvalidToken
	}
	payloadSize := fixedPayloadSizeJoin + nickLen
	if len(token) != payloadSize+macSize {
		return domain.Claims{}, ErrInvalidToken
	}

	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(token[:payloadSize])
	expected := mac.Sum(nil)
	if !hmac.Equal(token[payloadSize:], expected) {
		return domain.Claims{}, ErrInvalidToken
	}

	roomID, err := domain.RoomIDFromUint64(binary.BigEndian.Uint64(token[1:9]))
	if err != nil {
		return domain.Claims{}, ErrInvalidToken
	}
	peerID, err := domain.PeerIDFromUint64(binary.BigEndian.Uint64(token[9:17]))
	if err != nil {
		return domain.Claims{}, ErrInvalidToken
	}
	hostID := domain.HostID(binary.BigEndian.Uint16(token[hostIDOffset : hostIDOffset+2]))
	issuedAt := time.Unix(int64(binary.BigEndian.Uint64(token[17:25])), 0)
	expiresAt := time.Unix(int64(binary.BigEndian.Uint64(token[25:33])), 0)
	nickName := string(token[nickOffset : nickOffset+nickLen])
	if err := domain.ValidateNickName(nickName); err != nil {
		return domain.Claims{}, ErrInvalidToken
	}

	return domain.Claims{
		Kind:      domain.ClaimsKindJoin,
		RoomID:    roomID,
		PeerID:    peerID,
		NickName:  nickName,
		HostID:    hostID,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}, nil
}

// verifyReconnect parses a reconnect token (kind=2, P20): fixed layout
// kind | roomID u64 | peerID u64 | hostID u16 | MAC. Claims.Kind =
// ClaimsKindReconnect — the admit facade resume flow.
func (a *Admission) verifyReconnect(token []byte) (domain.Claims, error) {
	if len(token) != tokenSizeReconnect {
		return domain.Claims{}, ErrInvalidToken
	}

	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(token[:fixedPayloadSizeReconnect])
	if !hmac.Equal(token[fixedPayloadSizeReconnect:], mac.Sum(nil)) {
		return domain.Claims{}, ErrInvalidToken
	}

	roomID, err := domain.RoomIDFromUint64(binary.BigEndian.Uint64(token[1:9]))
	if err != nil {
		return domain.Claims{}, ErrInvalidToken
	}
	peerID, err := domain.PeerIDFromUint64(binary.BigEndian.Uint64(token[9:17]))
	if err != nil {
		return domain.Claims{}, ErrInvalidToken
	}

	return domain.Claims{
		Kind:   domain.ClaimsKindReconnect,
		RoomID: roomID,
		PeerID: peerID,
		HostID: domain.HostID(binary.BigEndian.Uint16(token[hostIDOffsetReconnect : hostIDOffsetReconnect+2])),
	}, nil
}

var _ portadmission.Admission = (*Admission)(nil)
