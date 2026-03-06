package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type fileInfo struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Size    int    `json:"size"`
	SHA256  string `json:"sha256"`
}

type buildInfo struct {
	ManifestVersion int                 `json:"manifestVersion"`
	FilesHash       string              `json:"filesHash"`
	Files           map[string]fileInfo `json:"files"`
}

var requiredFiles = []string{
	"ascension.yaml",
	"behaviors.yaml",
	"items.yaml",
	"player-behaviors.yaml",
	"player-stats.yaml",
	"upgrades.yaml",
	"world-behaviors.yaml",
}

type behaviorFile struct {
	Version   int           `yaml:"version"`
	Behaviors []behaviorDoc `yaml:"behaviors"`
}

type behaviorDoc struct {
	Key                   string             `yaml:"key"`
	Name                  string             `yaml:"name"`
	Summary               string             `yaml:"summary"`
	ActorType             string             `yaml:"actorType"`
	ExclusiveGroup        string             `yaml:"exclusiveGroup"`
	ScheduleModes         []string           `yaml:"scheduleModes"`
	SingleUsePerAscension bool               `yaml:"singleUsePerAscension"`
	DurationMinutes       int64              `yaml:"durationMinutes"`
	StaminaCost           int64              `yaml:"staminaCost"`
	Requirements          requirementDoc     `yaml:"requirements"`
	Costs                 map[string]int64   `yaml:"costs"`
	Outputs               map[string]int64   `yaml:"outputs"`
	OutputExpressions     map[string]string  `yaml:"outputExpressions"`
	OutputChances         map[string]float64 `yaml:"outputChances"`
	StatDeltas            map[string]int64   `yaml:"statDeltas"`
	RequiresMarketOpen    bool               `yaml:"requiresMarketOpen"`
	RequiresNight         bool               `yaml:"requiresNight"`
	GrantsUnlocks         []string           `yaml:"grantsUnlocks"`
	MarketEffects         map[string]int64   `yaml:"marketEffects"`
	StartMessage          string             `yaml:"startMessage"`
	CompleteMessage       string             `yaml:"completeMessage"`
	RepeatIntervalMin     int64              `yaml:"repeatIntervalMin"`
}

type upgradeFile struct {
	Version  int          `yaml:"version"`
	Upgrades []upgradeDoc `yaml:"upgrades"`
}

type upgradeDoc struct {
	Key           string           `yaml:"key"`
	Name          string           `yaml:"name"`
	Summary       string           `yaml:"summary"`
	Category      string           `yaml:"category"`
	GateTypes     []string         `yaml:"gateTypes"`
	MaxPurchases  int64            `yaml:"maxPurchases"`
	CostScaling   float64          `yaml:"costScaling"`
	OutputScaling float64          `yaml:"outputScaling"`
	Requirements  requirementDoc   `yaml:"requirements"`
	Costs         map[string]int64 `yaml:"costs"`
	Outputs       upgradeOutputDoc `yaml:"outputs"`
}

type upgradeOutputDoc struct {
	QueueSlotsDelta int64            `yaml:"queueSlotsDelta"`
	Unlocks         []string         `yaml:"unlocks"`
	Items           map[string]int64 `yaml:"items"`
	StatDeltas      map[string]int64 `yaml:"statDeltas"`
}

type requirementDoc struct {
	Unlocks []string         `yaml:"unlocks"`
	Items   map[string]int64 `yaml:"items"`
}

func main() {
	gameDataOutputPath := flag.String("out", "pkg/gamedata/zz_generated_game_data.go", "output go file path for pkg/gamedata generated artifact")
	gameplayOutputPath := flag.String("out-gameplay", "src/gameplay/zz_generated_behavior_definitions.go", "output go file path for src/gameplay generated behavior definitions")
	gameplayUpgradeOutputPath := flag.String("out-gameplay-upgrades", "src/gameplay/zz_generated_upgrade_definitions.go", "output go file path for src/gameplay generated upgrade definitions")
	flag.Parse()

	if err := run(*gameDataOutputPath, *gameplayOutputPath, *gameplayUpgradeOutputPath); err != nil {
		fmt.Fprintf(os.Stderr, "contentgen failed: %v\n", err)
		os.Exit(1)
	}
}

