package actressranking

import (
	"strings"

	"golang.org/x/net/html"
)

// dom.go owns the HTML traversal helpers shared by ranking parsers.
//
// Ownership summary:
// 1) provide shared DOM traversal helpers for ranking parsers
// 2) centralize attribute/class/node walking utilities for ranking HTML
// 3) keep ranking DOM utilities separate from fetch and cache behavior
//
// File map for maintainers:
// 1) HTML parse entrypoint
// 2) attribute/class lookup helpers
// 3) node walking and text extraction utilities

func parseHTMLDocument(source string) (*html.Node, error) {
	return html.Parse(strings.NewReader(source))
}

func getAttribute(node *html.Node, name string) string {
	if node == nil {
		return ""
	}
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func hasClass(node *html.Node, className string) bool {
	classAttr := getAttribute(node, "class")
	if classAttr == "" {
		return false
	}
	for _, item := range strings.Fields(classAttr) {
		if item == className {
			return true
		}
	}
	return false
}

func hasAllClasses(node *html.Node, classNames ...string) bool {
	for _, className := range classNames {
		if !hasClass(node, className) {
			return false
		}
	}
	return true
}

func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}

	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(node)
	return strings.TrimSpace(builder.String())
}

func findFirst(node *html.Node, predicate func(*html.Node) bool) *html.Node {
	if node == nil {
		return nil
	}
	if predicate(node) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if matched := findFirst(child, predicate); matched != nil {
			return matched
		}
	}
	return nil
}

func findAll(node *html.Node, predicate func(*html.Node) bool) []*html.Node {
	if node == nil {
		return nil
	}

	results := make([]*html.Node, 0)
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if predicate(current) {
			results = append(results, current)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(node)
	return results
}

func firstElementChild(node *html.Node, predicate func(*html.Node) bool) *html.Node {
	if node == nil {
		return nil
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if predicate(child) {
			return child
		}
	}
	return nil
}
