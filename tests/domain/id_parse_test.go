package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func TestParseRoomIDString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    domain.RoomID
		wantErr error
	}{
		{name: "one", raw: "1", want: domain.RoomID(1)},
		{name: "max uint32", raw: "4294967295", want: domain.RoomID(math.MaxUint32)},
		{name: "zero", raw: "0", wantErr: domain.ErrInvalidRoomID},
		{name: "negative", raw: "-1", wantErr: domain.ErrInvalidRoomID},
		{name: "overflow uint32", raw: "4294967296", wantErr: domain.ErrInvalidRoomID},
		{name: "empty", raw: "", wantErr: domain.ErrInvalidRoomID},
		{name: "not a number", raw: "abc", wantErr: domain.ErrInvalidRoomID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseRoomIDString(tt.raw)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRoomIDString: %v", err)
			}
			if got != tt.want {
				t.Fatalf("id = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRoomIDFromInt64MatchesStringParser(t *testing.T) {
	t.Parallel()

	const max = int64(math.MaxUint32)
	cases := []int64{1, 42, max, 0, -1, max + 1, math.MaxInt64}

	for _, v := range cases {
		fromInt, errInt := domain.RoomIDFromInt64(v)
		fromStr, errStr := domain.ParseRoomIDString(int64ToDecimal(v))

		if (errInt == nil) != (errStr == nil) {
			t.Fatalf("v=%d: int err=%v str err=%v", v, errInt, errStr)
		}
		if errInt != nil {
			if !errors.Is(errInt, domain.ErrInvalidRoomID) || !errors.Is(errStr, domain.ErrInvalidRoomID) {
				t.Fatalf("v=%d: int err=%v str err=%v", v, errInt, errStr)
			}
			continue
		}
		if fromInt != fromStr {
			t.Fatalf("v=%d: int=%v str=%v", v, fromInt, fromStr)
		}
	}
}

func TestRoomIDFromUint64MatchesInt64Parser(t *testing.T) {
	t.Parallel()

	cases := []uint64{1, math.MaxUint32, 0, math.MaxUint32 + 1, math.MaxUint64}

	for _, v := range cases {
		fromU64, errU64 := domain.RoomIDFromUint64(v)
		fromI64, errI64 := domain.RoomIDFromInt64(int64(v))

		if (errU64 == nil) != (errI64 == nil) {
			t.Fatalf("v=%d: u64 err=%v i64 err=%v", v, errU64, errI64)
		}
		if errU64 != nil {
			if !errors.Is(errU64, domain.ErrInvalidRoomID) {
				t.Fatalf("v=%d: u64 err=%v", v, errU64)
			}
			continue
		}
		if fromU64 != fromI64 {
			t.Fatalf("v=%d: u64=%v i64=%v", v, fromU64, fromI64)
		}
	}
}

func TestParsePeerIDString(t *testing.T) {
	t.Parallel()

	got, err := domain.ParsePeerIDString("4294967295")
	if err != nil {
		t.Fatalf("ParsePeerIDString: %v", err)
	}
	if got != domain.PeerID(math.MaxUint32) {
		t.Fatalf("id = %v, want max uint32", got)
	}

	_, err = domain.ParsePeerIDString("4294967296")
	if !errors.Is(err, domain.ErrInvalidPeerID) {
		t.Fatalf("err = %v, want ErrInvalidPeerID", err)
	}
}

func int64ToDecimal(v int64) string {
	if v < 0 {
		return "-" + int64ToDecimal(-v)
	}
	if v == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for v > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	return string(digits[i:])
}
