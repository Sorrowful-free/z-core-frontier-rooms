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

const (
	tokenVersion = 1
	payloadSize  = 1 + 8 + 8 + 8 + 8
	macSize      = sha256.Size
	tokenSize    = payloadSize + macSize
)

// Admission — минимальная реализация port/admission: HMAC-SHA256 ticket с TTL.
// password пустой — проверка пароля при Issue отключена.
type Admission struct {
	secret   []byte
	ttl      time.Duration
	password string
}

func NewAdmission(secret []byte, ttl time.Duration, password string) *Admission {
	return &Admission{
		secret:   append([]byte(nil), secret...),
		ttl:      ttl,
		password: password,
	}
}

func (a *Admission) TTL() time.Duration {
	return a.ttl
}

func (a *Admission) Issue(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, password string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !roomID.IsValid() || !peerID.IsValid() {
		return nil, fmt.Errorf("admission: %w", ErrInvalidCredentials)
	}
	if a.password != "" && password != a.password {
		return nil, ErrInvalidCredentials
	}
	now := time.Now()
	return a.sign(roomID, peerID, now, now.Add(a.ttl)), nil
}

func (a *Admission) Validate(ctx context.Context, token []byte) (domain.Claims, error) {
	if err := ctx.Err(); err != nil {
		return domain.Claims{}, err
	}
	roomID, peerID, issuedAt, expiresAt, err := a.verify(token)
	if err != nil {
		return domain.Claims{}, err
	}
	if time.Now().After(expiresAt) {
		return domain.Claims{}, ErrExpiredToken
	}
	return domain.Claims{
		RoomID:    roomID,
		PeerID:    peerID,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}, nil
}

func (a *Admission) sign(roomID domain.RoomID, peerID domain.PeerID, issuedAt, expiresAt time.Time) []byte {
	buf := make([]byte, tokenSize)
	buf[0] = tokenVersion
	binary.BigEndian.PutUint64(buf[1:9], uint64(roomID))
	binary.BigEndian.PutUint64(buf[9:17], uint64(peerID))
	binary.BigEndian.PutUint64(buf[17:25], uint64(issuedAt.Unix()))
	binary.BigEndian.PutUint64(buf[25:33], uint64(expiresAt.Unix()))

	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(buf[:payloadSize])
	copy(buf[payloadSize:], mac.Sum(nil))
	return buf
}

func (a *Admission) verify(token []byte) (domain.RoomID, domain.PeerID, time.Time, time.Time, error) {
	if len(token) != tokenSize || token[0] != tokenVersion {
		return 0, 0, time.Time{}, time.Time{}, ErrInvalidToken
	}

	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(token[:payloadSize])
	expected := mac.Sum(nil)
	if !hmac.Equal(token[payloadSize:], expected) {
		return 0, 0, time.Time{}, time.Time{}, ErrInvalidToken
	}

	roomID := domain.RoomID(binary.BigEndian.Uint64(token[1:9]))
	peerID := domain.PeerID(binary.BigEndian.Uint64(token[9:17]))
	issuedAt := time.Unix(int64(binary.BigEndian.Uint64(token[17:25])), 0)
	expiresAt := time.Unix(int64(binary.BigEndian.Uint64(token[25:33])), 0)

	if !roomID.IsValid() || !peerID.IsValid() {
		return 0, 0, time.Time{}, time.Time{}, ErrInvalidToken
	}
	return roomID, peerID, issuedAt, expiresAt, nil
}

var _ portadmission.Admission = (*Admission)(nil)
