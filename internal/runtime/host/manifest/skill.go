package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

const skillTargetPrefix = "skill:"

// Skill is the runtime daemon's normalized view of one SKILL.md frontmatter manifest.
// Skill target ids are namespaced as skill:<name> so they cannot collide with plugins
// in kernel/runtime maps that still key by target id.
type Skill struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Tools       []SkillTool      `json:"tools,omitempty"`
	RootDir     string           `json:"-"`
	WorkerMode  wire.WorkerMode  `json:"worker_mode"`
	HarnessKind wire.HarnessKind `json:"harness_kind"`
}

// SkillTool is the normalized tool metadata extracted from SKILL.md frontmatter.
type SkillTool struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Schema      json.RawMessage  `json:"schema,omitempty"`
	Required    []string         `json:"required,omitempty"`
	Exec        string           `json:"exec"`
	HarnessKind wire.HarnessKind `json:"harness_kind"`
}

type skillFrontmatter struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Tools       []skillToolRaw `json:"tools"`
}

type skillToolRaw struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Params      map[string]any `json:"params"`
	Required    []string       `json:"required"`
	Exec        string         `json:"exec"`
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
		Tools:       make([]SkillTool, 0, len(parsed.Tools)),
		RootDir:     filepath.Dir(abs),
		WorkerMode:  wire.WorkerModeCold,
		HarnessKind: wire.HarnessKindUnknown,
	}
	for i, tool := range parsed.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return Skill{}, fmt.Errorf("tools[%d].name is required", i)
		}
		execText := strings.TrimSpace(tool.Exec)
		if execText == "" {
			return Skill{}, fmt.Errorf("tools[%d].exec is required", i)
		}
		kind := detectHarnessKind(execText)
		if kind == wire.HarnessKindUnknown {
			slog.Warn("skip skill tool with unsupported harness kind", "skill", skill.Name, "tool", name, "exec", execText, "path", abs)
			continue
		}
		schema, err := toolSchema(tool.Params, tool.Required)
		if err != nil {
			return Skill{}, fmt.Errorf("tools[%d].schema: %w", i, err)
		}
		st := SkillTool{
			Name:        name,
			Description: strings.TrimSpace(tool.Description),
			Schema:      schema,
			Required:    normalizeStrings(tool.Required),
			Exec:        execText,
			HarnessKind: kind,
		}
		skill.Tools = append(skill.Tools, st)
		if skill.HarnessKind == wire.HarnessKindUnknown {
			skill.HarnessKind = kind
		} else if skill.HarnessKind != kind {
			skill.HarnessKind = wire.HarnessKindUnknown
		}
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
	for i, tool := range s.Tools {
		if tool.Name == "" {
			return fmt.Errorf("tools[%d].name is required", i)
		}
		if strings.TrimSpace(tool.Exec) == "" {
			return fmt.Errorf("tools[%d].exec is required", i)
		}
	}
	return nil
}

func (s Skill) TargetID() string {
	return skillTargetPrefix + s.Name
}

func (s Skill) RawJSON() json.RawMessage {
	data, _ := json.Marshal(s)
	return data
}

func (s Skill) Tool(name string) (SkillTool, bool) {
	for _, tool := range s.Tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return SkillTool{}, false
}

func (s Skill) Capability() wire.Capability {
	tools := make([]wire.ToolSpec, 0, len(s.Tools))
	for _, tool := range s.Tools {
		tools = append(tools, wire.ToolSpec{Name: tool.Name, Description: tool.Description, Schema: tool.Schema})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return wire.Capability{
		Target:      wire.Target{Kind: wire.TargetKindSkill, ID: s.TargetID()},
		Tools:       tools,
		Revision:    1,
		State:       wire.CapabilityStateManifestLoaded,
		Source:      wire.CapabilitySourceManifest,
		WorkerMode:  wire.WorkerModeCold,
		HarnessKind: s.HarnessKind,
	}
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
	rawTools, _ := parsed["tools"].([]any)
	tools := make([]skillToolRaw, 0, len(rawTools))
	for i, raw := range rawTools {
		m, ok := raw.(map[string]any)
		if !ok {
			return skillFrontmatter{}, fmt.Errorf("tools[%d] must be a mapping", i)
		}
		tool := skillToolRaw{
			Name:        stringValue(m["name"]),
			Description: stringValue(m["description"]),
			Exec:        stringValue(m["exec"]),
			Required:    stringSliceValue(m["required"]),
		}
		if params, ok := m["params"].(map[string]any); ok {
			tool.Params = params
		} else if m["params"] == nil {
			tool.Params = map[string]any{}
		} else {
			return skillFrontmatter{}, fmt.Errorf("tools[%d].params must be a mapping", i)
		}
		tools = append(tools, tool)
	}
	return skillFrontmatter{Name: name, Description: description, Tools: tools}, nil
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

func toolSchema(params map[string]any, required []string) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": params,
		"required":   normalizeStrings(required),
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func stringSliceValue(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			result = append(result, strings.TrimSpace(s))
		}
	}
	return normalizeStrings(result)
}

func normalizeStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func detectHarnessKind(execText string) wire.HarnessKind {
	fields := strings.Fields(strings.TrimSpace(execText))
	if len(fields) == 0 {
		return wire.HarnessKindUnknown
	}
	first := strings.ToLower(filepath.Base(fields[0]))
	switch first {
	case "python", "python3", "python3.11", "python3.12", "python3.13":
		return wire.HarnessKindPython
	case "bash", "sh":
		return wire.HarnessKindBash
	case "node", "nodejs":
		return wire.HarnessKindNode
	default:
		if strings.HasPrefix(first, "node") {
			return wire.HarnessKindNode
		}
		return wire.HarnessKindUnknown
	}
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
