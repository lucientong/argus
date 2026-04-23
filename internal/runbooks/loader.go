// Package runbooks provides runbook loading and the RAG pipeline setup.
package runbooks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lucientong/waggle/pkg/rag"
)

// LoadedRunbook holds one markdown runbook loaded from disk.
type LoadedRunbook struct {
	Title   string // derived from filename
	Path    string
	Content string
}

// LoadDir reads all *.md files from dir and returns them as LoadedRunbooks.
func LoadDir(dir string) ([]LoadedRunbook, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("runbooks: read dir %s: %w", dir, err)
	}
	var books []LoadedRunbook
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("runbooks: read %s: %w", path, err)
		}
		title := filenameToTitle(e.Name())
		books = append(books, LoadedRunbook{
			Title:   title,
			Path:    path,
			Content: string(data),
		})
	}
	return books, nil
}

// IngestAll embeds all runbooks into the provided vector store.
// Returns the number of documents ingested.
func IngestAll(ctx context.Context, books []LoadedRunbook, embedder rag.Embedder, store rag.VectorStore) (int, error) {
	splitter := rag.NewTokenSplitter(400, 50)
	total := 0
	for _, b := range books {
		id := filepath.Base(b.Path)
		if err := rag.Ingest(ctx, b.Content, id, embedder, store, splitter); err != nil {
			return total, fmt.Errorf("runbooks: ingest %s: %w", b.Path, err)
		}
		// Count chunks by splitting here — we just need an approximate count for logging.
		total++
	}
	return total, nil
}

// filenameToTitle converts "high-cpu.md" → "High Cpu"
func filenameToTitle(name string) string {
	base := strings.TrimSuffix(name, ".md")
	parts := strings.Split(base, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
