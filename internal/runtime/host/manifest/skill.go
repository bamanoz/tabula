package manifest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const skillTargetPrefix = "skill:"

// Skill is the runtime daemon's normalized view of one SKILL.md frontmatter manifest.
// Skill target ids are namespaced as skill:<name> so they cannot collide with plugins
// in kernel/runtime maps that still key by target id.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	RootDir     string `json:"-"`
}

type skillFrontmatter struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// LoadSkill reads and validates one SKILL.md file.
func LoadSkill(path string) (Skill, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Skill{}, fmt.Errorf("skill manifest path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Skill{}, fmt.Errorf("resolve skill manifest %s: %w", path, err)
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return Skill{}, fmt.Errorf("read skill manifest %s: %w", abs, err)
	}
	frontmatter, err := extractFrontmatter(string(body))
	if err != nil {
		return Skill{}, fmt.Errorf("skill manifest %s: %w", abs, err)
	}
	parsed, err := parseSkillFrontmatter(frontmatter)
	if err != nil {
		return Skill{}, fmt.Errorf("skill manifest %s: %w", abs, err)
	}
	skill := Skill{
		Name:        strings.TrimSpace(parsed.Name),
		Description: strings.TrimSpace(parsed.Description),
		RootDir:     filepath.Dir(abs),
	}
	if err := skill.Validate(); err != nil {
		return Skill{}, fmt.Errorf("skill manifest %s: %w", abs, err)
	}
	return skill, nil
}

func (s Skill) Validate() error {
	if !pluginIDPattern.MatchString(s.Name) {
		return fmt.Errorf("invalid skill name %q", s.Name)
	}
	return nil
}

func (s Skill) TargetID() string {
	return skillTargetPrefix + s.Name
}

func extractFrontmatter(text string) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") && text != "---" {
		return "", fmt.Errorf("missing YAML frontmatter")
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return "", fmt.Errorf("unterminated YAML frontmatter")
	}
	return strings.TrimSpace(text[4 : 4+end]), nil
}

func parseSkillFrontmatter(frontmatter string) (skillFrontmatter, error) {
	lines := splitManifestLines(frontmatter)
	parsed, next, err := parseMapping(lines, 0, 0)
	if err != nil {
		return skillFrontmatter{}, err
	}
	if next != len(lines) {
		return skillFrontmatter{}, fmt.Errorf("unexpected trailing frontmatter content")
	}
	name, _ := parsed["name"].(string)
	description, _ := parsed["description"].(string)
	return skillFrontmatter{Name: name, Description: description}, nil
}

type manifestLine struct {
	indent int
	text   string
}

func splitManifestLines(frontmatter string) []manifestLine {
	raw := strings.Split(strings.ReplaceAll(frontmatter, "\r\n", "\n"), "\n")
	lines := make([]manifestLine, 0, len(raw))
	for _, line := range raw {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		lines = append(lines, manifestLine{indent: indent, text: strings.TrimSpace(line)})
	}
	return lines
}

func parseMapping(lines []manifestLine, start, indent int) (map[string]any, int, error) {
	result := map[string]any{}
	i := start
	for i < len(lines) {
		line := lines[i]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			return nil, i, fmt.Errorf("unexpected indentation near %q", line.text)
		}
		if strings.HasPrefix(line.text, "- ") {
			return nil, i, fmt.Errorf("unexpected list item %q", line.text)
		}
		key, value, ok := strings.Cut(line.text, ":")
		if !ok {
			return nil, i, fmt.Errorf("expected key: value, got %q", line.text)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		i++
		if value == "" {
			if i >= len(lines) || lines[i].indent <= indent {
				result[key] = map[string]any{}
				continue
			}
			if strings.HasPrefix(lines[i].text, "- ") {
				list, next, err := parseList(lines, i, lines[i].indent)
				if err != nil {
					return nil, i, err
				}
				result[key] = list
				i = next
				continue
			}
			m, next, err := parseMapping(lines, i, lines[i].indent)
			if err != nil {
				return nil, i, err
			}
			result[key] = m
			i = next
			continue
		}
		if value == ">" || value == "|" {
			block, next, err := parseBlockScalar(lines, i, indent, value)
			if err != nil {
				return nil, i, err
			}
			result[key] = block
			i = next
			continue
		}
		result[key] = parseScalar(value)
	}
	return result, i, nil
}

