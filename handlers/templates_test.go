package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/marvinrabe/goatcounter"
)

func TestTemplateReloadAndEscaping(t *testing.T) {
	previous := pageTemplates.Load()
	t.Cleanup(func() { pageTemplates.Store(previous) })
	files := fstest.MapFS{"page.gohtml": {Data: []byte(`<p>{{.}}</p>`)}}
	if err := LoadTemplates(files); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		got, err := renderTemplate("page.gohtml", `<script>alert("x")</script>`)
		if err != nil || got != want {
			t.Fatalf("render = %q, %v; want %q", got, err, want)
		}
	}
	const escaped = `&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`
	check("<p>" + escaped + "</p>")
	files["page.gohtml"].Data = []byte(`{{if}}`)
	if err := LoadTemplates(files); err == nil {
		t.Fatal("invalid template was accepted")
	}
	check("<p>" + escaped + "</p>")
	files["page.gohtml"].Data = []byte(`<b>{{.}}</b>`)
	if err := LoadTemplates(files); err != nil {
		t.Fatal(err)
	}
	check("<b>" + escaped + "</b>")
}

func TestRenderHTMLBuffersErrors(t *testing.T) {
	previous := pageTemplates.Load()
	t.Cleanup(func() { pageTemplates.Store(previous) })
	if err := LoadTemplates(fstest.MapFS{"page.gohtml": {Data: []byte(`partial page {{.Missing}}`)}}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := renderHTML(w, "page.gohtml", struct{}{}); err == nil {
		t.Fatal("expected missing field error")
	}
	if w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
		t.Fatalf("render error sent a partial response: %#v, %q", w.Header(), w.Body.String())
	}
}

func TestChartTemplatesEscapeData(t *testing.T) {
	name := `/path/<script>alert("x")</script>`
	data := horizontalChartPages(goatcounter.HitLists{{Path: name, PathID: 123, Count: 1234}}, 1234, 0, goatcounter.HitStats{}, 7, false)
	got, err := renderTemplate("_chart.gohtml", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="rows pages"`, `data-id="123"`, `data-count="1234"`, `style="width: 100%"`, `&lt;script&gt;`, "1\u202f234"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	for _, unwanted := range []string{`<script>`, `ZgotmplZ`} {
		if strings.Contains(got, unwanted) {
			t.Errorf("unsafe or filtered data %q in %s", unwanted, got)
		}
	}
}