func run(gameDataOutputPath, gameplayOutputPath, gameplayUpgradeOutputPath string) error {
	files := append([]string(nil), requiredFiles...)
	sort.Strings(files)

	build := buildInfo{Files: make(map[string]fileInfo, len(files))}
	rawByFile := make(map[string]string, len(files))
	documents := make(map[string]any, len(files))
	hasher := sha256.New()

	for _, name := range files {
		fullPath := filepath.Join("docs", "game-data", name)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", fullPath, err)
		}

		doc := map[string]any{}
		if err := yaml.Unmarshal(content, &doc); err != nil {
			return fmt.Errorf("yaml parse %s: %w", fullPath, err)
		}

		version, err := parseVersion(doc)
		if err != nil {
			return fmt.Errorf("version parse %s: %w", fullPath, err)
		}
		if build.ManifestVersion == 0 {
			build.ManifestVersion = version
		} else if build.ManifestVersion != version {
			return fmt.Errorf("manifest version mismatch in %s: got %d expected %d", fullPath, version, build.ManifestVersion)
		}

		digest := sha256.Sum256(content)
		hash := hex.EncodeToString(digest[:])
		build.Files[name] = fileInfo{
			Name:    name,
			Version: version,
			Size:    len(content),
			SHA256:  hash,
		}

		rawByFile[name] = string(content)
		documents[name] = doc

		_, _ = hasher.Write([]byte(name))
		_, _ = hasher.Write([]byte{':'})
		_, _ = hasher.Write([]byte(hash))
		_, _ = hasher.Write([]byte{'\n'})
	}
	if build.ManifestVersion <= 0 {
		return fmt.Errorf("manifest version must be positive")
	}
	build.FilesHash = hex.EncodeToString(hasher.Sum(nil))

	gameDataPayload := map[string]any{"documents": documents}
	gameDataJSON, err := json.Marshal(gameDataPayload)
	if err != nil {
		return fmt.Errorf("marshal game data payload: %w", err)
	}

	gameDataFileBytes, err := renderGameDataGeneratedFile(build, rawByFile, string(gameDataJSON))
	if err != nil {
		return err
	}

	if err := os.WriteFile(gameDataOutputPath, gameDataFileBytes, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", gameDataOutputPath, err)
	}

	playerBehaviors, err := loadBehaviorFile(filepath.Join("docs", "game-data", "player-behaviors.yaml"), "player")
	if err != nil {
		return err
	}
	worldBehaviors, err := loadBehaviorFile(filepath.Join("docs", "game-data", "world-behaviors.yaml"), "world")
	if err != nil {
		return err
	}

	gameplayFileBytes, err := renderGameplayGeneratedFile(playerBehaviors, worldBehaviors)
	if err != nil {
		return err
	}

	if err := os.WriteFile(gameplayOutputPath, gameplayFileBytes, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", gameplayOutputPath, err)
	}

	upgrades, err := loadUpgradeFile(filepath.Join("docs", "game-data", "upgrades.yaml"))
	if err != nil {
		return err
	}

	upgradeFileBytes, err := renderUpgradeGeneratedFile(upgrades)
	if err != nil {
		return err
	}

	if err := os.WriteFile(gameplayUpgradeOutputPath, upgradeFileBytes, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", gameplayUpgradeOutputPath, err)
	}

	fmt.Printf("content generated: %s\n", gameDataOutputPath)
	fmt.Printf("content generated: %s\n", gameplayOutputPath)
	fmt.Printf("content generated: %s\n", gameplayUpgradeOutputPath)
	return nil
}

