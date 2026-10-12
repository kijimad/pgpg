package planimage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type PlanImage struct {
	Dir string
	// リンクの前に付けるパス(例: "images/")
	LinkPrefix string
}

var errNotJSONPlan = errors.New("結果が実行計画の JSON ではありません。EXPLAIN (FORMAT JSON) を付けてください")

// キーの順番を保つため、JSON のオブジェクトをスライスで持つ
type field struct {
	Key   string
	Value any
}

type object []field

func (o object) get(key string) (any, bool) {
	for _, f := range o {
		if f.Key == key {
			return f.Value, true
		}
	}
	return nil, false
}

func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			var obj object
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := keyTok.(string)
				val, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				obj = append(obj, field{Key: key, Value: val})
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	default:
		return tok, nil
	}
}

func parsePlanJSON(text string) (plan object, extras object, err error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	v, err := decodeOrdered(dec)
	if err != nil {
		return nil, nil, errNotJSONPlan
	}
	if dec.More() {
		return nil, nil, errNotJSONPlan
	}
	top, ok := v.([]any)
	if !ok || len(top) == 0 {
		return nil, nil, errNotJSONPlan
	}
	first, ok := top[0].(object)
	if !ok {
		return nil, nil, errNotJSONPlan
	}
	pv, ok := first.get("Plan")
	if !ok {
		return nil, nil, errNotJSONPlan
	}
	plan, ok = pv.(object)
	if !ok {
		return nil, nil, errNotJSONPlan
	}
	if nt, ok := plan.get("Node Type"); !ok {
		return nil, nil, errNotJSONPlan
	} else if _, ok := nt.(string); !ok {
		return nil, nil, errNotJSONPlan
	}
	for _, f := range first {
		if f.Key != "Plan" {
			extras = append(extras, f)
		}
	}
	return plan, extras, nil
}

// 数値は JSON に書かれた文字のまま出す(10.0 を 10 にしない)
func formatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "true"
		}
		return "false"
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = formatValue(e)
		}
		return strings.Join(parts, ", ")
	case object:
		parts := make([]string, len(x))
		for i, f := range x {
			parts[i] = f.Key + ": " + formatValue(f.Value)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprint(x)
	}
}

func dotLabel(lines []string) string {
	var b strings.Builder
	for _, line := range lines {
		line = strings.ReplaceAll(line, `\`, `\\`)
		line = strings.ReplaceAll(line, `"`, `\"`)
		line = strings.ReplaceAll(line, "\n", `\l`)
		// \l は Graphviz で「左寄せして改行」
		b.WriteString(line + `\l`)
	}
	return b.String()
}

// 情報を落とさないよう、子(Plans)以外のキーを全部、JSON の順番で出す
func planToDot(plan object, extras object) string {
	var b strings.Builder
	b.WriteString("digraph plan {\n")
	b.WriteString("  node [shape=box, fontname=\"monospace\"];\n")
	count := 0
	var node func(p object) string
	node = func(p object) string {
		count++
		id := fmt.Sprintf("n%d", count)
		nodeType, _ := p.get("Node Type")
		lines := []string{formatValue(nodeType)}
		var children []any
		for _, f := range p {
			switch f.Key {
			case "Node Type":
			case "Plans":
				children, _ = f.Value.([]any)
			default:
				lines = append(lines, f.Key+": "+formatValue(f.Value))
			}
		}
		fmt.Fprintf(&b, "  %s [label=\"%s\"];\n", id, dotLabel(lines))
		for _, c := range children {
			if child, ok := c.(object); ok {
				fmt.Fprintf(&b, "  %s -> %s;\n", id, node(child))
			}
		}
		return id
	}
	node(plan)
	if len(extras) > 0 {
		lines := make([]string, len(extras))
		for i, f := range extras {
			lines[i] = f.Key + ": " + formatValue(f.Value)
		}
		fmt.Fprintf(&b, "  summary [shape=note, label=\"%s\"];\n", dotLabel(lines))
	}
	b.WriteString("}\n")
	return b.String()
}

// 同じクエリなら同じ名前にして、実行し直したときは上書きする
func planImageName(query string, index int) string {
	sum := sha256.Sum256([]byte(query))
	name := "plan-" + hex.EncodeToString(sum[:])[:12]
	if index > 0 {
		name += fmt.Sprintf("-%d", index)
	}
	return name + ".svg"
}

func (pi *PlanImage) Render(planJSON, query string, index int) (string, error) {
	plan, extras, err := parsePlanJSON(planJSON)
	if err != nil {
		return "", err
	}
	name := planImageName(query, index)
	if err := os.MkdirAll(pi.Dir, 0o755); err != nil {
		return "", fmt.Errorf("画像のディレクトリを作れません: %w", err)
	}
	cmd := exec.Command("dot", "-Tsvg", "-o", filepath.Join(pi.Dir, name))
	cmd.Stdin = strings.NewReader(planToDot(plan, extras))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("Graphviz で描けません: %v: %s", err, stderr.String())
	}
	return fmt.Sprintf("[[file:%s%s]]", pi.LinkPrefix, name), nil
}
