package planimage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
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
		if len(x) == 0 {
			return "[]"
		}
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

func number(o object, key string) (float64, bool) {
	v, ok := o.get(key)
	if !ok {
		return 0, false
	}
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil
}

func children(p object) []object {
	v, _ := p.get("Plans")
	arr, _ := v.([]any)
	var cs []object
	for _, c := range arr {
		if o, ok := c.(object); ok {
			cs = append(cs, o)
		}
	}
	return cs
}

// totalRows は、ノードが親に渡した行数の合計。Actual Rows はループ1回あたりの平均なので Loops を掛ける
func totalRows(p object) (rows float64, actual bool) {
	if r, ok := number(p, "Actual Rows"); ok {
		loops, ok := number(p, "Actual Loops")
		if !ok {
			loops = 1
		}
		return r * loops, true
	}
	r, _ := number(p, "Plan Rows")
	return r, false
}

// weight は、ノードの重さ。時間があれば時間(ループ全体)、なければコストを使う
func weight(p object, useTime bool) float64 {
	if useTime {
		t, _ := number(p, "Actual Total Time")
		loops, ok := number(p, "Actual Loops")
		if !ok {
			loops = 1
		}
		return t * loops
	}
	c, _ := number(p, "Total Cost")
	return c
}

// selfShare は、子の分を引いたノード自身の重さが、計画全体の何割か。重さがわからなければ ok は false
func selfShare(p object, root float64, useTime bool) (float64, bool) {
	if root <= 0 {
		return 0, false
	}
	self := weight(p, useTime)
	for _, c := range children(p) {
		self -= weight(c, useTime)
	}
	return math.Max(self, 0) / root, true
}

func commas(f float64) string {
	s := fmt.Sprintf("%.0f", f)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
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
	svg, err := renderSVG(plan, extras)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(pi.Dir, name), []byte(svg), 0o644); err != nil {
		return "", fmt.Errorf("画像を書けません: %w", err)
	}
	return fmt.Sprintf("[[file:%s%s]]", pi.LinkPrefix, name), nil
}
