package okf

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"
	"go.yaml.in/yaml/v4/plugin/limit"
)

const maxFrontmatterBytes = 1 << 20
const maxProjectionNodes = 100_000
const maxYAMLDepth = 128

var coreInteger = regexp.MustCompile(`^[-+]?(?:[0-9][0-9_]*|0o[0-7_]+|0x[0-9a-fA-F_]+)$`)
var coreDecimal = regexp.MustCompile(`^[-+]?(?:\.[0-9_]+(?:[eE][-+]?[0-9]+)?|[0-9][0-9_]*\.[0-9_]*(?:[eE][-+]?[0-9]+)?|[0-9][0-9_]*[eE][-+]?[0-9]+)$`)

func frontmatterNode(text string) (*yaml.Node, error) {
	if len(text) > maxFrontmatterBytes {
		return nil, fmt.Errorf("%w: resource limit exceeded: frontmatter_bytes", ErrInvalidFrontmatter)
	}
	loader, err := yaml.NewLoader(strings.NewReader(text), yaml.WithPlugin(limit.New(limit.DepthValue(maxYAMLDepth))))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFrontmatter, err)
	}
	var document yaml.Node
	if err := loader.Load(&document); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidFrontmatter, err)
	}
	var extra yaml.Node
	if err := loader.Load(&extra); err != io.EOF {
		return nil, fmt.Errorf("%w: expected one YAML document", ErrInvalidFrontmatter)
	}
	if len(document.Content) == 0 {
		return nil, nil
	}
	return document.Content[0], nil
}

func ParseFrontmatter(text string) (map[string]any, []string, error) {
	node, err := frontmatterNode(text)
	if err != nil {
		return nil, nil, err
	}
	if node == nil {
		return map[string]any{}, nil, nil
	}
	if node.Kind != yaml.MappingNode {
		if node.Kind == yaml.ScalarNode {
			return nil, nil, fmt.Errorf("%w: expected key-value pair on line %d", ErrInvalidFrontmatter, node.Line)
		}
		return nil, nil, fmt.Errorf("%w: expected a mapping", ErrInvalidFrontmatter)
	}
	remaining := maxProjectionNodes
	value, err := projectYAML(node, map[*yaml.Node]bool{}, &remaining)
	if err != nil {
		return nil, nil, err
	}
	order := make([]string, 0, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		// Projection already validates every key as a string and checks aliases.
		key := node.Content[i]
		for key.Kind == yaml.AliasNode {
			key = key.Alias
		}
		order = append(order, key.Value)
	}
	return value.(map[string]any), order, nil
}

// ParseValue uses the same portable projection as document frontmatter.
func ParseValue(text string) (any, error) {
	node, err := frontmatterNode(text)
	if err != nil || node == nil {
		return nil, err
	}
	remaining := maxProjectionNodes
	return projectYAML(node, map[*yaml.Node]bool{}, &remaining)
}

