package encoding

import (
	"strings"
	"testing"
)

func TestStructuralCardName(t *testing.T) {
	if got := (StructuralCard{}).Name(); got != "structural-card" {
		t.Errorf("Name() = %q, want %q", got, "structural-card")
	}
}

func TestStructuralCardPython(t *testing.T) {
	body := `def add_user(name: str, role: str = "viewer") -> User:
    """Insert a new user row into the directory.

    Validates the role is one of the known set.
    """
    if role not in ALLOWED_ROLES:
        raise ValueError("bad role")
    record = User(name=name, role=role)
    db.insert(record)
    return record
`
	got, err := StructuralCard{}.Encode(body)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if !strings.Contains(got, "def add_user") {
		t.Errorf("expected signature in card, got %q", got)
	}
	if !strings.Contains(got, "Insert a new user row into the directory") {
		t.Errorf("expected docstring lede in card, got %q", got)
	}
	if !strings.Contains(got, "User") {
		t.Errorf("expected key identifier in card, got %q", got)
	}
}

func TestStructuralCardGo(t *testing.T) {
	body := `func RegisterHandler(name string, fn HandlerFunc) error {
	if name == "" {
		return ErrEmptyName
	}
	registry[name] = fn
	return nil
}`
	got, err := StructuralCard{}.Encode(body)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if !strings.Contains(got, "func RegisterHandler") {
		t.Errorf("expected signature in card, got %q", got)
	}
	if !strings.Contains(got, "registry") {
		t.Errorf("expected body identifier 'registry' in card, got %q", got)
	}
}

func TestStructuralCardCompresses(t *testing.T) {
	body := `def long_function(items: list[Item]) -> dict:
    """Aggregate items into a histogram."""
    result = {}
    for item in items:
        bucket = item.category
        if bucket not in result:
            result[bucket] = 0
        result[bucket] += item.weight
    for bucket in result:
        if result[bucket] > THRESHOLD:
            log.warning("bucket %s exceeded threshold", bucket)
    return result
`
	got, err := StructuralCard{}.Encode(body)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if len(got) >= len(body) {
		t.Errorf("expected compression: %d -> %d (no reduction)", len(body), len(got))
	}
}

func TestStructuralCardNoSignature(t *testing.T) {
	body := "just some loose text without any def or class"
	got, err := StructuralCard{}.Encode(body)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if got == "" {
		t.Errorf("expected a non-empty card even without signature")
	}
}

func TestStructuralCardDocLineTruncates(t *testing.T) {
	long := strings.Repeat("very long doc ", 30)
	body := `def f():
    """` + long + `"""
    pass
`
	got, err := (StructuralCard{DocChars: 50}).Encode(body)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if strings.Count(got, "very long doc") > 4 {
		t.Errorf("expected DocChars=50 to limit doc lede, got %q", got)
	}
}

func TestStructuralCardTopIdentsBound(t *testing.T) {
	body := `def f():
    a = 1; b = 2; c = 3; d = 4; e = 5; ff = 6; gg = 7; hh = 8
`
	got, err := (StructuralCard{TopIdents: 3}).Encode(body)
	if err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	// signature plus at most 3 top idents — overall short
	if len(strings.Fields(got)) > 12 {
		t.Errorf("expected TopIdents=3 to keep card short, got %q", got)
	}
}
