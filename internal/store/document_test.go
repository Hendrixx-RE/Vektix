package store

import (
	"reflect"
	"testing"
)

func TestMetadataEncodeDecode(t *testing.T) {
	c := Chunk{
		ID:        "chunk-1",
		Path:      "test/path.go",
		Content:   "func test() {}",
		Embedding: []float32{1.0, 2.0},
		Locator: Locator{
			Kind:   LocatorSymbol,
			Start:  10,
			End:    20,
			Symbol: "func test()",
		},
	}

	m := c.EncodeMetadata()

	if m["path"] != "test/path.go" {
		t.Errorf("expected path test/path.go, got %s", m["path"])
	}
	if m["locator_kind"] != string(LocatorSymbol) {
		t.Errorf("expected kind %s, got %s", LocatorSymbol, m["locator_kind"])
	}
	if m["locator_start"] != "10" {
		t.Errorf("expected start 10, got %s", m["locator_start"])
	}
	if m["locator_end"] != "20" {
		t.Errorf("expected end 20, got %s", m["locator_end"])
	}
	if m["locator_sym"] != "func test()" {
		t.Errorf("expected sym func test(), got %s", m["locator_sym"])
	}

	decoded, err := DecodeMetadata(m)
	if err != nil {
		t.Fatalf("unexpected error decoding metadata: %v", err)
	}

	if decoded.Path != c.Path {
		t.Errorf("path mismatch: got %v, want %v", decoded.Path, c.Path)
	}
	if !reflect.DeepEqual(decoded.Locator, c.Locator) {
		t.Errorf("locator mismatch: got %+v, want %+v", decoded.Locator, c.Locator)
	}
}

func TestDecodeMetadata_InvalidInt(t *testing.T) {
	m := map[string]string{
		"path":          "test.txt",
		"locator_start": "not-an-int",
	}
	_, err := DecodeMetadata(m)
	if err == nil {
		t.Error("expected error for invalid locator_start, got nil")
	}

	m = map[string]string{
		"path":          "test.txt",
		"locator_start": "10",
		"locator_end":   "not-an-int",
	}
	_, err = DecodeMetadata(m)
	if err == nil {
		t.Error("expected error for invalid locator_end, got nil")
	}
}
