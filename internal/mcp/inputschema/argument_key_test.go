package inputschema

import (
	"strings"
	"testing"
)

func TestCheckArgumentKey(t *testing.T) {
	for _, tc := range []struct {
		key, wantErr string
	}{
		{"region", ""},
		{"x-retry_2", ""},
		{strings.Repeat("k", MaxArgumentKeyLength), ""},
		{"", "empty"},
		{strings.Repeat("k", MaxArgumentKeyLength+1), "too long"},
		{"bad key", "invalid character"},
		{"key@host", "invalid character"},
		{"caf\xe9", "invalid character"},
	} {
		err := CheckArgumentKey(tc.key)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("CheckArgumentKey(%.20q) = %v, want nil", tc.key, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("CheckArgumentKey(%.20q) = %v, want error containing %q", tc.key, err, tc.wantErr)
		}
	}
}
