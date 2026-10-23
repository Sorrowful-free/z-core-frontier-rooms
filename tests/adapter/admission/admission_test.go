package admission_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func testAdmissionConfig(secret string, ttl time.Duration) admission.AdmissionConfig {
	return admission.AdmissionConfig{
		Secret: []byte(secret),
		TTL:    ttl,
	}
}

func TestIssueValidateRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	a := admission.NewAdmission(testAdmissionConfig("test-secret", time.Hour))

	const nickName = "player"
	token, err := a.Issue(ctx, domain.RoomID(1), domain.PeerID(2), nickName, domain.HostIDAny)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := a.Validate(ctx, token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.RoomID != 1 || claims.PeerID != 2 || claims.NickName != nickName {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.HostID != domain.HostIDAny {
		t.Fatalf("claims.HostID = %d, want HostIDAny", claims.HostID)
	}
}

// P18: hostID в ticket v3 - roundtrip; HostIDAny - без привязки.
func TestIssueValidateHostID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	a := admission.NewAdmission(testAdmissionConfig("test-secret", time.Hour))

	token, err := a.Issue(ctx, domain.RoomID(1), domain.PeerID(2), "player", domain.HostID(7))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	claims, err := a.Validate(ctx, token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.HostID != domain.HostID(7) {
		t.Fatalf("claims.HostID = %d, want 7", claims.HostID)
	}
}

// P18 backward compat: ticket v2 (без hostID) валиден, claims.HostID = HostIDAny.
func TestValidateTicketV2WithoutHostID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	a := admission.NewAdmission(testAdmissionConfig("test-secret", time.Hour))

	token := issueTicketV2(t, domain.RoomID(1), domain.PeerID(2), "player")
	claims, err := a.Validate(ctx, token)
	if err != nil {
		t.Fatalf("Validate v2: %v", err)
	}
	if claims.RoomID != 1 || claims.PeerID != 2 || claims.NickName != "player" {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.HostID != domain.HostIDAny {
		t.Fatalf("claims.HostID = %d, want HostIDAny", claims.HostID)
	}
}

// issueTicketV2 строит legacy ticket v2 (version | roomID | peerID | issuedAt |
// expiresAt | nickLen | nick | HMAC) тем же секретом - эмуляция тикета,
// выписанного до P18.
func issueTicketV2(t *testing.T, roomID domain.RoomID, peerID domain.PeerID, nickName string) []byte {
	t.Helper()

	secret := []byte("test-secret")
	nickLen := len(nickName)
	payloadSize := 1 + 8 + 8 + 8 + 8 + 1 + nickLen
	buf := make([]byte, payloadSize+32)
	buf[0] = 2
	binary.BigEndian.PutUint64(buf[1:9], uint64(roomID))
	binary.BigEndian.PutUint64(buf[9:17], uint64(peerID))
	now := time.Now()
	binary.BigEndian.PutUint64(buf[17:25], uint64(now.Unix()))
	binary.BigEndian.PutUint64(buf[25:33], uint64(now.Add(time.Hour).Unix()))
	buf[33] = byte(nickLen)
	copy(buf[34:34+nickLen], nickName)

	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(buf[:payloadSize])
	copy(buf[payloadSize:], mac.Sum(nil))
	return buf
}

func TestValidateInvalidToken(t *testing.T) {
	t.Parallel()

	a := admission.NewAdmission(testAdmissionConfig("secret", time.Hour))
	_, err := a.Validate(context.Background(), []byte{1, 2, 3})
	if !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestValidateExpiredToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	a := admission.NewAdmission(testAdmissionConfig("secret", time.Millisecond))

	token, err := a.Issue(ctx, domain.RoomID(1), domain.PeerID(1), "player", domain.HostIDAny)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	time.Sleep(2 * time.Millisecond)

	_, err = a.Validate(ctx, token)
	if !errors.Is(err, domain.ErrExpiredToken) {
		t.Fatalf("err = %v, want ErrExpiredToken", err)
	}
}

func TestIssueValidateMaxUint32IDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	a := admission.NewAdmission(testAdmissionConfig("test-secret", time.Hour))

	token, err := a.Issue(ctx, domain.RoomID(math.MaxUint32), domain.PeerID(math.MaxUint32), "player", domain.HostIDAny)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := a.Validate(ctx, token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.RoomID != domain.RoomID(math.MaxUint32) || claims.PeerID != domain.PeerID(math.MaxUint32) {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestIssueInvalidNickName(t *testing.T) {
	t.Parallel()

	a := admission.NewAdmission(testAdmissionConfig("secret", time.Hour))
	_, err := a.Issue(context.Background(), domain.RoomID(1), domain.PeerID(1), "", domain.HostIDAny)
	if !errors.Is(err, domain.ErrInvalidNickName) {
		t.Fatalf("err = %v, want ErrInvalidNickName", err)
	}
}

func TestIssueInvalidCredentials(t *testing.T) {
	t.Parallel()

	a := admission.NewAdmission(testAdmissionConfig("secret", time.Hour))
	_, err := a.Issue(context.Background(), domain.RoomIDInvalid, domain.PeerID(1), "player", domain.HostIDAny)
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}
