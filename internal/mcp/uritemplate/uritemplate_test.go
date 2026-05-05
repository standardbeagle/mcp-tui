package uritemplate

import (
	"reflect"
	"testing"
)

func TestIsTemplate(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		want bool
	}{
		{"empty", "", false},
		{"plain URI", "file:///etc/passwd", false},
		{"simple template", "file:///{path}", true},
		{"query template", "users://{userId}/profile", true},
		{"path expansion", "things://{/segments*}", true},
		{"unclosed brace is not a template", "weird://{userId/profile", false},
		{"only close brace is not a template", "weird://}/profile", false},
		{"empty expression still counts (degenerate)", "x://{}", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTemplate(tc.uri); got != tc.want {
				t.Fatalf("IsTemplate(%q) = %v, want %v", tc.uri, got, tc.want)
			}
		})
	}
}

func TestVariables(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		want []string
	}{
		{"non-template returns nil", "file:///etc/passwd", nil},
		{"single var", "users://{userId}", []string{"userId"}},
		{"multiple expressions", "users://{userId}/posts/{postId}", []string{"userId", "postId"}},
		{"multiple vars in one expression", "search{?q,limit}", []string{"q", "limit"}},
		{"with operator +", "{+base}/path", []string{"base"}},
		{"with operator #", "page{#fragment}", []string{"fragment"}},
		{"with operator .", "host{.domain}", []string{"domain"}},
		{"with operator /", "{/segments*}", []string{"segments"}},
		{"with operator ;", "{;name}", []string{"name"}},
		{"with operator ?", "{?q}", []string{"q"}},
		{"with operator &", "{&debug}", []string{"debug"}},
		{"explode modifier stripped", "{userId*}", []string{"userId"}},
		{"prefix modifier stripped", "{userId:3}", []string{"userId"}},
		{"duplicates de-duplicated", "{?q}{&q}", []string{"q"}},
		{"empty expression dropped", "{}", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Variables(tc.uri)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Variables(%q) = %v, want %v", tc.uri, got, tc.want)
			}
		})
	}
}

func TestVariableAtCursor(t *testing.T) {
	cases := []struct {
		name       string
		uri        string
		cursor     int
		wantName   string
		wantPrefix string
		wantOk     bool
	}{
		{
			name:   "cursor inside simple var",
			uri:    "users://{userId}/profile",
			cursor: 12, // between `{u` and `serId}`
			// The user has only typed up to the bracket; we report the var
			// name based on what is between `{` and the cursor.
			wantName:   "use",
			wantPrefix: "",
			wantOk:     true,
		},
		{
			name:       "cursor right after open brace",
			uri:        "users://{userId}/profile",
			cursor:     9,
			wantName:   "",
			wantPrefix: "",
			wantOk:     false,
		},
		{
			name:       "cursor at end of name",
			uri:        "users://{userId}",
			cursor:     15, // just before `}` — full name typed
			wantName:   "userId",
			wantPrefix: "",
			wantOk:     true,
		},
		{
			name:       "cursor outside braces returns false",
			uri:        "users://{userId}/profile",
			cursor:     20,
			wantName:   "",
			wantPrefix: "",
			wantOk:     false,
		},
		{
			name:       "cursor in plain URI",
			uri:        "file:///etc/passwd",
			cursor:     5,
			wantName:   "",
			wantPrefix: "",
			wantOk:     false,
		},
		{
			name:       "operator stripped",
			uri:        "search{?query}",
			cursor:     13, // last char before `}`
			wantName:   "query",
			wantPrefix: "",
			wantOk:     true,
		},
		{
			name:       "comma list takes last typed name",
			uri:        "search{?q,limit}",
			cursor:     15, // between `t` and `}`
			wantName:   "limit",
			wantPrefix: "",
			wantOk:     true,
		},
		{
			name:       "modifier produces prefix",
			uri:        "things://{userId:abc}",
			cursor:     20, // just before `}`
			wantName:   "userId",
			wantPrefix: "abc",
			wantOk:     true,
		},
		{
			name:       "negative cursor",
			uri:        "x://{a}",
			cursor:     -1,
			wantName:   "",
			wantPrefix: "",
			wantOk:     false,
		},
		{
			name:       "cursor past end",
			uri:        "x://{a}",
			cursor:     100,
			wantName:   "",
			wantPrefix: "",
			wantOk:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotPrefix, gotOk := VariableAtCursor(tc.uri, tc.cursor)
			if gotName != tc.wantName || gotPrefix != tc.wantPrefix || gotOk != tc.wantOk {
				t.Fatalf("VariableAtCursor(%q, %d) = (%q, %q, %v), want (%q, %q, %v)",
					tc.uri, tc.cursor, gotName, gotPrefix, gotOk, tc.wantName, tc.wantPrefix, tc.wantOk)
			}
		})
	}
}

func TestExpand(t *testing.T) {
	cases := []struct {
		name   string
		uri    string
		values map[string]string
		want   string
	}{
		{"non-template unchanged", "file:///etc/passwd", map[string]string{"x": "y"}, "file:///etc/passwd"},
		{"empty values unchanged", "users://{userId}", nil, "users://{userId}"},
		{"single substitution", "users://{userId}", map[string]string{"userId": "42"}, "users://42"},
		{"multiple substitutions", "users://{userId}/posts/{postId}", map[string]string{"userId": "42", "postId": "7"}, "users://42/posts/7"},
		{"missing value left intact", "users://{userId}/posts/{postId}", map[string]string{"userId": "42"}, "users://42/posts/{postId}"},
		{"empty value treated as missing", "users://{userId}/posts/{postId}", map[string]string{"userId": "42", "postId": ""}, "users://42/posts/{postId}"},
		{"complex operator left intact", "{+base}/path", map[string]string{"base": "https://x"}, "{+base}/path"},
		{"explode modifier left intact", "{/segs*}", map[string]string{"segs": "a/b"}, "{/segs*}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Expand(tc.uri, tc.values)
			if got != tc.want {
				t.Fatalf("Expand(%q, %v) = %q, want %q", tc.uri, tc.values, got, tc.want)
			}
		})
	}
}
