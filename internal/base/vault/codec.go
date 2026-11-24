package vault

import (
	"bytes"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
	"gopkg.in/yaml.v3"
)

func splitFrontmatter(data []byte, delimiter string) (string, string, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 2 || lines[0] != delimiter {
		return "", "", platform.Invalid("frontmatter", "資料缺少正確的檔頭")
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == delimiter {
			return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n"), nil
		}
	}
	return "", "", platform.Invalid("frontmatter", "資料檔頭沒有結束標記")
}
func Decode(data []byte) (r Record, order []string, err error) {
	header, body, err := splitFrontmatter(data, "---")
	if err != nil {
		return r, nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(header), &doc); err != nil {
		return r, nil, platform.Invalid("frontmatter", "資料檔頭不是有效的 YAML")
	}
	fields := map[string]any{}
	if len(doc.Content) == 0 {
		return decodedRecord(fields, body, data), nil, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return r, nil, platform.Invalid("frontmatter", "資料檔頭必須是欄位表格")
	}
	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return r, nil, platform.Invalid("frontmatter", "欄位名稱必須是文字")
		}
		if _, exists := fields[key.Value]; exists {
			return r, nil, platform.Invalid(key.Value, "欄位重複，請保留一個值")
		}
		value, err := decodeNode(root.Content[i+1], map[*yaml.Node]bool{})
		if err != nil {
			return r, nil, platform.Invalid(key.Value, "欄位內容無法解析")
		}
		fields[key.Value] = value
		order = append(order, key.Value)
	}
	return decodedRecord(fields, body, data), order, nil
}
func decodeNode(n *yaml.Node, visiting map[*yaml.Node]bool) (any, error) {
	if visiting[n] {
		return nil, platform.Invalid("frontmatter", "不支援循環參照")
	}
	visiting[n] = true
	defer delete(visiting, n)
	switch n.Kind {
	case yaml.AliasNode:
		return decodeNode(n.Alias, visiting)
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
				return nil, platform.Invalid("frontmatter", "欄位名稱必須是文字")
			}
			if _, ok := m[k.Value]; ok {
				return nil, platform.Invalid(k.Value, "欄位重複")
			}
			v, err := decodeNode(n.Content[i+1], visiting)
			if err != nil {
				return nil, err
			}
			m[k.Value] = v
		}
		return m, nil
	case yaml.SequenceNode:
		// Obsidian's unquoted link syntax is parsed as two nested flow sequences.
		if n.Style&yaml.FlowStyle != 0 && len(n.Content) == 1 {
			inner := n.Content[0]
			if inner.Kind == yaml.SequenceNode && inner.Style&yaml.FlowStyle != 0 && len(inner.Content) == 1 && inner.Content[0].Kind == yaml.ScalarNode && inner.Content[0].Tag == "!!str" {
				return "[[" + inner.Content[0].Value + "]]", nil
			}
		}
		values := make([]any, 0, len(n.Content))
		for _, item := range n.Content {
			v, err := decodeNode(item, visiting)
			if err != nil {
				return nil, err
			}
			values = append(values, v)
		}
		return values, nil
	case yaml.ScalarNode:
		if n.Tag == "!!timestamp" {
			return n.Value, nil
		}
		var v any
		err := n.Decode(&v)
		return v, err
	default:
		return nil, platform.Invalid("frontmatter", "不支援的欄位格式")
	}
}
func Encode(r Record, order []string, sc Spec) []byte {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	added := map[string]bool{}
	add := func(key string) {
		value, ok := r.Fields[key]
		if !ok || added[key] {
			return
		}
		added[key] = true
		var node yaml.Node
		// Unsupported Go values are a caller programming error: Encode accepts YAML data only.
		if err := node.Encode(value); err != nil {
			panic(err)
		}
		quoteLinks(&node)
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &node)
	}
	for _, f := range sc.Fields {
		add(f.Name)
	}
	for _, key := range order {
		add(key)
	}
	var remaining []string
	for key := range r.Fields {
		if !added[key] {
			remaining = append(remaining, key)
		}
	}
	sort.Strings(remaining)
	for _, key := range remaining {
		add(key)
	}
	var out bytes.Buffer
	out.WriteString("---\n")
	encoder := yaml.NewEncoder(&out)
	if err := encoder.Encode(root); err != nil {
		panic(err)
	}
	if err := encoder.Close(); err != nil {
		panic(err)
	}
	out.WriteString("---\n")
	body := normalizedBody(r.Body)
	if body != "" {
		out.WriteString(body)
		out.WriteByte('\n')
	}
	return out.Bytes()
}
func quoteLinks(n *yaml.Node) {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" && bracketLink.MatchString(n.Value) {
		n.Style = yaml.DoubleQuotedStyle
	}
	for _, child := range n.Content {
		quoteLinks(child)
	}
}
func DecodeLegacy(data []byte) (r Record, order []string, err error) {
	header, body, err := splitFrontmatter(data, "+++")
	if err != nil {
		return r, nil, err
	}
	fields := map[string]any{}
	meta, err := toml.Decode(header, &fields)
	if err != nil {
		return r, nil, platform.Invalid("frontmatter", "舊格式檔頭不是有效的 TOML")
	}
	seen := map[string]bool{}
	for _, key := range meta.Keys() {
		if len(key) > 0 && !seen[key[0]] {
			order = append(order, key[0])
			seen[key[0]] = true
		}
	}
	for key, value := range fields {
		fields[key] = normalizeLegacy(value)
	}
	return decodedRecord(fields, body, data), order, nil
}
func normalizeLegacy(value any) any {
	switch v := value.(type) {
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case map[string]any:
		for k, item := range v {
			v[k] = normalizeLegacy(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = normalizeLegacy(item)
		}
		return v
	default:
		return value
	}
}
