package ytmusic

import (
	"strconv"
	"strings"
)

// node is a tolerant view onto a decoded JSON document. InnerTube responses
// are deeply nested and their exact layout changes over time, so the provider
// searches the tree for the renderers it knows instead of following fixed
// paths. Every accessor returns a zero value instead of panicking, which keeps
// the extraction code free of nil checks.
type node struct {
	value any
}

// wrap builds a node from a decoded JSON value.
func wrap(value any) node { return node{value: value} }

// exists reports whether the node holds a value.
func (n node) exists() bool { return n.value != nil }

// get walks down a chain of object keys.
func (n node) get(keys ...string) node {
	current := n.value
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return node{}
		}
		current, ok = object[key]
		if !ok {
			return node{}
		}
	}
	return node{value: current}
}

// index returns the i-th element of an array.
func (n node) index(i int) node {
	array, ok := n.value.([]any)
	if !ok || i < 0 || i >= len(array) {
		return node{}
	}
	return node{value: array[i]}
}

// array returns the elements of an array node.
func (n node) array() []node {
	raw, ok := n.value.([]any)
	if !ok {
		return nil
	}
	out := make([]node, 0, len(raw))
	for _, item := range raw {
		out = append(out, node{value: item})
	}
	return out
}

// str returns the node as a string. Numbers are rendered, everything else
// yields an empty string.
func (n node) str() string {
	switch v := n.value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return ""
	}
}

// int returns the node as an integer.
func (n node) int() int {
	switch v := n.value.(type) {
	case float64:
		return int(v)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

// findAll collects every value stored under key anywhere in the subtree, in
// document order.
func (n node) findAll(key string) []node {
	var out []node
	n.walk(func(k string, value node) bool {
		if k == key {
			out = append(out, value)
		}
		return true
	})
	return out
}

// findFirst returns the first value stored under key anywhere in the subtree.
func (n node) findFirst(key string) node {
	var found node
	n.walk(func(k string, value node) bool {
		if k == key {
			found = value
			return false
		}
		return true
	})
	return found
}

// walk visits every key/value pair in the subtree. The callback stops the walk
// by returning false.
func (n node) walk(visit func(key string, value node) bool) {
	var descend func(value any) bool
	descend = func(value any) bool {
		switch typed := value.(type) {
		case map[string]any:
			for _, key := range sortedKeys(typed) {
				child := typed[key]
				if !visit(key, node{value: child}) {
					return false
				}
				if !descend(child) {
					return false
				}
			}
		case []any:
			for _, item := range typed {
				if !descend(item) {
					return false
				}
			}
		}
		return true
	}
	descend(n.value)
}

// sortedKeys returns the keys of a map in a deterministic order so that
// repeated extractions of the same document always yield the same result.
func sortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	// InnerTube objects are small; insertion sort keeps this allocation free
	// of a sort.Slice closure.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// text renders a YouTube text object, which is either a list of runs or a
// simple string.
func (n node) text() string {
	if simple := n.get("simpleText"); simple.exists() {
		return strings.TrimSpace(simple.str())
	}
	runs := n.get("runs").array()
	if len(runs) == 0 {
		return strings.TrimSpace(n.str())
	}
	var b strings.Builder
	for _, run := range runs {
		b.WriteString(run.get("text").str())
	}
	return strings.TrimSpace(b.String())
}

// runs returns the individual runs of a text object.
func (n node) runs() []node {
	if runs := n.get("runs").array(); len(runs) > 0 {
		return runs
	}
	return nil
}

// thumbnailURL returns the largest thumbnail found in the subtree.
func (n node) thumbnailURL() string {
	var (
		best  string
		width int
	)
	for _, list := range n.findAll("thumbnails") {
		for _, thumb := range list.array() {
			url := thumb.get("url").str()
			if url == "" {
				continue
			}
			w := thumb.get("width").int()
			if best == "" || w > width {
				best, width = url, w
			}
		}
	}
	return best
}

// browseID returns the browse id a node navigates to. Like videoID, the known
// locations are tried in order of specificity first.
//
// The item's own navigation endpoint has to win over a plain subtree search:
// a renderer also carries an overflow menu, and on the "show all" pages that
// menu holds a "go to artist" entry whose browse endpoint is the artist. The
// walk visits keys in sorted order, so "menu" would otherwise be reached
// before "navigationEndpoint" and every release on those pages would be read
// as the artist it belongs to.
func (n node) browseID() string {
	if id := n.get("navigationEndpoint").findFirst("browseEndpoint").get("browseId").str(); id != "" {
		return id
	}
	if id := n.get("overlay").findFirst("browseEndpoint").get("browseId").str(); id != "" {
		return id
	}
	return n.findFirst("browseEndpoint").get("browseId").str()
}

// videoID returns the video id a node refers to. YouTube Music stores it in
// several places depending on the renderer, so the known locations are tried in
// order of specificity before falling back to a search of the subtree.
func (n node) videoID() string {
	if id := n.get("videoId").str(); id != "" {
		return id
	}
	if id := n.findFirst("playlistItemData").get("videoId").str(); id != "" {
		return id
	}
	if id := n.findFirst("watchEndpoint").get("videoId").str(); id != "" {
		return id
	}
	return n.findFirst("videoId").str()
}

// parseDuration converts "3:45" or "1:02:03" into milliseconds.
func parseDuration(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	parts := strings.Split(trimmed, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	total := 0
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value < 0 {
			return 0
		}
		total = total*60 + value
	}
	return total * 1000
}

// parseYear returns the first four digit year in the text.
func parseYear(text string) int {
	runes := []rune(text)
	for i := 0; i+4 <= len(runes); i++ {
		candidate := string(runes[i : i+4])
		value, err := strconv.Atoi(candidate)
		if err != nil {
			continue
		}
		if value >= 1900 && value <= 2100 {
			if i > 0 && isDigit(runes[i-1]) {
				continue
			}
			if i+4 < len(runes) && isDigit(runes[i+4]) {
				continue
			}
			return value
		}
	}
	return 0
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }
