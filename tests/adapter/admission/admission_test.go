package admission_test

import (
	"context"
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
	token, err := a.Issue(ctx, domain.RoomID(1), domain.PeerID(2), nickName, domain.HostIDUnbound)
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
	if claims.HostID != domain.HostIDUnbound {
		t.Fatalf("claims.HostID = %d, want HostIDUnbound", claims.HostID)
	}
}

// P18: hostID в join-ticket'е - roundtrip; HostIDUnbound - без привязки.
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

	token, err := a.Issue(ctx, domain.RoomID(1), domain.PeerID(1), "player", domain.HostIDUnbound)
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

	token, err := a.Issue(ctx, domain.RoomID(math.MaxUint32), domain.PeerID(math.MaxUint32), "player", domain.HostIDUnbound)
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
	_, err := a.Issue(context.Background(), domain.RoomID(1), domain.PeerID(1), "", domain.HostIDUnbound)
	if !errors.Is(err, domain.ErrInvalidNickName) {
		t.Fatalf("err = %v, want ErrInvalidNickName", err)
	}
}

func TestIssueInvalidCredentials(t *testing.T) {
	t.Parallel()

	a := admission.NewAdmission(testAdmissionConfig("secret", time.Hour))
	_, err := a.Issue(context.Background(), domain.RoomIDInvalid, domain.PeerID(1), "player", domain.HostIDUnbound)
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

// P20: reconnect-токен (kind=2) — roundtrip, Kind=reconnect, без expiry и nick.
func TestIssueReconnectValidateRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	a := admission.NewAdmission(testAdmissionConfig("test-secret", time.Hour))

	token, err := a.IssueReconnect(ctx, domain.RoomID(1), domain.PeerID(2), domain.HostID(3))
	if err != nil {
		t.Fatalf("IssueReconnect: %v", err)
	}
	if len(token) != 51 {
		t.Fatalf("token len = %d, want 51", len(token))
	}
	if token[0] != 2 {
		t.Fatalf("token kind = %d, want 2", token[0])
	}

	claims, err := a.Validate(ctx, token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.Kind != domain.ClaimsKindReconnect {
		t.Fatalf("claims.Kind = %v, want reconnect", claims.Kind)
	}
	if claims.RoomID != 1 || claims.PeerID != 2 || claims.HostID != 3 {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.NickName != "" {
		t.Fatalf("claims.NickName = %q, want empty (nick из слота)", claims.NickName)
	}
}

// P20: join-ticket помечается Kind=join — фасад разводит потоки по Kind.
func TestIssueJoinClaimsKind(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	a := admission.NewAdmission(testAdmissionConfig("test-secret", time.Hour))

	token, err := a.Issue(ctx, domain.RoomID(1), domain.PeerID(2), "player", domain.HostIDUnbound)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	claims, err := a.Validate(ctx, token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.Kind != domain.ClaimsKindJoin {
		t.Fatalf("claims.Kind = %v, want join", claims.Kind)
	}
}

// P20: reconnect-токен не несёт expiry — живёт, пока жив peerID (grace серверное).
func TestReconnectTokenNoExpiry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	a := admission.NewAdmission(testAdmissionConfig("test-secret", time.Millisecond))

	token, err := a.IssueReconnect(ctx, domain.RoomID(1), domain.PeerID(2), domain.HostIDUnbound)
	if err != nil {
		t.Fatalf("IssueReconnect: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := a.Validate(ctx, token); err != nil {
		t.Fatalf("Validate reconnect после TTL admission: %v", err)
	}
}

func TestIssueReconnectInvalidCredentials(t *testing.T) {
	t.Parallel()

	a := admission.NewAdmission(testAdmissionConfig("test-secret", time.Hour))
	_, err := a.IssueReconnect(context.Background(), domain.RoomIDInvalid, domain.PeerID(1), domain.HostIDUnbound)
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestValidateReconnectTampered(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	a := admission.NewAdmission(testAdmissionConfig("test-secret", time.Hour))
	other := admission.NewAdmission(testAdmissionConfig("other-secret-32-bytes-padded-here!!", time.Hour))

	token, err := a.IssueReconnect(ctx, domain.RoomID(1), domain.PeerID(2), domain.HostIDUnbound)
	if err != nil {
		t.Fatalf("IssueReconnect: %v", err)
	}
	// Чужой секрет → ErrInvalidToken.
	if _, err := other.Validate(ctx, token); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("foreign secret err = %v, want ErrInvalidToken", err)
	}
	// Укороченный/битый токен → ErrInvalidToken.
	if _, err := a.Validate(ctx, token[:40]); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("truncated err = %v, want ErrInvalidToken", err)
	}
	token[10] ^= 0xFF
	if _, err := a.Validate(ctx, token); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("tampered err = %v, want ErrInvalidToken", err)
	}
}
