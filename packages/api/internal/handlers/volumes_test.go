package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVolumeNameValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		isValid bool
	}{
		// Valid names
		{name: "simple lowercase", input: "myvolume", isValid: true},
		{name: "with hyphen", input: "my-volume", isValid: true},
		{name: "single char", input: "a", isValid: true},
		{name: "letter and number", input: "a1", isValid: true},
		{name: "complex valid", input: "volume-123", isValid: true},
		{name: "max length (63 chars)", input: "a123456789012345678901234567890123456789012345678901234567890ab", isValid: true},
		{name: "starts with letter ends with number", input: "vol1", isValid: true},
		{name: "multiple hyphens", input: "my-test-volume", isValid: true},

		// Invalid names
		{name: "uppercase letter", input: "MyVolume", isValid: false},
		{name: "starts with hyphen", input: "-volume", isValid: false},
		{name: "ends with hyphen", input: "volume-", isValid: false},
		{name: "starts with number", input: "123volume", isValid: false},
		{name: "empty string", input: "", isValid: false},
		{name: "too long (64 chars)", input: "a1234567890123456789012345678901234567890123456789012345678901234", isValid: false},
		{name: "contains underscore", input: "my_volume", isValid: false},
		{name: "contains space", input: "my volume", isValid: false},
		{name: "contains period", input: "my.volume", isValid: false},
		{name: "only number", input: "1", isValid: false},
		{name: "only hyphen", input: "-", isValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := volumeNamePattern.MatchString(tt.input)
			assert.Equal(t, tt.isValid, result, "volumeNamePattern.MatchString(%q)", tt.input)
		})
	}
}
