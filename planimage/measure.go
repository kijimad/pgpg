package planimage

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"sync"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// 上流 (github.com/vercel/geist-font) の可変フォントを、そのまま置いている。
// TTF で幅を測り、同じ書体の woff2 を SVG に埋め込む
//
//go:embed fonts
var fontFS embed.FS

type textStyle struct {
	mono    bool
	weight  int
	size    float64
	spacing float64 // letter-spacing(px)
	upper   bool
}

// go-text の Face と shaper は同時に使えないので、まとめて1つの鍵で守る
var typeset struct {
	sync.Mutex
	faces   map[textStyle]*font.Face
	shaper  shaping.HarfbuzzShaper
	wrapper shaping.LineWrapper
}

func (st textStyle) family() string {
	if st.mono {
		return "GeistMono"
	}
	return "Geist"
}

// 太さごとに読み込む。同じフォントを共有すると、shaper が最初の太さのまま測る
func (st textStyle) face() *font.Face {
	key := textStyle{mono: st.mono, weight: st.weight}
	if f, ok := typeset.faces[key]; ok {
		return f
	}
	data, err := fontFS.ReadFile("fonts/" + st.family() + "[wght].ttf")
	if err != nil {
		panic(fmt.Sprintf("埋め込んだフォントがありません: %v", err))
	}
	f, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		panic(fmt.Sprintf("埋め込んだフォントを読めません: %v", err))
	}
	f.SetVariations([]font.Variation{{Tag: ot.MustNewTag("wght"), Value: float32(st.weight)}})
	if typeset.faces == nil {
		typeset.faces = map[textStyle]*font.Face{}
	}
	typeset.faces[key] = f
	return f
}

func (st textStyle) shape(text []rune) shaping.Output {
	return typeset.shaper.Shape(shaping.Input{
		Text: text, RunStart: 0, RunEnd: len(text),
		Direction: di.DirectionLTR, Face: st.face(), Size: fixed.Int26_6(st.size * 64),
		Script: language.Latin, Language: language.NewLanguage("en"),
	})
}

// 字間は文字組みではなく CSS が文字ごとに足すので、ここで足す
func (st textStyle) width(s string) float64 {
	if st.upper {
		s = strings.ToUpper(s)
	}
	text := []rune(s)
	typeset.Lock()
	defer typeset.Unlock()
	return float64(st.shape(text).Advance)/64 + st.spacing*float64(len(text))
}

// Unicode の改行の規則 (UAX #14) で折り、1語が収まらなければ語の途中で折る
func (st textStyle) wrap(s string, limit float64) []string {
	text := []rune(s)
	typeset.Lock()
	defer typeset.Unlock()
	out := st.shape(text)
	lines, _ := typeset.wrapper.WrapParagraphF(shaping.WrapConfig{BreakPolicy: shaping.WhenNecessary},
		fixed.Int26_6(limit*64), text, shaping.NewSliceIterator([]shaping.Output{out}))
	var res []string
	for _, line := range lines {
		start, end := len(text), 0
		for _, o := range line {
			start = min(start, o.Runes.Offset)
			end = max(end, o.Runes.Offset+o.Runes.Count)
		}
		res = append(res, strings.TrimRight(string(text[start:end]), " "))
	}
	return res
}
