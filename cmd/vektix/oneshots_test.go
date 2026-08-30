package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/format"
	"github.com/Hendrixx-RE/Vektix/internal/index"
	"github.com/Hendrixx-RE/Vektix/internal/store"
)

// fixture is a small, fully self-contained corpus: two indexed roots each
// holding one file that mentions "postgres", plus a third indexed root that is
// deliberately empty, and a real Go file and a real dotenv file for the
// line-range and secrets-denylist tests. No test in this file talks to a real
// Ollama instance; the embed endpoint is a local httptest server.
type fixture struct {
	cfg                                          config.Config
	dataDir                                      string
	ollama                                       *httptest.Server
	projectDir, otherDir, emptyDir               string
	infraPath, otherDocPath, mainGoPath, envPath string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()

	projectDir := filepath.Join(root, "project")
	otherDir := filepath.Join(root, "other")
	emptyDir := filepath.Join(root, "empty-project")
	for _, d := range []string{
		filepath.Join(projectDir, "notes"),
		filepath.Join(otherDir, "notes"),
		emptyDir,
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	infraPath := filepath.Join(projectDir, "notes", "infra.md")
	infraContent := "Local Postgres Configuration\n\n" +
		"Connection settings for the postgres database used by this project.\n" +
		"Host db01, pool max 20, idle timeout 5 minutes.\n"
	if err := os.WriteFile(infraPath, []byte(infraContent), 0644); err != nil {
		t.Fatal(err)
	}

	otherDocPath := filepath.Join(otherDir, "notes", "docker.md")
	otherContent := "Some unrelated notes about postgres kept here for reference.\n"
	if err := os.WriteFile(otherDocPath, []byte(otherContent), 0644); err != nil {
		t.Fatal(err)
	}

	mainGoPath := filepath.Join(projectDir, "main.go")
	mainGoContent := "line one\nline two\nline three\nline four\nline five\n"
	if err := os.WriteFile(mainGoPath, []byte(mainGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	envPath := filepath.Join(projectDir, ".env")
	if err := os.WriteFile(envPath, []byte("SECRET_KEY=abcd1234"), 0600); err != nil {
		t.Fatal(err)
	}

	// A local, deterministic stand-in for Ollama's /api/embed. Every test in
	// this file runs against this instead of a live model.
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		embeddings := make([][]float32, len(req.Input))
		for i := range req.Input {
			embeddings[i] = []float32{1, 0, 0, 0}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": embeddings})
	}))
	t.Cleanup(ollama.Close)

	dataDir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.General.ScopeMode = "auto"
	cfg.Index.IndexDirs = []string{projectDir, otherDir, emptyDir}
	cfg.Safety.ConfineToRoots = true
	cfg.Safety.AllowSecrets = false
	cfg.Search.MaxResults = 8
	cfg.Search.RRFK = 60
	cfg.Search.MinArms = 1
	cfg.Search.OversampleFloor = 0.01
	cfg.Ollama.Host = ollama.URL
	cfg.Ollama.EmbeddingModel = "mock-embed"

	infraChunk := store.Chunk{
		ID:        "infra-1",
		Path:      infraPath,
		Content:   "postgres",
		Embedding: []float32{1, 0, 0, 0},
		Locator:   store.Locator{Kind: store.LocatorLineRange, Start: 3, End: 3},
	}
	otherChunk := store.Chunk{
		ID:        "other-1",
		Path:      otherDocPath,
		Content:   "postgres",
		Embedding: []float32{0, 1, 0, 0},
		Locator:   store.Locator{Kind: store.LocatorLineRange, Start: 1, End: 1},
	}

	db, err := store.NewPersistentDB(index.StorePath(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddDocuments(context.Background(), []store.Chunk{infraChunk, otherChunk}); err != nil {
		t.Fatal(err)
	}

	m := &index.Manifest{
		EmbeddingModel: cfg.Ollama.EmbeddingModel,
		Dim:            4,
		PrefixScheme:   "v1",
		ChunkerVersion: 1,
		Files: map[string]index.FileMeta{
			infraPath:    {Chunks: []string{"infra-1"}},
			otherDocPath: {Chunks: []string{"other-1"}},
		},
		DirCounts: map[string]int{
			"":         2,
			projectDir: 1,
			otherDir:   1,
		},
	}
	if err := m.SaveManifest(index.ManifestPath(dataDir)); err != nil {
		t.Fatal(err)
	}

	return &fixture{
		cfg:          cfg,
		dataDir:      dataDir,
		ollama:       ollama,
		projectDir:   projectDir,
		otherDir:     otherDir,
		emptyDir:     emptyDir,
		infraPath:    infraPath,
		otherDocPath: otherDocPath,
		mainGoPath:   mainGoPath,
		envPath:      envPath,
	}
}

// testEnv is a fresh *env over the fixture's corpus, with its own output
// buffers and no clipboard/editor side effects.
func (f *fixture) testEnv(cwd string) (*env, *bytes.Buffer, *bytes.Buffer) {
	cfg := f.cfg
	var out, errOut bytes.Buffer
	e := &env{
		cfg:     &cfg,
		dataDir: f.dataDir,
		cwd:     cwd,
		stdout:  &out,
		stderr:  &errOut,
		copyFn: func(w io.Writer, text string) (string, error) {
			return "mock", nil
		},
		openFn: func(path string, allowUnsafe bool, cfg *config.Config) error {
			return nil
		},
	}
	return e, &out, &errOut
}

func decodeJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, data)
	}
	return m
}

