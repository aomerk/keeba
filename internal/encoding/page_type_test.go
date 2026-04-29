package encoding

import "testing"

func TestDetectPageType_Function_FromCitedFile(t *testing.T) {
	cases := []string{
		"src/foo.go", "lib/bar.py", "components/baz.ts", "src/auth.rs", "Main.java",
	}
	for _, f := range cases {
		if got := DetectPageType("just narrative prose", []string{f}); got != PageTypeFunction {
			t.Errorf("DetectPageType(_, [%q]) = %q, want function", f, got)
		}
	}
}

func TestDetectPageType_Function_FromBody(t *testing.T) {
	bodies := []string{
		"def foo(x):\n    return x\n",
		"class Foo:\n    pass\n",
		"func RegisterHandler(name string) error { return nil }",
		"async function getUser(id) { return id }",
		"impl Foo {\n  fn bar(&self) {}\n}",
	}
	for _, b := range bodies {
		if got := DetectPageType(b, nil); got != PageTypeFunction {
			t.Errorf("DetectPageType for code body got %q, want function:\n%s", got, b)
		}
	}
}

func TestDetectPageType_Entity_FromBulletFacts(t *testing.T) {
	body := `# AaveV3

> Lending protocol on Ethereum.

- **chain**: ethereum
- **address**: 0x1234567890abcdef
- **deployed**: 2023-01-15
- **status**: active
- **website**: https://aave.com
`
	if got := DetectPageType(body, nil); got != PageTypeEntity {
		t.Errorf("DetectPageType for entity body got %q, want entity", got)
	}
}

func TestDetectPageType_NarrativeDefault(t *testing.T) {
	bodies := []string{
		"# Overview\n\nThis is a flowing narrative about something. It has paragraphs.\n\nMore prose follows here.",
		"# Decision\n\nWe decided to use BM25 because of the reasons below.\n\nFirst, BM25 is simple. Second, it works.",
	}
	for _, b := range bodies {
		if got := DetectPageType(b, nil); got != PageTypeNarrative {
			t.Errorf("DetectPageType for narrative got %q, want narrative:\n%s", got, b)
		}
	}
}

func TestDetectPageType_NoFalsePositiveOnShortBulletList(t *testing.T) {
	// 3 bullets is below the threshold; should remain narrative.
	body := "Some intro.\n\n- one: a\n- two: b\n- three: c\n\nMore prose."
	if got := DetectPageType(body, nil); got != PageTypeNarrative {
		t.Errorf("3-bullet body should stay narrative, got %q", got)
	}
}

func TestDetectPageType_CitedFileBeatsBody(t *testing.T) {
	// Even if body is narrative, cited code file wins.
	body := "Just prose, nothing code-y."
	if got := DetectPageType(body, []string{"src/foo.py"}); got != PageTypeFunction {
		t.Errorf("cited code file should override body, got %q", got)
	}
}
