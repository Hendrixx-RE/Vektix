package parser

import (
	"context"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

// PDFPage contains the extracted text of a single PDF page.
type PDFPage struct {
	Number  int // 1-based page number
	Content string
}

// PDFDocument represents a parsed PDF file.
type PDFDocument struct {
	Pages []PDFPage
}

// ParsePDF extracts text from a PDF file located at path.
// It isolates the parsing in a goroutine with panic recovery
// and respects the provided timeout via context, ensuring that
// malformed PDFs do not abort the indexer.
func ParsePDF(ctx context.Context, path string) (*PDFDocument, error) {
	type result struct {
		doc *PDFDocument
		err error
	}
	
	resCh := make(chan result, 1)
	
	go func() {
		defer func() {
			if r := recover(); r != nil {
				resCh <- result{err: fmt.Errorf("panic during PDF parsing: %v", r)}
			}
		}()
		
		f, r, err := pdf.Open(path)
		if err != nil {
			resCh <- result{err: fmt.Errorf("failed to open PDF: %w", err)}
			return
		}
		defer f.Close()
		
		numPages := r.NumPage()
		var pages []PDFPage
		
		for i := 1; i <= numPages; i++ {
			// Check context before heavy work on each page
			if ctx.Err() != nil {
				resCh <- result{err: ctx.Err()}
				return
			}
			
			p := r.Page(i)
			text, err := p.GetPlainText(nil)
			if err != nil {
				// Returning error ensures quarantine of the file.
				resCh <- result{err: fmt.Errorf("failed to read page %d: %w", i, err)}
				return
			}
			
			// Some pages might be empty, but we still append them to maintain correct numbering.
			pages = append(pages, PDFPage{
				Number:  i,
				Content: strings.TrimSpace(text),
			})
		}
		
		resCh <- result{doc: &PDFDocument{Pages: pages}}
	}()
	
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		return res.doc, res.err
	}
}