func loadBehaviorFile(path string, expectedActorType string) ([]behaviorDoc, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	payload := behaviorFile{}
	if err := yaml.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("yaml parse %s: %w", path, err)
	}
	if payload.Version <= 0 {
		return nil, fmt.Errorf("invalid version in %s", path)
	}

	for _, behavior := range payload.Behaviors {
		if strings.TrimSpace(behavior.Key) == "" {
			return nil, fmt.Errorf("behavior missing key in %s", path)
		}
		if strings.TrimSpace(behavior.ActorType) != expectedActorType {
			return nil, fmt.Errorf("behavior %s in %s has actorType=%q, expected %q", behavior.Key, path, behavior.ActorType, expectedActorType)
		}
		if behavior.DurationMinutes <= 0 {
			return nil, fmt.Errorf("behavior %s in %s must have positive durationMinutes", behavior.Key, path)
		}
	}

	return payload.Behaviors, nil
}

func loadUpgradeFile(path string) ([]upgradeDoc, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	payload := upgradeFile{}
	if err := yaml.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("yaml parse %s: %w", path, err)
	}
	if payload.Version <= 0 {
		return nil, fmt.Errorf("invalid version in %s", path)
	}

	for _, upgrade := range payload.Upgrades {
		if strings.TrimSpace(upgrade.Key) == "" {
			return nil, fmt.Errorf("upgrade missing key in %s", path)
		}
		if upgrade.CostScaling <= 0 {
			upgrade.CostScaling = 1
		}
		if upgrade.OutputScaling <= 0 {
			upgrade.OutputScaling = 1
		}
	}

	return payload.Upgrades, nil
}

func parseVersion(doc map[string]any) (int, error) {
	raw, ok := doc["version"]
	if !ok {
		return 0, fmt.Errorf("missing version")
	}

	switch typed := raw.(type) {
	case int:
		if typed <= 0 {
			return 0, fmt.Errorf("non-positive version %d", typed)
		}
		return typed, nil
	case int64:
		if typed <= 0 {
			return 0, fmt.Errorf("non-positive version %d", typed)
		}
		return int(typed), nil
	case float64:
		value := int(typed)
		if float64(value) != typed {
			return 0, fmt.Errorf("non-integer version %v", typed)
		}
		if value <= 0 {
			return 0, fmt.Errorf("non-positive version %d", value)
		}
		return value, nil
	case string:
		value, err := strconv.Atoi(typed)
		if err != nil {
			return 0, fmt.Errorf("invalid version %q", typed)
		}
		if value <= 0 {
			return 0, fmt.Errorf("non-positive version %d", value)
		}
		return value, nil
	default:
		return 0, fmt.Errorf("unsupported version type %T", raw)
	}
}

func renderGameDataGeneratedFile(build buildInfo, rawByFile map[string]string, gameDataJSON string) ([]byte, error) {
	files := sortedKeys(rawByFile)
	fileMetaKeys := sortedFileInfoKeys(build.Files)

	var b bytes.Buffer
	b.WriteString("// Code generated by tools/contentgen; DO NOT EDIT.\n")
	b.WriteString("\n")
	b.WriteString("package gamedata\n")
	b.WriteString("\n")
	b.WriteString("var generatedBuildInfo = BuildInfo{\n")
	b.WriteString(fmt.Sprintf("\tManifestVersion: %d,\n", build.ManifestVersion))
	b.WriteString(fmt.Sprintf("\tFilesHash: %s,\n", strconv.Quote(build.FilesHash)))
	b.WriteString("\tFiles: map[string]FileInfo{\n")
	for _, key := range fileMetaKeys {
		entry := build.Files[key]
		b.WriteString(fmt.Sprintf("\t\t%s: {Name: %s, Version: %d, Size: %d, SHA256: %s},\n",
			strconv.Quote(key), strconv.Quote(entry.Name), entry.Version, entry.Size, strconv.Quote(entry.SHA256)))
	}
	b.WriteString("\t},\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("var generatedRawFiles = map[string]string{\n")
	for _, key := range files {
		b.WriteString(fmt.Sprintf("\t%s: %s,\n", strconv.Quote(key), strconv.Quote(rawByFile[key])))
	}
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("var generatedGameDataJSON = %s\n", strconv.Quote(gameDataJSON)))

	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated file: %w", err)
	}
	return formatted, nil
}

