package plugin

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in    string
		ok    bool
		major int
		minor int
		patch int
		pre   string
	}{
		{"0.1.0", true, 0, 1, 0, ""},
		{"v1.2.3", true, 1, 2, 3, ""},
		{"1.2.3-alpha.1", true, 1, 2, 3, "alpha.1"},
		{"1.2", false, 0, 0, 0, ""},
		{"abc", false, 0, 0, 0, ""},
		{"", false, 0, 0, 0, ""},
	}
	for _, tt := range tests {
		v, err := ParseVersion(tt.in)
		if tt.ok && err != nil {
			t.Errorf("ParseVersion(%q) err=%v, want ok", tt.in, err)
			continue
		}
		if !tt.ok && err == nil {
			t.Errorf("ParseVersion(%q) ok, want err", tt.in)
			continue
		}
		if !tt.ok {
			continue
		}
		if v.Major != tt.major || v.Minor != tt.minor || v.Patch != tt.patch || v.Pre != tt.pre {
			t.Errorf("ParseVersion(%q) = %#v", tt.in, v)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.9.9", 1},
		{"0.9.0", "0.10.0", -1},
		{"1.0.0", "1.0.0-alpha", -1}, // "" < "alpha" lexically
	}
	for _, c := range cases {
		va, _ := ParseVersion(c.a)
		vb, _ := ParseVersion(c.b)
		if got := va.Compare(vb); got != c.want {
			t.Errorf("Compare(%s,%s)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseConstraintAndMatches(t *testing.T) {
	cs, err := ParseConstraint(">=0.9.0,<1.0.0")
	if err != nil {
		t.Fatalf("ParseConstraint err: %v", err)
	}
	cases := []struct {
		v    string
		want bool
	}{
		{"0.9.0", true},
		{"0.9.5", true},
		{"0.8.9", false},
		{"1.0.0", false},
		{"1.0.1", false},
	}
	for _, c := range cases {
		v, err := ParseVersion(c.v)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", c.v, err)
		}
		if got := cs.Matches(v); got != c.want {
			t.Errorf("Matches(%s)=%v want %v", c.v, got, c.want)
		}
	}
}

func TestParseConstraintBareIsExact(t *testing.T) {
	cs, err := ParseConstraint("0.9.0")
	if err != nil {
		t.Fatalf("ParseConstraint err: %v", err)
	}
	v090, _ := ParseVersion("0.9.0")
	v091, _ := ParseVersion("0.9.1")
	if !cs.Matches(v090) {
		t.Errorf("bare constraint should match exact")
	}
	if cs.Matches(v091) {
		t.Errorf("bare constraint should not match neighbor")
	}
}

func TestParseConstraintRejectsEmpty(t *testing.T) {
	if _, err := ParseConstraint(""); err == nil {
		t.Errorf("expected error for empty constraint")
	}
	if _, err := ParseConstraint(", ,"); err == nil {
		t.Errorf("expected error for blank constraint")
	}
}
