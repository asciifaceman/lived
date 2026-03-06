package gamedata

import (
	"testing"
)

func TestEmbeddedInfoIncludesRequiredFiles(t *testing.T) {
	info, err := Info()
	if err != nil {
		t.Fatalf("expected embedded info, got error: %v", err)
	}

	if info.ManifestVersion <= 0 {
		t.Fatalf("expected positive manifest version, got %d", info.ManifestVersion)
	}
	if info.FilesHash == "" {
		t.Fatal("expected non-empty aggregate files hash")
	}

	for _, fileName := range requiredFiles {
		entry, ok := info.Files[fileName]
		if !ok {
			t.Fatalf("missing required embedded file %s", fileName)
		}
		if entry.Version <= 0 {
			t.Fatalf("expected positive file version for %s, got %d", fileName, entry.Version)
		}
		if entry.Size <= 0 {
			t.Fatalf("expected positive file size for %s, got %d", fileName, entry.Size)
		}
		if entry.SHA256 == "" {
			t.Fatalf("expected sha for %s", fileName)
		}
	}
}

func TestEmbeddedReadFileReturnsData(t *testing.T) {
	content, err := ReadFile("ascension.yaml")
	if err != nil {
		t.Fatalf("expected ascension data, got error: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected non-empty file content")
	}

	if _, err := ReadFile("missing.yaml"); err == nil {
		t.Fatal("expected error for unknown file")
	}
}

func TestContentContainsAllRequiredDocuments(t *testing.T) {
	content, err := Content()
	if err != nil {
		t.Fatalf("expected generated content, got error: %v", err)
	}

	if len(content.Documents) == 0 {
		t.Fatal("expected non-empty generated documents")
	}

	for _, fileName := range requiredFiles {
		if _, ok := content.Documents[fileName]; !ok {
			t.Fatalf("missing document for %s", fileName)
		}
	}
}
