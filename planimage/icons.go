package planimage

import "strings"

// アイコンは 24 の格子に、線の太さ 1.75、丸い線端でそろえて描いた
var icons = map[string]string{
	"seqscan":     `<path d="M3 5h12M3 10h8M3 15h5"/><circle cx="16" cy="15" r="4"/><path d="m19 18 2.5 2.5"/>`,
	"indexscan":   `<circle cx="7.5" cy="15.5" r="4.5"/><path d="m10.7 12.3 9.3-9.3M17 6l3 3M14.5 8.5l2 2"/>`,
	"bitmap":      `<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><path d="M14 17.5h7M17.5 14v7"/>`,
	"nestedloop":  `<path d="M17 2l4 4-4 4"/><path d="M3 11v-1a4 4 0 0 1 4-4h14"/><path d="M7 22l-4-4 4-4"/><path d="M21 13v1a4 4 0 0 1-4 4H3"/>`,
	"hashjoin":    `<circle cx="9" cy="12" r="6"/><circle cx="15" cy="12" r="6"/>`,
	"mergejoin":   `<path d="M6 3v6a6 6 0 0 0 6 6 6 6 0 0 1 6 6"/><path d="M18 3v6a6 6 0 0 1-6 6"/>`,
	"hash":        `<path d="M4 9h16M4 15h16M10 3 8 21M16 3l-2 18"/>`,
	"sort":        `<path d="m21 16-4 4-4-4M17 20V4M3 8l4-4 4 4M7 4v16"/>`,
	"aggregate":   `<path d="M18 7V4H6l6 8-6 8h12v-3"/>`,
	"limit":       `<path d="M3 6h18M3 12h10M3 18h4"/><path d="M17 15l4 4M21 15l-4 4"/>`,
	"materialize": `<path d="m12 3 9 5-9 5-9-5z"/><path d="m3 13 9 5 9-5"/>`,
	"gather":      `<path d="M3 4h4l5 8 5-8h4"/><path d="M12 12v8"/>`,
	"append":      `<rect x="4" y="3" width="16" height="6" rx="1"/><rect x="4" y="13" width="16" height="6" rx="1"/><path d="M12 9v4"/>`,
	"modify":      `<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z"/>`,
	"node":        `<circle cx="12" cy="12" r="3"/><circle cx="12" cy="12" r="9"/>`,
	"table":       `<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 10h18M9 10v10"/>`,
	"filter":      `<path d="M3 5h18l-7 8v6l-4 2v-8z"/>`,
	"rows":        `<path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/>`,
	"details":     `<path d="M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1"/><circle cx="15" cy="6" r="2"/><circle cx="9" cy="12" r="2"/><circle cx="17" cy="18" r="2"/>`,
	"flame":       `<path d="M12 3c.5 3 4.5 5 4.5 10a4.5 4.5 0 0 1-9 0c0-2.5 1.2-4 2.5-5 0 2 1 3 2 3 .5-3-.5-5 0-8z"/>`,
	"gauge":       `<path d="m12 14 4-4"/><path d="M3.34 19a10 10 0 1 1 17.32 0"/>`,
	"check":       `<circle cx="12" cy="12" r="9"/><path d="m8.5 12 2.5 2.5 4.5-5"/>`,
	"alert":       `<path d="M12 3 2 20h20z"/><path d="M12 10v4M12 17h.01"/>`,
	"up":          `<path d="M12 19V5M6 11l6-6 6 6"/>`,
	"bolt":        `<path d="M13 2 4 14h7l-1 8 9-12h-7z"/>`,
	"clock":       `<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>`,
}

type category struct {
	name                   string
	color, tint, ring, ink string
}

var (
	catScan      = category{"Scan", "#2563eb", "#eaf1ff", "#bfdbfe", "#1d4ed8"}
	catJoin      = category{"Join", "#ea580c", "#fff1e6", "#fed7aa", "#c2410c"}
	catTransform = category{"Transform", "#16a34a", "#ecfdf3", "#bbf7d0", "#15803d"}
	catSupport   = category{"Support", "#52525b", "#f1f1f0", "#e4e4e7", "#3f3f46"}
	catParallel  = category{"Parallel", "#0d9488", "#e6fbf8", "#99f6e4", "#0f766e"}
	catModify    = category{"Modify", "#7c3aed", "#f3edff", "#ddd6fe", "#6d28d9"}
)

func classify(nodeType string) (category, string) {
	switch {
	case nodeType == "Seq Scan":
		return catScan, "seqscan"
	case strings.HasPrefix(nodeType, "Bitmap"):
		return catScan, "bitmap"
	case strings.Contains(nodeType, "Index"):
		return catScan, "indexscan"
	case strings.Contains(nodeType, "Scan"):
		return catScan, "seqscan"
	case nodeType == "Nested Loop":
		return catJoin, "nestedloop"
	case nodeType == "Hash Join":
		return catJoin, "hashjoin"
	case nodeType == "Merge Join":
		return catJoin, "mergejoin"
	case strings.Contains(nodeType, "Join"):
		return catJoin, "hashjoin"
	case nodeType == "Hash":
		return catSupport, "hash"
	case nodeType == "Materialize" || nodeType == "Memoize":
		return catSupport, "materialize"
	case strings.Contains(nodeType, "Sort"):
		return catTransform, "sort"
	case strings.Contains(nodeType, "Aggregate") || nodeType == "Group" || nodeType == "WindowAgg" || nodeType == "Unique" || nodeType == "SetOp":
		return catTransform, "aggregate"
	case nodeType == "Limit":
		return catTransform, "limit"
	case strings.HasPrefix(nodeType, "Gather"):
		return catParallel, "gather"
	case strings.Contains(nodeType, "Append"):
		return catSupport, "append"
	case nodeType == "ModifyTable" || nodeType == "LockRows":
		return catModify, "modify"
	default:
		return catSupport, "node"
	}
}
