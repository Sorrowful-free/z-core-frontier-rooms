package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func TestValidateNickName(t *testing.T) {
	t.Parallel()

	longNick := strings.Repeat("a", domain.MaxNickNameLen+1)

	cases := []struct {
		name    string
		nick    string
		wantErr error
	}{
		{name: "valid", nick: "player_1"},
		{name: "max length", nick: strings.Repeat("b", domain.MaxNickNameLen)},
		{name: "empty", nick: "", wantErr: domain.ErrInvalidNickName},
		{name: "too long", nick: longNick, wantErr: domain.ErrInvalidNickName},
		{name: "control char", nick: "bad\x01nick", wantErr: domain.ErrInvalidNickName},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := domain.ValidateNickName(tc.nick)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateNickName(%q) = %v, want nil", tc.nick, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateNickName(%q) = %v, want %v", tc.nick, err, tc.wantErr)
			}
		})
	}
}
