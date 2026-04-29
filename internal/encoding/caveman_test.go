package encoding

import "testing"

func TestMDCavemanName(t *testing.T) {
	if got := (MDCaveman{}).Name(); got != "md-caveman" {
		t.Errorf("Name() = %q, want %q", got, "md-caveman")
	}
}

func TestMDCavemanEncode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "drops articles",
			in:   "The quick brown fox jumps over a lazy dog.",
			want: "quick brown fox jumps over lazy dog.",
		},
		{
			name: "drops copula",
			in:   "It is a test that was easy.",
			want: "It test that easy.",
		},
		{
			name: "rewrites compound phrases",
			in:   "We need this in order to ship.",
			want: "We need this to ship.",
		},
		{
			name: "rewrites cannot",
			in:   "We cannot do this.",
			want: "We cant this.",
		},
		{
			name: "drops hedges",
			in:   "This is just really actually basically simple.",
			want: "This simple.",
		},
		{
			name: "case insensitive multi-word",
			in:   "DUE TO THE FACT THAT it works.",
			want: "because it works.",
		},
		{
			name: "preserves punctuation tightness",
			in:   "the cat , the dog . the fish !",
			want: "cat, dog. fish!",
		},
		{
			name: "preserves identifier-looking tokens",
			in:   "Call func_name with foo_bar.",
			want: "Call func_name with foo_bar.",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
		{
			name: "whitespace only",
			in:   "   \n\t  ",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MDCaveman{}.Encode(tc.in)
			if err != nil {
				t.Fatalf("Encode(%q) err = %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("Encode(%q):\n  got  = %q\n  want = %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMDCavemanCompresses(t *testing.T) {
	in := "This is a really very long sentence that has a lot of articles and copula words " +
		"and will be compressed by the caveman encoder so that the output is much shorter."
	got, err := MDCaveman{}.Encode(in)
	if err != nil {
		t.Fatalf("Encode err: %v", err)
	}
	if len(got) >= len(in) {
		t.Errorf("expected compression: %d -> %d (no reduction)", len(in), len(got))
	}
}
