package domain

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

const MaxNickNameLen = 64

var ErrInvalidNickName = errors.New("invalid nick name")

func ValidateNickName(nickName string) error {
	if nickName == "" {
		return fmt.Errorf("%w: empty", ErrInvalidNickName)
	}
	if !utf8.ValidString(nickName) {
		return fmt.Errorf("%w: invalid UTF-8", ErrInvalidNickName)
	}
	if len(nickName) > MaxNickNameLen {
		return fmt.Errorf("%w: length %d exceeds %d", ErrInvalidNickName, len(nickName), MaxNickNameLen)
	}
	for _, r := range nickName {
		if r < 0x20 {
			return fmt.Errorf("%w: control character", ErrInvalidNickName)
		}
	}
	return nil
}