func renderGameplayGeneratedFile(playerBehaviors, worldBehaviors []behaviorDoc) ([]byte, error) {
	playerByKey := map[string]behaviorDoc{}
	for _, behavior := range playerBehaviors {
		if _, exists := playerByKey[behavior.Key]; exists {
			return nil, fmt.Errorf("duplicate player behavior key %s", behavior.Key)
		}
		playerByKey[behavior.Key] = behavior
	}

	worldByKey := map[string]behaviorDoc{}
	for _, behavior := range worldBehaviors {
		if _, exists := worldByKey[behavior.Key]; exists {
			return nil, fmt.Errorf("duplicate world behavior key %s", behavior.Key)
		}
		worldByKey[behavior.Key] = behavior
	}

	playerKeys := sortedBehaviorKeys(playerByKey)
	worldKeys := sortedBehaviorKeys(worldByKey)

	var b bytes.Buffer
	b.WriteString("// Code generated by tools/contentgen; DO NOT EDIT.\n")
	b.WriteString("\n")
	b.WriteString("package gameplay\n")
	b.WriteString("\n")
	b.WriteString("var playerBehaviorDefinitions = map[string]BehaviorDefinition{\n")
	for _, key := range playerKeys {
		behavior := playerByKey[key]
		b.WriteString(fmt.Sprintf("\t%q: %s,\n", key, renderBehaviorDefinitionLiteral(behavior)))
	}
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("var worldBehaviorDefinitions = map[string]BehaviorDefinition{\n")
	for _, key := range worldKeys {
		behavior := worldByKey[key]
		b.WriteString(fmt.Sprintf("\t%q: %s,\n", key, renderBehaviorDefinitionLiteral(behavior)))
	}
	b.WriteString("}\n")

	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated gameplay definitions: %w", err)
	}
	return formatted, nil
}

func renderBehaviorDefinitionLiteral(behavior behaviorDoc) string {
	parts := make([]string, 0, 24)
	parts = append(parts, fmt.Sprintf("Key: %q", behavior.Key))
	if behavior.Name != "" {
		parts = append(parts, fmt.Sprintf("Name: %q", behavior.Name))
	}
	if behavior.Summary != "" {
		parts = append(parts, fmt.Sprintf("Summary: %q", behavior.Summary))
	}
	parts = append(parts, fmt.Sprintf("ActorType: %s", actorTypeConstant(behavior.ActorType)))
	if behavior.ExclusiveGroup != "" {
		parts = append(parts, fmt.Sprintf("ExclusiveGroup: %q", behavior.ExclusiveGroup))
	}
	if len(behavior.ScheduleModes) > 0 {
		parts = append(parts, fmt.Sprintf("ScheduleModes: %s", renderStringSliceLiteral(behavior.ScheduleModes)))
	}
	if behavior.SingleUsePerAscension {
		parts = append(parts, "SingleUsePerAscension: true")
	}
	parts = append(parts, fmt.Sprintf("DurationMinutes: %d", behavior.DurationMinutes))
	if behavior.StaminaCost != 0 {
		parts = append(parts, fmt.Sprintf("StaminaCost: %d", behavior.StaminaCost))
	}
	if len(behavior.Requirements.Unlocks) > 0 || len(behavior.Requirements.Items) > 0 {
		parts = append(parts, fmt.Sprintf("Requirements: %s", renderRequirementLiteral(behavior.Requirements)))
	}
	if len(behavior.Costs) > 0 {
		parts = append(parts, fmt.Sprintf("Costs: %s", renderMapStringInt64Literal(behavior.Costs)))
	}
	if len(behavior.Outputs) > 0 {
		parts = append(parts, fmt.Sprintf("Outputs: %s", renderMapStringInt64Literal(behavior.Outputs)))
	}
	if len(behavior.OutputExpressions) > 0 {
		parts = append(parts, fmt.Sprintf("OutputExpressions: %s", renderMapStringStringLiteral(behavior.OutputExpressions)))
	}
	if len(behavior.OutputChances) > 0 {
		parts = append(parts, fmt.Sprintf("OutputChances: %s", renderMapStringFloat64Literal(behavior.OutputChances)))
	}
	if len(behavior.StatDeltas) > 0 {
		parts = append(parts, fmt.Sprintf("StatDeltas: %s", renderMapStringInt64Literal(behavior.StatDeltas)))
	}
	if behavior.RequiresMarketOpen {
		parts = append(parts, "RequiresMarketOpen: true")
	}
	if behavior.RequiresNight {
		parts = append(parts, "RequiresNight: true")
	}
	if len(behavior.GrantsUnlocks) > 0 {
		parts = append(parts, fmt.Sprintf("GrantsUnlocks: %s", renderStringSliceLiteral(behavior.GrantsUnlocks)))
	}
	if len(behavior.MarketEffects) > 0 {
		parts = append(parts, fmt.Sprintf("MarketEffects: %s", renderMapStringInt64Literal(behavior.MarketEffects)))
	}
	if behavior.StartMessage != "" {
		parts = append(parts, fmt.Sprintf("StartMessage: %q", behavior.StartMessage))
	}
	if behavior.CompleteMessage != "" {
		parts = append(parts, fmt.Sprintf("CompleteMessage: %q", behavior.CompleteMessage))
	}
	if behavior.RepeatIntervalMin > 0 {
		parts = append(parts, fmt.Sprintf("RepeatIntervalMin: %d", behavior.RepeatIntervalMin))
	}

	return fmt.Sprintf("BehaviorDefinition{%s}", strings.Join(parts, ", "))
}

