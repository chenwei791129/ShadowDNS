package shadowdnscfg

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type envLookup func(string) (string, bool)

type envReference struct {
	Name   string
	Path   string
	Line   int
	Column int
}

type sourcePosition func(int) (int, int)

func expandString(value string, line, column int, lookup envLookup) (string, []string, error) {
	return expandStringAt(value, lookup, func(offset int) (int, int) {
		prefix := value[:offset]
		lastNewline := strings.LastIndexByte(prefix, '\n')
		if lastNewline < 0 {
			return line, column + len([]rune(prefix))
		}
		return line + strings.Count(prefix, "\n"), 1 + len([]rune(prefix[lastNewline+1:]))
	})
}

func expandStringAt(value string, lookup envLookup, position sourcePosition) (string, []string, error) {
	var out strings.Builder
	used := make(map[string]struct{})

	for i := 0; i < len(value); {
		if value[i] != '$' {
			out.WriteByte(value[i])
			i++
			continue
		}
		if i+1 >= len(value) {
			out.WriteByte('$')
			i++
			continue
		}
		switch value[i+1] {
		case '$':
			out.WriteByte('$')
			i += 2
		case '{':
			expressionLine, expressionColumn := position(i)
			end := expressionEnd(value, i+2)
			if end < 0 {
				return "", nil, expressionError(expressionLine, expressionColumn, "unterminated environment expression")
			}
			body := value[i+2 : end]
			name, fallback, hasFallback, err := parseExpression(body)
			if err != nil {
				return "", nil, expressionError(expressionLine, expressionColumn, err.Error())
			}
			envValue, ok := lookup(name)
			if !ok || envValue == "" {
				if !hasFallback {
					return "", nil, expressionError(expressionLine, expressionColumn, fmt.Sprintf("required environment variable %s is unset or empty", name))
				}
				out.WriteString(fallback)
			} else {
				out.WriteString(envValue)
				used[name] = struct{}{}
			}
			i = end + 1
		default:
			out.WriteByte('$')
			i++
		}
	}

	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sort.Strings(names)
	return out.String(), names, nil
}

func expressionEnd(value string, start int) int {
	if operator := strings.Index(value[start:], ":-"); operator >= 0 {
		defaultStart := start + operator + 2
		if nested := strings.Index(value[defaultStart:], "${"); nested == 0 {
			if nestedEnd := strings.IndexByte(value[defaultStart+2:], '}'); nestedEnd >= 0 {
				outerEnd := defaultStart + 2 + nestedEnd + 1
				if outerEnd < len(value) && value[outerEnd] == '}' {
					return outerEnd
				}
			}
		}
	}
	if end := strings.IndexByte(value[start:], '}'); end >= 0 {
		return start + end
	}
	return -1
}

func parseExpression(body string) (name, fallback string, hasFallback bool, err error) {
	operator := strings.Index(body, ":-")
	if operator >= 0 {
		name = body[:operator]
		fallback = body[operator+2:]
		hasFallback = true
	} else {
		name = body
	}
	if name == "" || !isEnvName(name) {
		if strings.ContainsAny(name, "-:?") {
			return "", "", false, fmt.Errorf("unsupported environment expression")
		}
		return "", "", false, fmt.Errorf("invalid environment variable name")
	}
	if !hasFallback && strings.ContainsAny(body, "-:?") {
		return "", "", false, fmt.Errorf("unsupported environment expression for %s", namePrefix(body))
	}
	return name, fallback, hasFallback, nil
}

func isEnvNameStart(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func isEnvNameContinue(c byte) bool {
	return isEnvNameStart(c) || c >= '0' && c <= '9'
}

func isEnvName(name string) bool {
	if name == "" || !isEnvNameStart(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !isEnvNameContinue(name[i]) {
			return false
		}
	}
	return true
}

func namePrefix(body string) string {
	if body == "" || !isEnvNameStart(body[0]) {
		return "expression"
	}
	for i := 1; i < len(body); i++ {
		if !isEnvNameContinue(body[i]) {
			return body[:i]
		}
	}
	return body
}

func expressionError(line, column int, message string) error {
	return fmt.Errorf("line %d, column %d: %s", line, column, message)
}

func expandYAMLValues(root *yaml.Node, lookup envLookup) ([]envReference, error) {
	refs, _, err := expandYAMLValuesFromSource(root, lookup, nil)
	return refs, err
}

func expandYAMLValuesFromSource(root *yaml.Node, lookup envLookup, source []byte) ([]envReference, bool, error) {
	var refs []envReference
	changed := false
	var walk func(*yaml.Node, string) error
	walk = func(node *yaml.Node, path string) error {
		switch node.Kind {
		case yaml.DocumentNode:
			if len(node.Content) > 0 {
				return walk(node.Content[0], path)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				childPath := key.Value
				if path != "" {
					childPath = path + "." + key.Value
				}
				if err := walk(value, childPath); err != nil {
					return err
				}
			}
		case yaml.SequenceNode:
			for i, child := range node.Content {
				if err := walk(child, path+"["+strconv.Itoa(i)+"]"); err != nil {
					return err
				}
			}
		case yaml.ScalarNode:
			if node.Tag != "!!str" {
				return nil
			}
			position := scalarPosition(node, source)
			expanded, names, err := expandStringAt(node.Value, lookup, position)
			if err != nil {
				return err
			}
			if strings.Contains(node.Value, "${") || strings.Contains(node.Value, "$$") {
				changed = true
			}
			node.Value = expanded
			for _, name := range names {
				refs = append(refs, envReference{Name: name, Path: path, Line: node.Line, Column: node.Column})
			}
		case yaml.AliasNode:
			if node.Alias != nil {
				for _, ref := range refs {
					if ref.Line == node.Alias.Line && ref.Column == node.Alias.Column {
						refs = append(refs, envReference{Name: ref.Name, Path: path, Line: node.Line, Column: node.Column})
					}
				}
			}
			return nil
		}
		return nil
	}
	if err := walk(root, ""); err != nil {
		return nil, false, err
	}
	return refs, changed, nil
}

func scalarPosition(node *yaml.Node, source []byte) sourcePosition {
	if len(source) == 0 {
		return func(offset int) (int, int) {
			prefix := node.Value[:offset]
			lastNewline := strings.LastIndexByte(prefix, '\n')
			if lastNewline < 0 {
				return node.Line, node.Column + len([]rune(prefix))
			}
			return node.Line + strings.Count(prefix, "\n"), 1 + len([]rune(prefix[lastNewline+1:]))
		}
	}
	lines := strings.Split(string(source), "\n")
	return func(offset int) (int, int) {
		needle := node.Value[offset:]
		if newline := strings.IndexByte(needle, '\n'); newline >= 0 {
			needle = needle[:newline]
		}
		if needle != "" {
			for line := node.Line - 1; line < len(lines); line++ {
				if column := strings.Index(lines[line], needle); column >= 0 {
					return line + 1, len([]rune(lines[line][:column])) + 1
				}
			}
		}
		return node.Line, node.Column
	}
}
