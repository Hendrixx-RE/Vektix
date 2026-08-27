package parser

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParsePDF_Valid(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	doc, err := ParsePDF(ctx, "testdata/valid.pdf")
	if err != nil {
		t.Fatalf("ParsePDF failed: %v", err)
	}

	if len(doc.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(doc.Pages))
	}

	if doc.Pages[0].Number != 1 {
		t.Errorf("expected page number 1, got %d", doc.Pages[0].Number)
	}

	if !strings.Contains(doc.Pages[0].Content, "Hello World") {
		t.Errorf("expected content to contain 'Hello World', got %q", doc.Pages[0].Content)
	}
}

func TestParsePDF_PanicRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// panic.pdf is crafted to trigger a panic in ledongthuc/pdf due to 
	// invalid object offsets (unexpected keyword "ndobj" parsing object, or EOF panic).
	// This tests that our ParsePDF function isolates the panic in a goroutine
	// and returns it as a normal error, allowing the index run to continue.
	doc, err := ParsePDF(ctx, "testdata/panic.pdf")
	if err == nil {
		t.Fatal("expected error from malformed/panic-inducing PDF, got none")
	}

	if doc != nil {
		t.Errorf("expected nil document on failure, got %+v", doc)
	}
	
	if !strings.Contains(err.Error(), "panic during PDF parsing") {
		t.Errorf("expected error to mention 'panic during PDF parsing', got: %v", err)
	}
}

func TestParsePDF_Timeout(t *testing.T) {
	// A very short timeout to ensure the context cancellation works
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(2 * time.Millisecond) // ensure it's already expired

	doc, err := ParsePDF(ctx, "testdata/valid.pdf")
	if err == nil {
		t.Fatal("expected error due to timeout, got none")
	}

	if doc != nil {
		t.Errorf("expected nil document on failure, got %+v", doc)
	}
}
