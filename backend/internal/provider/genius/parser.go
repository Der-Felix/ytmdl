package genius

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	ErrNoLyricsFound  = errors.New("no lyrics containers found in HTML")
	ErrChallengePage  = errors.New("detected bot challenge or captcha page")
	ErrInvalidContent = errors.New("invalid or insufficient lyrics content")
)

var (
	challengeKeywords = []string{
		"attention required! | cloudflare",
		"just a moment...",
		"checking your browser",
		"please enable javascript",
		"security check",
		"verify you are human",
		"cf-chl-bypass",
	}

	multiNewlineRe = regexp.MustCompile(`\n{3,}`)
)

// ParseLyrics extracts clean plain text lyrics from Genius song page HTML.
func ParseLyrics(r io.Reader) (string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", err
	}

	// 1. Detect challenge / bot protection pages early
	if isChallengePage(doc) {
		return "", ErrChallengePage
	}

	// 2. Find all lyrics containers
	containers := findLyricsContainers(doc)
	if len(containers) == 0 {
		return "", ErrNoLyricsFound
	}

	// 3. Extract text from each container in DOM order
	var containerTexts []string
	for _, c := range containers {
		var buf bytes.Buffer
		extractNodeText(c, &buf)
		text := strings.TrimSpace(buf.String())
		if text != "" {
			containerTexts = append(containerTexts, text)
		}
	}

	if len(containerTexts) == 0 {
		return "", ErrNoLyricsFound
	}

	// Join multiple containers (e.g. separated by ad breaks) with double newline
	combined := strings.Join(containerTexts, "\n\n")

	// 4. Clean up formatting
	cleaned := cleanLyricsText(combined)

	// 5. Content validation
	if len([]rune(cleaned)) < 30 {
		return "", ErrInvalidContent
	}

	lower := strings.ToLower(cleaned)
	for _, kw := range challengeKeywords {
		if strings.Contains(lower, kw) {
			return "", ErrChallengePage
		}
	}

	return cleaned, nil
}

// isChallengePage scans the document title and head for anti-bot signatures.
func isChallengePage(n *html.Node) bool {
	var check func(*html.Node) bool
	check = func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.DataAtom == atom.Title {
			var b strings.Builder
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					b.WriteString(c.Data)
				}
			}
			title := strings.ToLower(b.String())
			for _, kw := range challengeKeywords {
				if strings.Contains(title, kw) {
					return true
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if check(c) {
				return true
			}
		}
		return false
	}
	return check(n)
}

// findLyricsContainers locates elements with data-lyrics-container="true" or class="lyrics".
func findLyricsContainers(root *html.Node) []*html.Node {
	var containers []*html.Node

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Modern Genius: <div data-lyrics-container="true" ...>
			for _, attr := range n.Attr {
				if attr.Key == "data-lyrics-container" && attr.Val == "true" {
					containers = append(containers, n)
					return // Don't look for nested containers
				}
			}

			// Legacy Genius fallback: <div class="lyrics">
			if n.DataAtom == atom.Div {
				for _, attr := range n.Attr {
					if attr.Key == "class" && strings.Contains(attr.Val, "lyrics") && !strings.Contains(attr.Val, "header") {
						// Only if modern data-lyrics-container was not already found
						if len(containers) == 0 {
							containers = append(containers, n)
							return
						}
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(root)
	return containers
}

// extractNodeText recursively traverses a container node and extracts lyrics text.
func extractNodeText(n *html.Node, buf *bytes.Buffer) {
	if n == nil {
		return
	}

	switch n.Type {
	case html.TextNode:
		buf.WriteString(n.Data)

	case html.ElementNode:
		// Skip non-lyrics elements
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Svg, atom.Button, atom.Form,
			atom.Noscript, atom.Header, atom.Footer, atom.Nav, atom.Aside:
			return
		case atom.Br:
			buf.WriteByte('\n')
			return
		}

		// Check exclusion attribute
		for _, attr := range n.Attr {
			if attr.Key == "data-exclude-from-selection" && attr.Val == "true" {
				return
			}
		}

		// Process children
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extractNodeText(c, buf)
		}

		// Certain block elements should end with a newline if not already there
		if n.DataAtom == atom.P || n.DataAtom == atom.Div {
			b := buf.Bytes()
			if len(b) > 0 && b[len(b)-1] != '\n' {
				buf.WriteByte('\n')
			}
		}
	}
}

// cleanLyricsText normalizes line endings and removes redundant spacing.
func cleanLyricsText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	lines := strings.Split(s, "\n")
	var cleanedLines []string

	for _, line := range lines {
		trimmed := strings.TrimFunc(line, func(r rune) bool {
			return unicode.IsSpace(r) && r != '\n'
		})
		cleanedLines = append(cleanedLines, trimmed)
	}

	joined := strings.Join(cleanedLines, "\n")
	joined = multiNewlineRe.ReplaceAllString(joined, "\n\n")
	return strings.TrimSpace(joined)
}
