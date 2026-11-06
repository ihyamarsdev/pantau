package web

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxGroupNameLength = 64

// cleanGroupName trims a playbook/workspace group label. "" means ungrouped. Groups are free text like Host Group;
// they only label and sort, so the one rule is a short single-line string.
func cleanGroupName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxGroupNameLength {
		return "", errors.New("group name must not exceed 64 characters")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", errors.New("group name must not contain control characters")
		}
	}
	return s, nil
}
