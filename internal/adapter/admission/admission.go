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

// Ticket v2 (legacy): version | roomID | peerID | issuedAt | expiresAt | nickLen | nick | MAC.
// Ticket v3 (P18): те же поля + hostID u16 после expiresAt; hostID=0 - без привязки.
// Ticket v4 (P20): version | roomID | peerID | hostID u16 | MAC — reconnect-
// токен фиксированной длины (51 байт), без nickName и timestamps.
// Verify принимает все версии: у v2 claims.HostID = domain.HostIDAny, у v4
// claims.Kind = ClaimsKindReconnect и нет expiry (grace — серверное).
const (
	tokenVersionV2     = 2
	tokenVersionV3     = 3
	tokenVersionV4     = 4
	fixedPayloadSizeV2 = 1 + 8 + 8 + 8 + 8 + 1
	fixedPayloadSize   = 1 + 8 + 8 + 8 + 8 + 2 + 1
	fixedPayloadSizeV4 = 1 + 8 + 8 + 2
	tokenSizeV4        = fixedPayloadSizeV4 + sha256.Size
	macSize            = sha256.Size
	minTokenSizeV2     = fixedPayloadSizeV2 + macSize
	hostIDOffsetV3     = 33
	nickLenOffsetV3    = 35
	nickOffsetV3       = 36
	nickLenOffsetV2    = 33
	nickOffsetV2       = 34
	hostIDOffsetV4     = 17
)

// Admission — минимальная реализация port/admission: HMAC-SHA256 ticket с TTL.
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

// IssueReconnect — reconnect-токен v4 (P20): credential воскрешения peer'а
// под тем же peerID. Без nickName (ник берётся из слота reservation) и без
// expiry — grace окно отсчитывает сервер от lastSeen слота.
func (a *Admission) IssueReconnect(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, hostID domain.HostID) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !roomID.IsValid() || !peerID.IsValid() {
		return nil, fmt.Errorf("admission: %w", ErrInvalidCredentials)
	}
	buf := make([]byte, tokenSizeV4)
	buf[0] = tokenVersionV4
	binary.BigEndian.PutUint64(buf[1:9], uint64(roomID))
	binary.BigEndian.PutUint64(buf[9:17], uint64(peerID))
	binary.BigEndian.PutUint16(buf[hostIDOffsetV4:hostIDOffsetV4+2], uint16(hostID))

	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(buf[:fixedPayloadSizeV4])
	copy(buf[fixedPayloadSizeV4:], mac.Sum(nil))
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
	// Expiry несёт только join-ticket; reconnect-токен v4 живёт, пока жив
	// peerID — grace считается сервером от lastSeen слота (P20, Q8).
	if claims.Kind == domain.ClaimsKindJoin && time.Now().After(claims.ExpiresAt) {
		return domain.Claims{}, ErrExpiredToken
	}
	return claims, nil
}

func (a *Admission) sign(roomID domain.RoomID, peerID domain.PeerID, hostID domain.HostID, issuedAt, expiresAt time.Time, nickName string) []byte {
	nickLen := len(nickName)
	payloadSize := fixedPayloadSize + nickLen
	buf := make([]byte, payloadSize+macSize)
	buf[0] = tokenVersionV3
	binary.BigEndian.PutUint64(buf[1:9], uint64(roomID))
	binary.BigEndian.PutUint64(buf[9:17], uint64(peerID))
	binary.BigEndian.PutUint64(buf[17:25], uint64(issuedAt.Unix()))
	binary.BigEndian.PutUint64(buf[25:33], uint64(expiresAt.Unix()))
	binary.BigEndian.PutUint16(buf[33:35], uint16(hostID))
	buf[nickLenOffsetV3] = byte(nickLen)
	copy(buf[nickOffsetV3:nickOffsetV3+nickLen], nickName)

	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(buf[:payloadSize])
	copy(buf[payloadSize:], mac.Sum(nil))
	return buf
}

// verify разбирает ticket v2/v3/v4 и проверяет HMAC. Общие поля (roomID,
// peerID, timestamps) лежат по одним смещениям в v2/v3; hostID
// существует только в v3+, у v2 claims.HostID = domain.HostIDAny.
// Version-диспатч идёт до проверки длины: v4 короче minTokenSizeV2.
func (a *Admission) verify(token []byte) (domain.Claims, error) {
	if len(token) == 0 {
		return domain.Claims{}, ErrInvalidToken
	}
	if token[0] == tokenVersionV4 {
		return a.verifyReconnect(token)
	}
	if len(token) < minTokenSizeV2 {
		return domain.Claims{}, ErrInvalidToken
	}

	version := token[0]
	var fixedSize, hostIDOffset, nickLenOffset, nickOffset int
	switch version {
	case tokenVersionV2:
		fixedSize = fixedPayloadSizeV2
		nickLenOffset = nickLenOffsetV2
		nickOffset = nickOffsetV2
	case tokenVersionV3:
		fixedSize = fixedPayloadSize
		hostIDOffset = hostIDOffsetV3
		nickLenOffset = nickLenOffsetV3
		nickOffset = nickOffsetV3
	default:
		return domain.Claims{}, ErrInvalidToken
	}
	if len(token) < fixedSize+macSize {
		return domain.Claims{}, ErrInvalidToken
	}

	nickLen := int(token[nickLenOffset])
	if nickLen == 0 || nickLen > domain.MaxNickNameLen {
		return domain.Claims{}, ErrInvalidToken
	}
	payloadSize := fixedSize + nickLen
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
	hostID := domain.HostIDAny
	if version == tokenVersionV3 {
		hostID = domain.HostID(binary.BigEndian.Uint16(token[hostIDOffset : hostIDOffset+2]))
	}
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

// verifyReconnect разбирает ticket v4 (P20): фиксированный layout
// ver | roomID u64 | peerID u64 | hostID u16 | MAC. Claims.Kind =
// ClaimsKindReconnect — resume-поток admit-фасада.
func (a *Admission) verifyReconnect(token []byte) (domain.Claims, error) {
	if len(token) != tokenSizeV4 {
		return domain.Claims{}, ErrInvalidToken
	}

	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(token[:fixedPayloadSizeV4])
	if !hmac.Equal(token[fixedPayloadSizeV4:], mac.Sum(nil)) {
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
		HostID: domain.HostID(binary.BigEndian.Uint16(token[hostIDOffsetV4 : hostIDOffsetV4+2])),
	}, nil
}

var _ portadmission.Admission = (*Admission)(nil)