func renderUpgradeGeneratedFile(upgrades []upgradeDoc) ([]byte, error) {
	upgradeByKey := map[string]upgradeDoc{}
	for _, upgrade := range upgrades {
		if _, exists := upgradeByKey[upgrade.Key]; exists {
			return nil, fmt.Errorf("duplicate upgrade key %s", upgrade.Key)
		}
		upgradeByKey[upgrade.Key] = upgrade
	}

	upgradeKeys := sortedUpgradeKeys(upgradeByKey)

	var b bytes.Buffer
	b.WriteString("// Code generated by tools/contentgen; DO NOT EDIT.\n")
	b.WriteString("\n")
	b.WriteString("package gameplay\n")
	b.WriteString("\n")
	b.WriteString("var upgradeDefinitions = map[string]UpgradeDefinition{\n")
	for _, key := range upgradeKeys {
		upgrade := upgradeByKey[key]
		b.WriteString(fmt.Sprintf("\t%q: %s,\n", key, renderUpgradeDefinitionLiteral(upgrade)))
	}
	b.WriteString("}\n")

	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated gameplay upgrade definitions: %w", err)
	}
	return formatted, nil
}

func renderUpgradeDefinitionLiteral(upgrade upgradeDoc) string {
	parts := make([]string, 0, 16)
	parts = append(parts, fmt.Sprintf("Key: %q", upgrade.Key))
	if upgrade.Name != "" {
		parts = append(parts, fmt.Sprintf("Name: %q", upgrade.Name))
	}
	if upgrade.Summary != "" {
		parts = append(parts, fmt.Sprintf("Summary: %q", upgrade.Summary))
	}
	if upgrade.Category != "" {
		parts = append(parts, fmt.Sprintf("Category: %q", upgrade.Category))
	}
	if len(upgrade.GateTypes) > 0 {
		parts = append(parts, fmt.Sprintf("GateTypes: %s", renderStringSliceLiteral(upgrade.GateTypes)))
	}
	if upgrade.MaxPurchases > 0 {
		parts = append(parts, fmt.Sprintf("MaxPurchases: %d", upgrade.MaxPurchases))
	}
	if upgrade.CostScaling > 0 && upgrade.CostScaling != 1 {
		parts = append(parts, fmt.Sprintf("CostScaling: %s", strconv.FormatFloat(upgrade.CostScaling, 'f', -1, 64)))
	}
	if upgrade.OutputScaling > 0 && upgrade.OutputScaling != 1 {
		parts = append(parts, fmt.Sprintf("OutputScaling: %s", strconv.FormatFloat(upgrade.OutputScaling, 'f', -1, 64)))
	}
	if len(upgrade.Requirements.Unlocks) > 0 || len(upgrade.Requirements.Items) > 0 {
		parts = append(parts, fmt.Sprintf("Requirements: %s", renderRequirementLiteral(upgrade.Requirements)))
	}
	if len(upgrade.Costs) > 0 {
		parts = append(parts, fmt.Sprintf("Costs: %s", renderMapStringInt64Literal(upgrade.Costs)))
	}

	outputParts := make([]string, 0, 4)
	if upgrade.Outputs.QueueSlotsDelta != 0 {
		outputParts = append(outputParts, fmt.Sprintf("QueueSlotsDelta: %d", upgrade.Outputs.QueueSlotsDelta))
	}
	if len(upgrade.Outputs.Unlocks) > 0 {
		outputParts = append(outputParts, fmt.Sprintf("Unlocks: %s", renderStringSliceLiteral(upgrade.Outputs.Unlocks)))
	}
	if len(upgrade.Outputs.Items) > 0 {
		outputParts = append(outputParts, fmt.Sprintf("Items: %s", renderMapStringInt64Literal(upgrade.Outputs.Items)))
	}
	if len(upgrade.Outputs.StatDeltas) > 0 {
		outputParts = append(outputParts, fmt.Sprintf("StatDeltas: %s", renderMapStringInt64Literal(upgrade.Outputs.StatDeltas)))
	}
	if len(outputParts) > 0 {
		parts = append(parts, fmt.Sprintf("Outputs: UpgradeOutputDefinition{%s}", strings.Join(outputParts, ", ")))
	}

	return fmt.Sprintf("UpgradeDefinition{%s}", strings.Join(parts, ", "))
}