func projectYAML(node *yaml.Node, active map[*yaml.Node]bool, remaining *int) (any, error) {
	*remaining--
	if *remaining < 0 || len(active) > maxYAMLDepth {
		return nil, fmt.Errorf("%w: resource limit exceeded: YAML projection", ErrInvalidFrontmatter)
	}
	if active[node] {
		return nil, fmt.Errorf("%w: aliases must be acyclic on line %d", ErrInvalidFrontmatter, node.Line)
	}
	active[node] = true
	defer delete(active, node)
	if node.Kind == yaml.AliasNode {
		return projectYAML(node.Alias, active, remaining)
	}
	if node.Style&yaml.TaggedStyle != 0 {
		switch node.ShortTag() {
		case "!!str", "!!null", "!!bool", "!!int", "!!float":
			if node.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("%w: scalar tag requires a scalar on line %d", ErrInvalidFrontmatter, node.Line)
			}
		case "!!seq":
			if node.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("%w: sequence tag requires a sequence on line %d", ErrInvalidFrontmatter, node.Line)
			}
		case "!!map":
			if node.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("%w: mapping tag requires a mapping on line %d", ErrInvalidFrontmatter, node.Line)
			}
		default:
			return nil, fmt.Errorf("%w: unsupported YAML tag on line %d", ErrInvalidFrontmatter, node.Line)
		}
	}
	switch node.Kind {
	case yaml.MappingNode:
		value := map[string]any{}
		for i := 0; i < len(node.Content); i += 2 {
			key, err := projectYAML(node.Content[i], active, remaining)
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("%w: expected string keys on line %d", ErrInvalidFrontmatter, node.Content[i].Line)
			}
			if _, exists := value[name]; exists {
				return nil, fmt.Errorf("%w: duplicate key %q on line %d", ErrInvalidFrontmatter, name, node.Content[i].Line)
			}
			item, err := projectYAML(node.Content[i+1], active, remaining)
			if err != nil {
				return nil, err
			}
			value[name] = item
		}
		return value, nil
	case yaml.SequenceNode:
		value := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			item, err := projectYAML(child, active, remaining)
			if err != nil {
				return nil, err
			}
			value = append(value, item)
		}
		return value, nil
	case yaml.ScalarNode:
		if node.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 {
			return node.Value, nil
		}
		if node.Style&yaml.TaggedStyle != 0 {
			tag := node.ShortTag()
			if tag == "!!str" {
				return node.Value, nil
			}
			valid := false
			switch tag {
			case "!!null":
				valid = node.Value == "" || node.Value == "~" || node.Value == "null" || node.Value == "Null" || node.Value == "NULL"
			case "!!bool":
				valid = node.Value == "true" || node.Value == "True" || node.Value == "TRUE" || node.Value == "false" || node.Value == "False" || node.Value == "FALSE"
			case "!!int":
				valid = coreInteger.MatchString(node.Value)
			case "!!float":
				valid = coreDecimal.MatchString(node.Value) || coreInteger.MatchString(node.Value) && !strings.ContainsAny(node.Value, "ox")
			}
			if !valid {
				return nil, fmt.Errorf("%w: invalid %s value on line %d", ErrInvalidFrontmatter, tag, node.Line)
			}
		} else if node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			return node.Value, nil
		}
		return coreScalar(node.Value)
	}
	return nil, fmt.Errorf("%w: unsupported YAML node", ErrInvalidFrontmatter)
}

func coreScalar(value string) (any, error) {
	switch value {
	case "", "~", "null", "Null", "NULL":
		return nil, nil
	case "true", "True", "TRUE":
		return true, nil
	case "false", "False", "FALSE":
		return false, nil
	}
	normalized := strings.ReplaceAll(value, "_", "")
	if coreInteger.MatchString(value) {
		unsigned := strings.TrimLeft(normalized, "+-")
		base := 10
		if strings.HasPrefix(unsigned, "0o") {
			base, unsigned = 8, unsigned[2:]
		} else if strings.HasPrefix(unsigned, "0x") {
			base, unsigned = 16, unsigned[2:]
		}
		number, ok := new(big.Int).SetString(unsigned, base)
		if !ok {
			return nil, fmt.Errorf("%w: invalid integer", ErrInvalidFrontmatter)
		}
		if strings.HasPrefix(normalized, "-") {
			number.Neg(number)
		}
		if number.IsInt64() {
			return number.Int64(), nil
		}
		return json.Number(number.String()), nil
	}
	if coreDecimal.MatchString(value) {
		negative := strings.HasPrefix(normalized, "-")
		normalized = strings.TrimLeft(normalized, "+-")
		parts := strings.FieldsFunc(normalized, func(r rune) bool { return r == 'e' || r == 'E' })
		mantissa := strings.SplitN(parts[0], ".", 2)
		whole := strings.TrimLeft(mantissa[0], "0")
		if whole == "" {
			whole = "0"
		}
		if len(mantissa) == 2 && mantissa[1] != "" {
			whole += "." + mantissa[1]
		}
		if len(parts) == 2 {
			whole += "e" + parts[1]
		}
		if negative {
			whole = "-" + whole
		}
		return json.Number(whole), nil
	}
	switch strings.ToLower(strings.TrimLeft(value, "+-")) {
	case ".inf", ".nan":
		return nil, fmt.Errorf("%w: numbers must be finite", ErrInvalidFrontmatter)
	}
	return value, nil
}
