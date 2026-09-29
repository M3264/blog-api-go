package blog

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"net/url"
	"strings"
)

type Node struct {
	Type    string         `json:"type"`
	Text    string         `json:"text,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []Node         `json:"content,omitempty"`
	Marks   []Node         `json:"marks,omitempty"`
}

func SafeURL(s string, image bool) bool {
	if strings.HasPrefix(s, "/media/") && !strings.Contains(s, "..") && !strings.ContainsAny(s, "\r\n\\") {
		return true
	}
	u, e := url.Parse(s)
	return e == nil && u.Host != "" && u.User == nil && (u.Scheme == "https" || (!image && u.Scheme == "http"))
}
func RenderContent(raw json.RawMessage, body string) (template.HTML, string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		var out strings.Builder
		for _, p := range strings.Split(body, "\n\n") {
			out.WriteString("<p>" + strings.ReplaceAll(html.EscapeString(p), "\n", "<br>") + "</p>")
		}
		return template.HTML(out.String()), body, nil
	}
	if len(raw) > 1<<20 {
		return "", "", errors.New("article content too large")
	}
	var root Node
	if e := json.Unmarshal(raw, &root); e != nil || root.Type != "doc" {
		return "", "", errors.New("content must be a document")
	}
	count := 0
	var render func(Node, int) (string, string, error)
	render = func(n Node, depth int) (string, string, error) {
		count++
		if depth > 24 || count > 20000 {
			return "", "", errors.New("article content too complex")
		}
		var h, t strings.Builder
		for _, c := range n.Content {
			a, b, e := render(c, depth+1)
			if e != nil {
				return "", "", e
			}
			h.WriteString(a)
			t.WriteString(b)
		}
		inner, text := h.String(), t.String()
		switch n.Type {
		case "text":
			inner = html.EscapeString(n.Text)
			text = n.Text
			for _, m := range n.Marks {
				switch m.Type {
				case "bold":
					inner = "<strong>" + inner + "</strong>"
				case "italic":
					inner = "<em>" + inner + "</em>"
				case "underline":
					inner = "<u>" + inner + "</u>"
				case "strike":
					inner = "<s>" + inner + "</s>"
				case "code":
					inner = "<code>" + inner + "</code>"
				case "link":
					href, _ := m.Attrs["href"].(string)
					if !SafeURL(href, false) {
						return "", "", errors.New("unsafe article link")
					}
					inner = `<a rel="noopener noreferrer" href="` + html.EscapeString(href) + `">` + inner + `</a>`
				default:
					return "", "", errors.New("unsupported mark")
				}
			}
		case "doc":
		case "paragraph":
			inner = "<p>" + inner + "</p>"
			text += "\n\n"
		case "heading":
			level, _ := n.Attrs["level"].(float64)
			if level < 2 || level > 4 {
				level = 2
			}
			tag := fmt.Sprintf("h%d", int(level))
			inner = "<" + tag + ">" + inner + "</" + tag + ">"
			text += "\n\n"
		case "bulletList":
			inner = "<ul>" + inner + "</ul>"
		case "orderedList":
			inner = "<ol>" + inner + "</ol>"
		case "listItem":
			inner = "<li>" + inner + "</li>"
		case "blockquote":
			inner = "<blockquote>" + inner + "</blockquote>"
		case "codeBlock":
			inner = "<pre><code>" + inner + "</code></pre>"
			text += "\n\n"
		case "hardBreak":
			inner = "<br>"
			text = "\n"
		case "horizontalRule":
			inner = "<hr>"
		case "image":
			src, _ := n.Attrs["src"].(string)
			alt, _ := n.Attrs["alt"].(string)
			if !SafeURL(src, true) {
				return "", "", errors.New("unsafe image URL")
			}
			inner = `<img loading="lazy" src="` + html.EscapeString(src) + `" alt="` + html.EscapeString(alt) + `">`
		default:
			return "", "", errors.New("unsupported article node")
		}
		return inner, text, nil
	}
	h, t, e := render(root, 0)
	return template.HTML(h), strings.TrimSpace(t), e
}
