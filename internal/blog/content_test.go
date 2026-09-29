package blog

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContentRenderingRejectsUnsafeNodesAndLinks(t *testing.T) {
	for _, raw := range []string{`{"type":"doc","content":[{"type":"script","text":"bad"}]}`, `{"type":"doc","content":[{"type":"text","text":"bad","marks":[{"type":"link","attrs":{"href":"javascript:alert(1)"}}]}]}`, `{"type":"doc","content":[{"type":"image","attrs":{"src":"data:image/svg+xml,bad"}}]}`} {
		if _, _, e := RenderContent(json.RawMessage(raw), ""); e == nil {
			t.Fatalf("unsafe accepted: %s", raw)
		}
	}
	h, text, e := RenderContent(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"<script>hello</script>","marks":[{"type":"bold"}]}]}]}`), "")
	if e != nil || !strings.Contains(string(h), "<strong>&lt;script&gt;") || text != "<script>hello</script>" {
		t.Fatalf("safe render failed: %s %s %v", h, text, e)
	}
}