func jsonResultPaths(t *testing.T, payload map[string]any) []string {
	t.Helper()
	var paths []string
	results, _ := payload["results"].([]any)
	for _, r := range results {
		row := r.(map[string]any)
		paths = append(paths, row["path"].(string))
	}
	return paths
}

// TestLocate_ScopedVsGlobal covers the core scoping requirement: a query that
// matches in two different indexed roots must only surface the in-scope root's
// result when scoped, and both when --global is passed.
func TestLocate_ScopedVsGlobal(t *testing.T) {
	f := newFixture(t)

	e, out, _ := f.testEnv(f.projectDir)
	code := runLocate(e, []string{"--scope=" + f.projectDir, "--json", "postgres"})
	if code != 0 {
		t.Fatalf("scoped locate: expected exit 0, got %d (stderr empty? out=%s)", code, out.String())
	}
	payload := decodeJSON(t, out.Bytes())
	paths := jsonResultPaths(t, payload)
	if len(paths) != 1 || paths[0] != f.infraPath {
		t.Errorf("scoped locate: expected only %s, got %v", f.infraPath, paths)
	}

	e2, out2, _ := f.testEnv(f.projectDir)
	code2 := runLocate(e2, []string{"--global", "--json", "postgres"})
	if code2 != 0 {
		t.Fatalf("global locate: expected exit 0, got %d", code2)
	}
	payload2 := decodeJSON(t, out2.Bytes())
	paths2 := jsonResultPaths(t, payload2)
	seen := map[string]bool{}
	for _, p := range paths2 {
		seen[p] = true
	}
	if !seen[f.infraPath] || !seen[f.otherDocPath] {
		t.Errorf("global locate: expected both %s and %s, got %v", f.infraPath, f.otherDocPath, paths2)
	}
}

// TestLocate_EmptyScopeNamesScopeAndOffersGlobal is plan.md's central UX
// requirement: a zero-result response must name the active scope and offer
// the global retry inline, never implying the file doesn't exist at all.
func TestLocate_EmptyScopeNamesScopeAndOffersGlobal(t *testing.T) {
	f := newFixture(t)

	e, _, errOut := f.testEnv(f.projectDir)
	code := runLocate(e, []string{"--scope=" + f.emptyDir, "postgres"})
	if code == 0 {
		t.Fatalf("expected non-zero exit for empty scope, got 0")
	}
	msg := errOut.String()
	if !strings.Contains(msg, format.DisplayPath(f.emptyDir)) {
		t.Errorf("empty-result message must name the active scope %q; got: %s", f.emptyDir, msg)
	}
	if !strings.Contains(msg, "--global") {
		t.Errorf("empty-result message must offer the --global retry; got: %s", msg)
	}
	if !strings.Contains(msg, "match outside this scope") && !strings.Contains(msg, "chunks") {
		t.Errorf("empty-result message must be honest about matches existing elsewhere; got: %s", msg)
	}
}