func renderRequirementLiteral(requirement requirementDoc) string {
	parts := make([]string, 0, 2)
	if len(requirement.Unlocks) > 0 {
		parts = append(parts, fmt.Sprintf("Unlocks: %s", renderStringSliceLiteral(requirement.Unlocks)))
	}
	if len(requirement.Items) > 0 {
		parts = append(parts, fmt.Sprintf("Items: %s", renderMapStringInt64Literal(requirement.Items)))
	}
	return fmt.Sprintf("Requirement{%s}", strings.Join(parts, ", "))
}

func renderStringSliceLiteral(values []string) string {
	if len(values) == 0 {
		return "nil"
	}
	ordered := append([]string(nil), values...)
	sort.Strings(ordered)
	quoted := make([]string, 0, len(ordered))
	for _, value := range ordered {
		quoted = append(quoted, strconv.Quote(value))
	}
	return fmt.Sprintf("[]string{%s}", strings.Join(quoted, ", "))
}

func renderMapStringInt64Literal(values map[string]int64) string {
	keys := sortedStringKeys(values)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%q: %d", key, values[key]))
	}
	return fmt.Sprintf("map[string]int64{%s}", strings.Join(parts, ", "))
}

func renderMapStringStringLiteral(values map[string]string) string {
	keys := sortedStringKeys(values)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%q: %q", key, values[key]))
	}
	return fmt.Sprintf("map[string]string{%s}", strings.Join(parts, ", "))
}

func renderMapStringFloat64Literal(values map[string]float64) string {
	keys := sortedStringKeys(values)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%q: %s", key, strconv.FormatFloat(values[key], 'f', -1, 64)))
	}
	return fmt.Sprintf("map[string]float64{%s}", strings.Join(parts, ", "))
}

func sortedStringKeys[V any](input map[string]V) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedBehaviorKeys(input map[string]behaviorDoc) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedUpgradeKeys(input map[string]upgradeDoc) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func actorTypeConstant(value string) string {
	switch strings.TrimSpace(value) {
	case "player":
		return "ActorPlayer"
	case "world":
		return "ActorWorld"
	default:
		return strconv.Quote(value)
	}
}

func sortedKeys(input map[string]string) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedFileInfoKeys(input map[string]fileInfo) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
