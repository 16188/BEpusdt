package router

import (
	"html/template"
	"io/fs"
	"strings"
	"testing"

	"github.com/v03413/bepusdt/static"
)

func TestLangGeCheckoutTemplateIsEmbedded(t *testing.T) {
	checkout, err := readCheckoutInfoFromFS(static.Checkout, "checkout/langge")
	if err != nil {
		t.Fatalf("read langge checkout info: %v", err)
	}

	if checkout.Name != "LangGe design" {
		t.Fatalf("unexpected checkout name: %q", checkout.Name)
	}
	if checkout.Author == "" {
		t.Fatal("checkout author is required")
	}
	if checkout.Desc == "" {
		t.Fatal("checkout desc is required")
	}

	view, err := fs.ReadFile(static.Checkout, "checkout/langge/views/checkout.html")
	if err != nil {
		t.Fatalf("read langge checkout template: %v", err)
	}
	if !strings.Contains(string(view), "{{ .trade_id }}") {
		t.Fatal("langge checkout template must only depend on trade_id server injection")
	}

	tmpl := template.New("default")
	if !registerTemplatesFromFS(tmpl, static.Checkout, "checkout/langge", "langge") {
		t.Fatal("langge checkout template was not registered")
	}
	if tmpl.Lookup("langge/checkout.html") == nil {
		t.Fatal("langge checkout template was not registered under expected name")
	}
}

func TestOfficialCheckoutCustomizationIsEmbedded(t *testing.T) {
	view, err := fs.ReadFile(static.Checkout, "checkout/official/views/checkout.html")
	if err != nil {
		t.Fatalf("read official checkout template: %v", err)
	}
	html := string(view)
	if !strings.Contains(html, `class="brand-wordmark">Crypto</span>`) || !strings.Contains(html, "data-language-toggle") {
		t.Fatal("official checkout must include the custom logo and language toggle")
	}
	if !strings.Contains(html, "checkout.css?v=") || !strings.Contains(html, "checkout.js?v=") {
		t.Fatal("official checkout assets must use cache-busting URLs")
	}
	if strings.Contains(html, "Powered by") || strings.Contains(html, "card-footer") || strings.Contains(html, "modal-footer") {
		t.Fatal("official checkout must not include copyright footers")
	}
}