// TestExcerpt_JSONHasNoANSI guards against internal/excerpt/render.go's
// hardcoded highlight escape codes leaking into machine-readable output.
func TestExcerpt_JSONHasNoANSI(t *testing.T) {
	f := newFixture(t)

	e, out, _ := f.testEnv(f.projectDir)
	code := runExcerpt(e, []string{"--scope=" + f.projectDir, "--json", "postgres"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if bytes.ContainsRune(out.Bytes(), 0x1b) {
		t.Errorf("--json output contains an ANSI escape byte: %q", out.String())
	}
	payload := decodeJSON(t, out.Bytes())
	results, _ := payload["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("expected at least one excerpt result, got none: %s", out.String())
	}
	text := results[0].(map[string]any)["text"].(string)
	if strings.ContainsRune(text, 0x1b) {
		t.Errorf("excerpt text field contains an ANSI escape: %q", text)
	}
}

// TestExcerpt_NoColorFlag tests default non-TTY ANSI suppression and explicit flag control.
func TestExcerpt_NoColorFlag(t *testing.T) {
	f := newFixture(t)

	// In testEnv, e.stdout is a bytes.Buffer (not a TTY), so NoColor should default to true.
	eDefault, outDefault, _ := f.testEnv(f.projectDir)
	code := runExcerpt(eDefault, []string{"--scope=" + f.projectDir, "postgres"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, outDefault.String())
	}
	if bytes.ContainsRune(outDefault.Bytes(), 0x1b) {
		t.Errorf("non-TTY default should not contain ANSI escape codes, got: %q", outDefault.String())
	}

	// Explicit --no-color=false overrides the non-TTY detection and produces ANSI codes.
	eColor, outColor, _ := f.testEnv(f.projectDir)
	code = runExcerpt(eColor, []string{"--scope=" + f.projectDir, "--no-color=false", "postgres"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, outColor.String())
	}
	if !bytes.ContainsRune(outColor.Bytes(), 0x1b) {
		t.Errorf("--no-color=false should force ANSI highlight codes, got: %q", outColor.String())
	}

	// Explicit --no-color flag suppresses ANSI codes.
	eNoColor, outNoColor, _ := f.testEnv(f.projectDir)
	code = runExcerpt(eNoColor, []string{"--scope=" + f.projectDir, "--no-color", "postgres"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, outNoColor.String())
	}
	if bytes.ContainsRune(outNoColor.Bytes(), 0x1b) {
		t.Errorf("--no-color should suppress ANSI escape codes, got: %q", outNoColor.String())
	}
}

// TestRead_LineRange covers a direct path read restricted to an explicit line
// range, verifying the printed bytes are exactly the requested lines.
func TestRead_LineRange(t *testing.T) {
	f := newFixture(t)

	e, out, _ := f.testEnv(f.projectDir)
	code := runRead(e, []string{"--lines=2-4", f.mainGoPath})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	want := "line two\nline three\nline four\n"
	if out.String() != want {
		t.Errorf("read --lines=2-4: got %q, want %q", out.String(), want)
	}
}

// TestRead_SecretsDenylistRefusal ensures a dotenv file is refused without
// --unsafe, and that its content never reaches stdout or stderr.
func TestRead_SecretsDenylistRefusal(t *testing.T) {
	f := newFixture(t)

	e, out, errOut := f.testEnv(f.projectDir)
	code := runRead(e, []string{f.envPath})
	if code == 0 {
		t.Fatalf("expected refusal (non-zero exit) reading a secrets-denylist path")
	}
	combined := out.String() + errOut.String()
	if strings.Contains(combined, "abcd1234") {
		t.Errorf("secret content leaked into output: %s", combined)
	}
	if !strings.Contains(errOut.String(), "secrets denylist") {
		t.Errorf("refusal message should mention the secrets denylist; got: %s", errOut.String())
	}

	// The same read succeeds once --unsafe is passed as a literal CLI flag.
	e2, out2, _ := f.testEnv(f.projectDir)
	code2 := runRead(e2, []string{"--unsafe", f.envPath})
	if code2 != 0 {
		t.Fatalf("expected --unsafe to permit the read, got exit %d", code2)
	}
	if !strings.Contains(out2.String(), "abcd1234") {
		t.Errorf("expected --unsafe read to return file content, got: %s", out2.String())
	}
}

// TestLocate_MissingManifestGivesReindexGuidance checks that a missing index
// produces the documented remedy rather than a stack trace or empty output.
func TestLocate_MissingManifestGivesReindexGuidance(t *testing.T) {
	f := newFixture(t)
	cfg := f.cfg
	var out, errOut bytes.Buffer
	e := &env{
		cfg:     &cfg,
		dataDir: t.TempDir(), // no manifest.json written here
		cwd:     f.projectDir,
		stdout:  &out,
		stderr:  &errOut,
	}

	code := runLocate(e, []string{"anything"})
	if code == 0 {
		t.Fatalf("expected non-zero exit when the index is missing")
	}
	msg := errOut.String()
	if !strings.Contains(msg, "vektix index") || !strings.Contains(msg, "vektix reindex") {
		t.Errorf("missing-manifest error should point at 'vektix index'/'vektix reindex'; got: %s", msg)
	}
}

// TestStatus_WithIndex tests the human-readable output of vektix status
// when an index exists.
func TestStatus_WithIndex(t *testing.T) {
	f := newFixture(t)
	e, out, _ := f.testEnv(f.projectDir)

	code := runStatus(e, nil)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	str := out.String()
	if !strings.Contains(str, "Vektix Status") {
		t.Errorf("expected header 'Vektix Status', got: %s", str)
	}
	if !strings.Contains(str, "Index Chunks:") || !strings.Contains(str, "2 (2 files)") {
		t.Errorf("expected 2 chunks (2 files), got: %s", str)
	}
	if !strings.Contains(str, "mock-embed") {
		t.Errorf("expected model mock-embed, got: %s", str)
	}
	if !strings.Contains(str, "Active Scope:") {
		t.Errorf("expected Active Scope, got: %s", str)
	}
	if !strings.Contains(str, "Quarantine:     clean") {
		t.Errorf("expected clean quarantine, got: %s", str)
	}
}

// TestStatus_NoIndex tests status output when no index is present.
func TestStatus_NoIndex(t *testing.T) {
	f := newFixture(t)
	cfg := f.cfg
	var out, errOut bytes.Buffer
	e := &env{
		cfg:     &cfg,
		dataDir: t.TempDir(),
		cwd:     f.projectDir,
		stdout:  &out,
		stderr:  &errOut,
	}

	code := runStatus(e, nil)
	if code != 0 {
		t.Fatalf("expected exit 0 for status with no index, got %d", code)
	}
	str := strings.ToLower(out.String())
	if !strings.Contains(str, "no index found") {
		t.Errorf("expected 'no index found', got: %s", out.String())
	}
}

// TestStatus_JSON tests the machine-readable JSON output of vektix status.
func TestStatus_JSON(t *testing.T) {
	f := newFixture(t)
	e, out, _ := f.testEnv(f.projectDir)

	code := runStatus(e, []string{"--json"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	payload := decodeJSON(t, out.Bytes())
	if payload["command"] != "status" {
		t.Errorf("expected command 'status', got %v", payload["command"])
	}
	if payload["has_index"] != true {
		t.Errorf("expected has_index true, got %v", payload["has_index"])
	}
	manifest, ok := payload["manifest"].(map[string]any)
	if !ok {
		t.Fatalf("expected manifest map, got %v", payload["manifest"])
	}
	if manifest["embedding_model"] != "mock-embed" {
		t.Errorf("expected mock-embed, got %v", manifest["embedding_model"])
	}
	if int(manifest["total_files"].(float64)) != 2 {
		t.Errorf("expected 2 total files, got %v", manifest["total_files"])
	}
}

// TestStatus_Quarantine verifies that quarantined files are reported.
func TestStatus_Quarantine(t *testing.T) {
	f := newFixture(t)
	qEntries := []index.QuarantineEntry{
		{Path: "/tmp/corrupt.pdf", Reason: "pdf: stream not present", Time: time.Now()},
	}
	if err := index.SaveQuarantine(index.QuarantinePath(f.dataDir), qEntries); err != nil {
		t.Fatal(err)
	}

	e, out, _ := f.testEnv(f.projectDir)
	code := runStatus(e, nil)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	str := out.String()
	if !strings.Contains(str, "1 quarantined file(s)") {
		t.Errorf("expected quarantine entry in status, got: %s", str)
	}
	if !strings.Contains(str, "corrupt.pdf") {
		t.Errorf("expected corrupt.pdf in status, got: %s", str)
	}
}

// TestStatus_ManifestMismatch tests that a manifest failing validity checks
// surfaces the specific mismatch error instead of "No index found".
func TestStatus_ManifestMismatch(t *testing.T) {
	f := newFixture(t)
	m := &index.Manifest{
		EmbeddingModel: "different-model",
		Dim:            4,
		PrefixScheme:   "v1",
		ChunkerVersion: 1,
		Files:          map[string]index.FileMeta{},
		DirCounts:      map[string]int{},
	}
	if err := m.SaveManifest(index.ManifestPath(f.dataDir)); err != nil {
		t.Fatal(err)
	}

	e, out, _ := f.testEnv(f.projectDir)
	code := runStatus(e, nil)
	if code != 0 {
		t.Fatalf("expected exit 0 for status, got %d", code)
	}
	str := out.String()
	if strings.Contains(str, "No index found") {
		t.Errorf("expected mismatch error instead of 'No index found', got: %s", str)
	}
	if !strings.Contains(str, "different-model") {
		t.Errorf("expected mismatch to mention different-model, got: %s", str)
	}
}

// TestStatus_CorruptQuarantine tests that a corrupt quarantine.json surfaces an error
// instead of silently reporting clean.
func TestStatus_CorruptQuarantine(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(index.QuarantinePath(f.dataDir), []byte("invalid json {{"), 0644); err != nil {
		t.Fatal(err)
	}

	e, out, _ := f.testEnv(f.projectDir)
	code := runStatus(e, nil)
	if code != 0 {
		t.Fatalf("expected exit 0 for status, got %d", code)
	}
	str := out.String()
	if !strings.Contains(str, "error loading quarantine") {
		t.Errorf("expected quarantine loading error, got: %s", str)
	}
}