func parseList(lines []manifestLine, start, indent int) ([]any, int, error) {
	items := []any{}
	i := start
	for i < len(lines) {
		line := lines[i]
		if line.indent < indent {
			break
		}
		if line.indent != indent || !strings.HasPrefix(line.text, "- ") {
			break
		}
		itemText := strings.TrimSpace(strings.TrimPrefix(line.text, "- "))
		i++
		item := map[string]any{}
		if itemText != "" {
			key, value, ok := strings.Cut(itemText, ":")
			if !ok {
				items = append(items, parseScalar(itemText))
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if value == ">" || value == "|" {
				block, next, err := parseBlockScalar(lines, i, indent, value)
				if err != nil {
					return nil, i, err
				}
				item[key] = block
				i = next
			} else if value == "" {
				item[key] = map[string]any{}
			} else {
				item[key] = parseScalar(value)
			}
		}
		if i < len(lines) && lines[i].indent > indent {
			nested, next, err := parseMapping(lines, i, lines[i].indent)
			if err != nil {
				return nil, i, err
			}
			for k, v := range nested {
				item[k] = v
			}
			i = next
		}
		items = append(items, item)
	}
	return items, i, nil
}

func parseBlockScalar(lines []manifestLine, start, parentIndent int, style string) (string, int, error) {
	if start >= len(lines) || lines[start].indent <= parentIndent {
		return "", start, nil
	}
	indent := lines[start].indent
	parts := []string{}
	i := start
	for i < len(lines) {
		line := lines[i]
		if line.indent < indent {
			break
		}
		if line.indent == parentIndent {
			break
		}
		parts = append(parts, strings.TrimSpace(line.text))
		i++
	}
	if style == "|" {
		return strings.Join(parts, "\n"), i, nil
	}
	return strings.Join(parts, " "), i, nil
}

func parseScalar(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}
	if n, err := strconv.Atoi(value); err == nil {
		return n
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		inner := strings.TrimSpace(value[1 : len(value)-1])
		if inner == "" {
			return []any{}
		}
		parts := splitTopLevel(inner)
		out := make([]any, 0, len(parts))
		for _, part := range parts {
			out = append(out, parseScalar(part))
		}
		return out
	}
	if strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}") {
		inner := strings.TrimSpace(value[1 : len(value)-1])
		out := map[string]any{}
		if inner == "" {
			return out
		}
		for _, part := range splitTopLevel(inner) {
			key, raw, ok := strings.Cut(part, ":")
			if !ok {
				continue
			}
			out[strings.TrimSpace(key)] = parseScalar(raw)
		}
		return out
	}
	return strings.Trim(value, `"'`)
}

func splitTopLevel(value string) []string {
	parts := []string{}
	current := strings.Builder{}
	depth := 0
	quote := rune(0)
	for _, ch := range value {
		if quote != 0 {
			current.WriteRune(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			current.WriteRune(ch)
		case '[', '{':
			depth++
			current.WriteRune(ch)
		case ']', '}':
			depth--
			current.WriteRune(ch)
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(current.String()))
				current.Reset()
				continue
			}
			current.WriteRune(ch)
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}
	return parts
}

func skillManifestPaths(root string) ([]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat skill search dir %s: %w", root, err)
	}
	if !info.IsDir() {
		if filepath.Base(root) == "SKILL.md" {
			return []string{root}, nil
		}
		return nil, fmt.Errorf("skill search path %s is not a directory or SKILL.md", root)
	}
	var paths []string
	if err := walkSkillManifests(root, map[string]struct{}{}, &paths); err != nil {
		return nil, fmt.Errorf("walk skill search dir %s: %w", root, err)
	}
	sort.Strings(paths)
	return paths, nil
}

func walkSkillManifests(root string, seen map[string]struct{}, paths *[]string) error {
	realRoot := filepath.Clean(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		realRoot = filepath.Clean(resolved)
	}
	if _, ok := seen[realRoot]; ok {
		return nil
	}
	seen[realRoot] = struct{}{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if entry.Name() == "SKILL.md" {
			*paths = append(*paths, path)
			continue
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && path != root {
				continue
			}
			if err := walkSkillManifests(path, seen, paths); err != nil {
				return err
			}
			continue
		}
		if entry.Type()&fs.ModeSymlink == 0 {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		if err := walkSkillManifests(path, seen, paths); err != nil {
			return err
		}
	}
	return nil
}
