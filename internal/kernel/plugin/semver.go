package plugin

// Minimal SemVer + constraint parser (no third-party deps).
//
// Mirrors tools/tabula-distro/src/tabula_distro/semver.py — same syntax,
// same semantics. Versions are strict MAJOR.MINOR.PATCH ints with an
// optional -pre.release suffix (compared lexically). Constraints are
// comma-separated AND clauses, e.g. ">=0.8.0,<1.0.0". A bare version
// is treated as exact match. Recognized operators: <, <=, =, ==, >, >=.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var versionRE = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)

// Version is a parsed SemVer triple plus optional pre-release tag.
type Version struct {
	Major int
	Minor int
	Patch int
	Pre   string
}

// String renders the canonical "X.Y.Z[-pre]" form.
func (v Version) String() string {
	base := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		return base + "-" + v.Pre
	}
	return base
}

// Compare returns -1, 0, +1 for v < o, ==, >.
func (v Version) Compare(o Version) int {
	if v.Major != o.Major {
		return cmpInt(v.Major, o.Major)
	}
	if v.Minor != o.Minor {
		return cmpInt(v.Minor, o.Minor)
	}
	if v.Patch != o.Patch {
		return cmpInt(v.Patch, o.Patch)
	}
	// Match the Python implementation: pre is compared lexically. This is
	// imprecise per the SemVer spec but adequate for early development; a
	// version without pre is "less" than one with (lex order: "" < "alpha").
	// We don't ship pre-releases yet.
	return strings.Compare(v.Pre, o.Pre)
}

// ParseVersion parses "X.Y.Z" or "vX.Y.Z" (optionally with "-pre" tag).
func ParseVersion(text string) (Version, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "v")
	m := versionRE.FindStringSubmatch(text)
	if m == nil {
		return Version{}, fmt.Errorf("not a valid version: %q", text)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return Version{Major: major, Minor: minor, Patch: patch, Pre: m[4]}, nil
}

// Constraint is a parsed comma-AND of clauses, e.g. ">=0.9.0,<1.0.0".
type Constraint struct {
	Raw     string
	Clauses []constraintClause
}

type constraintClause struct {
	op      string
	version Version
}

// Matches reports whether v satisfies the constraint.
func (c Constraint) Matches(v Version) bool {
	for _, cl := range c.Clauses {
		if !cl.matches(v) {
			return false
		}
	}
	return true
}

func (c constraintClause) matches(v Version) bool {
	cmp := v.Compare(c.version)
	switch c.op {
	case "==", "=":
		return cmp == 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	}
	return false
}

// ParseConstraint parses a comma-separated AND of clauses. Empty input is
// an error.
func ParseConstraint(text string) (Constraint, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Constraint{}, fmt.Errorf("empty constraint")
	}
	var clauses []constraintClause
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		cl, err := parseClause(part)
		if err != nil {
			return Constraint{}, err
		}
		clauses = append(clauses, cl)
	}
	if len(clauses) == 0 {
		return Constraint{}, fmt.Errorf("empty constraint")
	}
	return Constraint{Raw: text, Clauses: clauses}, nil
}

// Order matters: longer ops first to avoid mis-prefixing.
var clauseOps = []string{"<=", ">=", "==", "<", ">", "="}

func parseClause(text string) (constraintClause, error) {
	for _, op := range clauseOps {
		if strings.HasPrefix(text, op) {
			v, err := ParseVersion(text[len(op):])
			if err != nil {
				return constraintClause{}, err
			}
			canonical := op
			if op == "=" {
				canonical = "=="
			}
			return constraintClause{op: canonical, version: v}, nil
		}
	}
	// Bare version → exact match (mirrors Python parser).
	v, err := ParseVersion(text)
	if err != nil {
		return constraintClause{}, err
	}
	return constraintClause{op: "==", version: v}, nil
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
