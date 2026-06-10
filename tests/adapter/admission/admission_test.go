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

func TestIssueValidateRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	a := admission.NewAdmission([]byte("test-secret"), time.Hour, "")

	const nickName = "player"
	token, err := a.Issue(ctx, domain.RoomID(1), domain.PeerID(2), nickName, "")
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
}

func TestValidateInvalidToken(t *testing.T) {
	t.Parallel()

	a := admission.NewAdmission([]byte("secret"), time.Hour, "")
	_, err := a.Validate(context.Background(), []byte{1, 2, 3})
	if !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestValidateExpiredToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	a := admission.NewAdmission([]byte("secret"), time.Millisecond, "")

	token, err := a.Issue(ctx, domain.RoomID(1), domain.PeerID(1), "player", "")
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
	a := admission.NewAdmission([]byte("test-secret"), time.Hour, "")

	token, err := a.Issue(ctx, domain.RoomID(math.MaxUint32), domain.PeerID(math.MaxUint32), "player", "")
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

	a := admission.NewAdmission([]byte("secret"), time.Hour, "")
	_, err := a.Issue(context.Background(), domain.RoomID(1), domain.PeerID(1), "", "")
	if !errors.Is(err, domain.ErrInvalidNickName) {
		t.Fatalf("err = %v, want ErrInvalidNickName", err)
	}
}

func TestIssueInvalidCredentials(t *testing.T) {
	t.Parallel()

	a := admission.NewAdmission([]byte("secret"), time.Hour, "room-pass")
	_, err := a.Issue(context.Background(), domain.RoomIDInvalid, domain.PeerID(1), "player", "")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}
