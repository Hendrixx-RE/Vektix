package excerpt

import (
	"strings"
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/store"
	"github.com/mattn/go-runewidth"
)

func TestRenderLineRange(t *testing.T) {
	chunk := store.Chunk{
		Path:    "~/notes/infra.md",
		Content: "DATABASE_URL=postgres://dev@localhost:5432/appdb",
		Locator: store.Locator{Kind: store.LocatorLineRange, Start: 41, End: 43},
	}
	
	expandedText := `## Local Postgres

DATABASE_URL=postgres://dev@localhost:5432/appdb`

	cfg := RenderConfig{
		HeaderRankInfo: "(bm25+vec, rank 1)",
	}

	result := Render(chunk, expandedText, chunk.Locator, cfg)

	// Since we padded the header to 80 chars
	// width of headerLeft: len("~/notes/infra.md:41-43") = 22
	// padding = 80 - 22 - 18 = 40
	expectedHeader := "~/notes/infra.md:41-43                                        (bm25+vec, rank 1)\n"
	
	expectedBody := `   41 | ## Local Postgres
   42 | 
   43 | ` + "\x1b[33mDATABASE_URL=postgres://dev@localhost:5432/appdb\x1b[0m" + `
`

	expected := expectedHeader + expectedBody

	if result != expected {
		t.Errorf("expected:\n%q\n\ngot:\n%q\n", expected, result)
	}
}

func TestRenderPage(t *testing.T) {
	chunk := store.Chunk{
		Path:    "~/docs/manual.pdf",
		Content: "matched text",
		Locator: store.Locator{Kind: store.LocatorPage, Start: 12},
	}
	
	expandedText := `This is some expanded text
with the matched text inside it.`

	cfg := RenderConfig{
		HeaderRankInfo: "(rank 2)",
	}

	result := Render(chunk, expandedText, chunk.Locator, cfg)
	
	expectedHeader := "~/docs/manual.pdf:page 12                                               (rank 2)\n"
	expectedBody := `   12 | This is some expanded text
   12 | with the ` + "\x1b[33mmatched text\x1b[0m" + ` inside it.
`
	
	expected := expectedHeader + expectedBody

	if result != expected {
		t.Errorf("expected:\n%q\n\ngot:\n%q\n", expected, result)
	}
}

func TestRenderNonASCIIHeaderAlignment(t *testing.T) {
	chunk := store.Chunk{
		Path:    "~/文档/projekt/über_uns.md",
		Content: "Herzlich willkommen!",
		Locator: store.Locator{Kind: store.LocatorLineRange, Start: 5, End: 5},
	}

	cfg := RenderConfig{
		HeaderRankInfo: "(bm25, rank 1)",
	}

	result := Render(chunk, chunk.Content, chunk.Locator, cfg)
	lines := strings.Split(result, "\n")
	header := lines[0]

	displayLen := runewidth.StringWidth(header)
	if displayLen != 80 {
		t.Errorf("expected header display width 80, got %d for header %q", displayLen, header)
	}
	if !strings.HasSuffix(header, "(bm25, rank 1)") {
		t.Errorf("expected header to end with rank info, got %q", header)
	}
}

func TestRenderTabExpansion(t *testing.T) {
	chunk := store.Chunk{
		Path:    "~/code/main.go",
		Content: "\t\treturn true",
		Locator: store.Locator{Kind: store.LocatorLineRange, Start: 10, End: 11},
	}

	expandedText := "func test() bool {\n\t\treturn true\n}"

	cfg := RenderConfig{
		HeaderRankInfo: "(rank 1)",
		NoColor:        true,
	}

	result := Render(chunk, expandedText, chunk.Locator, cfg)

	if strings.Contains(result, "\t") {
		t.Errorf("result should not contain unexpanded tabs, got:\n%s", result)
	}

	expectedBody := "   10 | func test() bool {\n   11 |         return true\n   12 | }\n"
	lines := strings.Split(result, "\n")
	body := strings.Join(lines[1:], "\n")
	if body != expectedBody {
		t.Errorf("expected body:\n%q\ngot:\n%q", expectedBody, body)
	}
}

func TestRenderNoColor(t *testing.T) {
	chunk := store.Chunk{
		Path:    "~/notes/plain.txt",
		Content: "match without color",
		Locator: store.Locator{Kind: store.LocatorLineRange, Start: 1, End: 1},
	}

	cfg := RenderConfig{
		HeaderRankInfo: "(rank 1)",
		NoColor:        true,
	}

	result := Render(chunk, "match without color", chunk.Locator, cfg)
	if strings.Contains(result, "\x1b[") {
		t.Errorf("expected no ANSI escape codes with NoColor=true, got:\n%q", result)
	}
}
