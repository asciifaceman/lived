//go:generate go run ../../tools/contentgen -out zz_generated_game_data.go

package gamedata

import (
	"encoding/json"
	"fmt"
	"maps"
	"sync"
)

var requiredFiles = []string{
	"ascension.yaml",
	"behaviors.yaml",
	"items.yaml",
	"player-behaviors.yaml",
	"player-stats.yaml",
	"upgrades.yaml",
	"world-behaviors.yaml",
}

type FileInfo struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Size    int    `json:"size"`
	SHA256  string `json:"sha256"`
}

type BuildInfo struct {
	ManifestVersion int                 `json:"manifestVersion"`
	FilesHash       string              `json:"filesHash"`
	Files           map[string]FileInfo `json:"files"`
}

// GameData is a dynamic content view keyed by source filename.
// Values are decoded document objects from docs/game-data/*.yaml.
type GameData struct {
	Documents map[string]any `json:"documents"`
}

var (
	contentLoadOnce sync.Once
	loadedContent   GameData
	contentLoadErr  error
)

func Info() (BuildInfo, error) {
	if err := requireGeneratedArtifacts(); err != nil {
		return BuildInfo{}, err
	}

	copied := generatedBuildInfo
	copied.Files = maps.Clone(generatedBuildInfo.Files)
	return copied, nil
}

func ReadFile(name string) ([]byte, error) {
	if err := requireGeneratedArtifacts(); err != nil {
		return nil, err
	}

	content, ok := generatedRawFiles[normalizeFileName(name)]
	if !ok {
		return nil, fmt.Errorf("unknown embedded game-data file: %s", name)
	}

	copied := []byte(content)
	return copied, nil
}

func Content() (GameData, error) {
	if err := requireGeneratedArtifacts(); err != nil {
		return GameData{}, err
	}

	contentLoadOnce.Do(func() {
		decoded := GameData{}
		if err := json.Unmarshal([]byte(generatedGameDataJSON), &decoded); err != nil {
			contentLoadErr = fmt.Errorf("decode generated game-data JSON: %w", err)
			return
		}
		loadedContent = decoded
	})

	if contentLoadErr != nil {
		return GameData{}, contentLoadErr
	}

	cloned := GameData{Documents: maps.Clone(loadedContent.Documents)}
	return cloned, nil
}

func requireGeneratedArtifacts() error {
	if generatedBuildInfo.ManifestVersion <= 0 {
		return fmt.Errorf("generated game-data artifacts missing or invalid; run `go run ./tools/contentgen -out pkg/gamedata/zz_generated_game_data.go`")
	}
	if len(generatedRawFiles) == 0 {
		return fmt.Errorf("generated game-data file payloads missing; run `go run ./tools/contentgen -out pkg/gamedata/zz_generated_game_data.go`")
	}
	return nil
}

func normalizeFileName(name string) string {
	normalized := name
	if len(normalized) >= len("docs/game-data/") && normalized[:len("docs/game-data/")] == "docs/game-data/" {
		normalized = normalized[len("docs/game-data/"):]
	}
	if len(normalized) >= 2 && normalized[:2] == "./" {
		normalized = normalized[2:]
	}
	return normalized
}
