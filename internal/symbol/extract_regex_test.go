package symbol

import "testing"

func runExtract(t *testing.T, lang, file, src string) []Symbol {
	t.Helper()
	rx := regexExtractorsByLang[lang]
	if rx == nil {
		t.Fatalf("no extractor for lang %q", lang)
	}
	syms, err := regexExtractor{lang: lang, rx: rx}.Extract(file, []byte(src))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	return syms
}

func TestRegexExtractor_Python(t *testing.T) {
	syms := runExtract(t, "py", "foo.py", `class User:
    def __init__(self, name):
        self.name = name

    def greet(self):
        return "hi " + self.name

async def fetch(url):
    pass
`)
	want := map[string]string{
		"User":     "class",
		"__init__": "function",
		"greet":    "function",
		"fetch":    "function",
	}
	got := map[string]string{}
	for _, s := range syms {
		got[s.Name] = s.Kind
	}
	for n, k := range want {
		if got[n] != k {
			t.Errorf("missing %s (kind %q), got %v", n, k, got)
		}
	}
}

func TestRegexExtractor_TypeScript(t *testing.T) {
	syms := runExtract(t, "ts", "foo.ts", `export interface User {
  name: string;
}

export type ID = string;

export class Server {
  start() { return; }
}

export function greet(name: string): void {}

export const handler = async (req) => {};
`)
	want := map[string]string{
		"User":    "interface",
		"ID":      "type",
		"Server":  "class",
		"greet":   "function",
		"handler": "function",
	}
	got := map[string]string{}
	for _, s := range syms {
		got[s.Name] = s.Kind
	}
	for n, k := range want {
		if got[n] != k {
			t.Errorf("missing %s (kind %q), got %v", n, k, got)
		}
	}
}

func TestRegexExtractor_Rust(t *testing.T) {
	syms := runExtract(t, "rs", "foo.rs", `pub struct Server {
    addr: String,
}

pub trait Handler {
    fn handle(&self);
}

impl Handler for Server {
    fn handle(&self) {}
}

pub async fn run(addr: &str) {}
`)
	want := []string{"Server", "Handler", "run"}
	got := map[string]bool{}
	for _, s := range syms {
		got[s.Name] = true
	}
	for _, n := range want {
		if !got[n] {
			t.Errorf("missing %s, got %v", n, got)
		}
	}
}

func TestRegexExtractor_JavaScript(t *testing.T) {
	syms := runExtract(t, "js", "foo.js", `function greet(name) {
  return "hi " + name;
}

class Counter {
  inc() {}
}

const handler = async (req) => req;
`)
	want := []string{"greet", "Counter", "handler"}
	got := map[string]bool{}
	for _, s := range syms {
		got[s.Name] = true
	}
	for _, n := range want {
		if !got[n] {
			t.Errorf("missing %s, got %v", n, got)
		}
	}
}

func TestEstimateEndLine_StopsAtBlankBlankOrSibling(t *testing.T) {
	body := `def first():
    return 1


def second():
    pass
`
	lines := splitLines(body)
	// "first()" body is lines 1-2 ("def first():" + "    return 1");
	// blank-blank then sibling at line 5. estimateEndLine returns the
	// 1-based last-meaningful line of the symbol body, i.e. 2.
	end := estimateEndLine(lines, 1)
	if end != 2 {
		t.Errorf("end of first() = %d, want 2 (last-meaningful body line), body=%q", end, body)
	}
}

func TestEstimateEndLine_SiblingStopsBeforeNextDef(t *testing.T) {
	body := `def first():
    return 1
def second():
    pass
`
	lines := splitLines(body)
	end := estimateEndLine(lines, 1)
	if end != 2 {
		t.Errorf("end of first() (sibling stop) = %d, want 2", end)
	}
}

func splitLines(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func TestRegexExtractor_RecordsLanguage(t *testing.T) {
	syms := runExtract(t, "py", "foo.py", "def f(): pass\n")
	if len(syms) == 0 {
		t.Fatal("no symbols")
	}
	if syms[0].Language != "py" {
		t.Errorf("Language = %q, want py", syms[0].Language)
	}
}
