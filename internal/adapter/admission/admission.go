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
	tokenVersion     = 2
	fixedPayloadSize = 1 + 8 + 8 + 8 + 8 + 1
	macSize          = sha256.Size
	minTokenSize     = fixedPayloadSize + macSize
)

// Admission — минимальная реализация port/admission: HMAC-SHA256 ticket с TTL.
// password пустой — проверка пароля при Issue отключена.
type Admission struct {
	secret   []byte
	ttl      time.Duration
	password string
}

func NewAdmission(cfg AdmissionConfig) *Admission {
	return &Admission{
		secret:   append([]byte(nil), cfg.Secret...),
		ttl:      cfg.TTL,
		password: cfg.Password,
	}
}

func (a *Admission) TTL() time.Duration {
	return a.ttl
}

func (a *Admission) Issue(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, nickName string, password string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := domain.ValidateNickName(nickName); err != nil {
		return nil, err
	}
	if !roomID.IsValid() || !peerID.IsValid() {
		return nil, fmt.Errorf("admission: %w", ErrInvalidCredentials)
	}
	if a.password != "" && password != a.password {
		return nil, ErrInvalidCredentials
	}
	now := time.Now()
	return a.sign(roomID, peerID, now, now.Add(a.ttl), nickName), nil
}

func (a *Admission) Validate(ctx context.Context, token []byte) (domain.Claims, error) {
	if err := ctx.Err(); err != nil {
		return domain.Claims{}, err
	}
	roomID, peerID, nickName, issuedAt, expiresAt, err := a.verify(token)
	if err != nil {
		return domain.Claims{}, err
	}
	if time.Now().After(expiresAt) {
		return domain.Claims{}, ErrExpiredToken
	}
	return domain.Claims{
		RoomID:    roomID,
		PeerID:    peerID,
		NickName:  nickName,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}, nil
}

func (a *Admission) sign(roomID domain.RoomID, peerID domain.PeerID, issuedAt, expiresAt time.Time, nickName string) []byte {
	nickLen := len(nickName)
	payloadSize := fixedPayloadSize + nickLen
	buf := make([]byte, payloadSize+macSize)
	buf[0] = tokenVersion
	binary.BigEndian.PutUint64(buf[1:9], uint64(roomID))
	binary.BigEndian.PutUint64(buf[9:17], uint64(peerID))
	binary.BigEndian.PutUint64(buf[17:25], uint64(issuedAt.Unix()))
	binary.BigEndian.PutUint64(buf[25:33], uint64(expiresAt.Unix()))
	buf[33] = byte(nickLen)
	copy(buf[34:34+nickLen], nickName)

	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(buf[:payloadSize])
	copy(buf[payloadSize:], mac.Sum(nil))
	return buf
}

func (a *Admission) verify(token []byte) (domain.RoomID, domain.PeerID, string, time.Time, time.Time, error) {
	if len(token) < minTokenSize || token[0] != tokenVersion {
		return 0, 0, "", time.Time{}, time.Time{}, ErrInvalidToken
	}

	nickLen := int(token[33])
	if nickLen == 0 || nickLen > domain.MaxNickNameLen {
		return 0, 0, "", time.Time{}, time.Time{}, ErrInvalidToken
	}

	payloadSize := fixedPayloadSize + nickLen
	if len(token) != payloadSize+macSize {
		return 0, 0, "", time.Time{}, time.Time{}, ErrInvalidToken
	}

	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(token[:payloadSize])
	expected := mac.Sum(nil)
	if !hmac.Equal(token[payloadSize:], expected) {
		return 0, 0, "", time.Time{}, time.Time{}, ErrInvalidToken
	}

	roomID, err := domain.RoomIDFromUint64(binary.BigEndian.Uint64(token[1:9]))
	if err != nil {
		return 0, 0, "", time.Time{}, time.Time{}, ErrInvalidToken
	}
	peerID, err := domain.PeerIDFromUint64(binary.BigEndian.Uint64(token[9:17]))
	if err != nil {
		return 0, 0, "", time.Time{}, time.Time{}, ErrInvalidToken
	}
	issuedAt := time.Unix(int64(binary.BigEndian.Uint64(token[17:25])), 0)
	expiresAt := time.Unix(int64(binary.BigEndian.Uint64(token[25:33])), 0)
	nickName := string(token[34 : 34+nickLen])
	if err := domain.ValidateNickName(nickName); err != nil {
		return 0, 0, "", time.Time{}, time.Time{}, ErrInvalidToken
	}

	return roomID, peerID, nickName, issuedAt, expiresAt, nil
}

var _ portadmission.Admission = (*Admission)(nil)
