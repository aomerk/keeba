package cli

import "testing"

func TestValidRepoFormat(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"aomerk/keeba", true},
		{"openai/codex-rs", true},
		{"a/b", true},
		{"with-dash/name.with.dot", true},
		{"keeba", false},
		{"too/many/slashes", false},
		{"/leading", false},
		{"trailing/", false},
		{"", false},
		{"-leadinghyphen/x", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := validRepoFormat(tt.in); got != tt.want {
				t.Fatalf("validRepoFormat(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
