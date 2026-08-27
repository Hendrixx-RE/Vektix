package excerpt

import (
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/store"
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
	expectedBody := `      | This is some expanded text
      | with the ` + "\x1b[33mmatched text\x1b[0m" + ` inside it.
`
	
	expected := expectedHeader + expectedBody

	if result != expected {
		t.Errorf("expected:\n%q\n\ngot:\n%q\n", expected, result)
	}
}
