package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type trackerData struct {
	Title             string         `yaml:"title"`
	Description       string         `yaml:"description"`
	Items             []trackerItem  `yaml:"items"`
	Dependencies      []dependency   `yaml:"dependencies"`
	RecentlyCompleted []string       `yaml:"recentlyCompleted"`
	NextBatchQueue    []queueItem    `yaml:"nextBatchQueue"`
	SanityCrossRef    []sanityRow    `yaml:"sanityCrossRef"`
	UnmappedFindings  []string       `yaml:"unmappedFindings"`
	Notes             []string       `yaml:"notes"`
	ReferenceDocs     []string       `yaml:"referenceDocs"`
}

type trackerItem struct {
	ID         string `yaml:"id"`
	Type       string `yaml:"type"`
	Priority   string `yaml:"priority"`
	ActionState string `yaml:"actionState"`
	Area       string `yaml:"area"`
	Item       string `yaml:"item"`
	NextAction string `yaml:"nextAction"`
}

type dependency struct {
	ID        string `yaml:"id"`
	DependsOn string `yaml:"dependsOn"`
	Why       string `yaml:"why"`
}

type queueItem struct {
	ID      string `yaml:"id"`
	Summary string `yaml:"summary"`
}

type sanityRow struct {
	Finding string `yaml:"finding"`
	Mapping string `yaml:"mapping"`
	Status  string `yaml:"status"`
}

func main() {
	importMD := flag.String("import-md", "", "existing markdown tracker path to import")
	yamlPath := flag.String("yaml", "docs/feature-tracker.yaml", "yaml tracker path")
	mdPath := flag.String("md", "docs/feature-tracker.md", "markdown tracker path to generate")
	flag.Parse()

	data := trackerData{}
	var err error
	if strings.TrimSpace(*importMD) != "" {
		data, err = importFromMarkdown(*importMD)
		if err != nil {
			panic(err)
		}
	} else {
		data, err = readYAML(*yamlPath)
		if err != nil {
			panic(err)
		}
	}

	if err := writeYAML(*yamlPath, data); err != nil {
		panic(err)
	}
	if err := writeMarkdown(*mdPath, data); err != nil {
		panic(err)
	}

	fmt.Printf("tracker generated: %s -> %s\n", *yamlPath, *mdPath)
}

func readYAML(path string) (trackerData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return trackerData{}, err
	}
	data := trackerData{}
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return trackerData{}, err
	}
	return data, nil
}

func writeYAML(path string, data trackerData) error {
	raw, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func importFromMarkdown(path string) (trackerData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return trackerData{}, err
	}
	lines := splitLines(string(raw))
	data := trackerData{
		Title:       "Feature Tracker",
		Description: "Canonical execution tracker for bugs, features, technical work, and MMO rollout tasks.",
	}

	data.Items = parseActiveTracker(lines)
	data.Dependencies = parseDependencies(lines)
	data.RecentlyCompleted = parseBulletsUnder(lines, "## Recently Completed")
	data.NextBatchQueue = parseNextBatch(lines)
	data.SanityCrossRef = parseSanityCrossRef(lines)
	data.UnmappedFindings = parseUnmapped(lines)
	data.Notes = parseBulletsUnder(lines, "## Notes")
	data.ReferenceDocs = parseReferenceDocs(lines)

	if len(data.Items) == 0 {
		return trackerData{}, errors.New("failed to parse tracker items from markdown")
	}
	return data, nil
}

func splitLines(input string) []string {
	scanner := bufio.NewScanner(strings.NewReader(input))
	lines := make([]string, 0, 512)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

func findHeading(lines []string, heading string) int {
	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			return i
		}
	}
	return -1
}

func parseActiveTracker(lines []string) []trackerItem {
	rows := parseTableUnder(lines, "## Active Tracker")
	items := make([]trackerItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, trackerItem{
			ID:          row["ID"],
			Type:        row["Type"],
			Priority:    row["Priority"],
			ActionState: row["Action State"],
			Area:        row["Area"],
			Item:        row["Item"],
			NextAction:  row["Next Action"],
		})
	}
	return items
}

func parseDependencies(lines []string) []dependency {
	rows := parseTableUnder(lines, "## Dependency Index (Iterable)")
	deps := make([]dependency, 0, len(rows))
	for _, row := range rows {
		deps = append(deps, dependency{
			ID:        row["ID"],
			DependsOn: row["Depends On"],
			Why:       row["Why this dependency exists"],
		})
	}
	return deps
}

func parseSanityCrossRef(lines []string) []sanityRow {
	rows := parseTableUnder(lines, "## Sanity Scan Cross-Reference (2026-03-05)")
	out := make([]sanityRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, sanityRow{
			Finding: row["Sanity Scan Finding"],
			Mapping: row["Tracker Mapping"],
			Status:  row["Coalesced Status"],
		})
	}
	return out
}

func parseBulletsUnder(lines []string, heading string) []string {
	start := findHeading(lines, heading)
	if start == -1 {
		return nil
	}
	items := []string{}
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if strings.HasPrefix(trimmed, "- ") {
			items = append(items, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		}
	}
	return items
}

func parseNextBatch(lines []string) []queueItem {
	start := findHeading(lines, "## Next Batch Queue (Priority Pass)")
	if start == -1 {
		return nil
	}
	re := regexp.MustCompile("^\\d+\\.\\s+(.*)$")
	idRe := regexp.MustCompile("`([^`]+)`")
	out := []queueItem{}
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		match := re.FindStringSubmatch(trimmed)
		if len(match) != 2 {
			continue
		}
		raw := match[1]
		id := ""
		idMatch := idRe.FindStringSubmatch(raw)
		if len(idMatch) == 2 {
			id = idMatch[1]
		}
		out = append(out, queueItem{ID: id, Summary: raw})
	}
	return out
}

func parseUnmapped(lines []string) []string {
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Unmapped findings intentionally left") {
			start = i
			break
		}
	}
	if start == -1 {
		return nil
	}
	out := []string{}
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if strings.HasPrefix(trimmed, "- ") {
			out = append(out, strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "- "), ",")))
		}
	}
	return out
}

func parseReferenceDocs(lines []string) []string {
	start := findHeading(lines, "## Documentation Canon")
	if start == -1 {
		return nil
	}
	out := []string{}
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if strings.HasPrefix(trimmed, "- `docs/") {
			out = append(out, strings.Trim(trimmed[2:], "`"))
		}
	}
	return out
}

func parseTableUnder(lines []string, heading string) []map[string]string {
	start := findHeading(lines, heading)
	if start == -1 {
		return nil
	}
	header := []string{}
	rows := []map[string]string{}
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		parts := splitTableLine(trimmed)
		if len(parts) == 0 {
			continue
		}
		if isDivider(parts) {
			continue
		}
		if len(header) == 0 {
			header = parts
			continue
		}
		if len(parts) < len(header) {
			continue
		}
		row := map[string]string{}
		for col := 0; col < len(header); col++ {
			row[header[col]] = parts[col]
		}
		rows = append(rows, row)
	}
	return rows
}

func splitTableLine(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, strings.TrimSpace(part))
	}
	return out
}

func isDivider(parts []string) bool {
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if strings.Trim(trimmed, "-") != "" {
			return false
		}
	}
	return true
}

func writeMarkdown(path string, data trackerData) error {
	var b strings.Builder
	b.WriteString("# Feature Tracker\n\n")
	b.WriteString("<img src=\"static/logo.png\" alt=\"Lived\" width=\"180\" />\n\n")
	b.WriteString(data.Description + "\n\n")
	b.WriteString("_Generated from docs/feature-tracker.yaml via tools/trackergen. Edit YAML, not this file._\n\n")

	b.WriteString("## Action State Legend\n\n")
	b.WriteString("- `🟨 todo`: not started\n")
	b.WriteString("- `🟦 in-progress`: currently being implemented\n")
	b.WriteString("- `🟥 blocked`: waiting on prerequisite/decision\n")
	b.WriteString("- `🟩 done`: shipped/verified\n\n")

	b.WriteString("## Active Tracker\n\n")
	b.WriteString("| ID | Type | Priority | Action State | Area | Item | Next Action |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, item := range data.Items {
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s |\n",
			escapePipes(item.ID),
			escapePipes(item.Type),
			escapePipes(item.Priority),
			escapePipes(item.ActionState),
			escapePipes(item.Area),
			escapePipes(item.Item),
			escapePipes(item.NextAction),
		))
	}
	b.WriteString("\n")

	b.WriteString("## Dependency Index (Iterable)\n\n")
	b.WriteString("Use comma-separated IDs in `Depends On` for machine-parsable dependency traversal.\n\n")
	b.WriteString("| ID | Depends On | Why this dependency exists |\n")
	b.WriteString("|---|---|---|\n")
	for _, dep := range data.Dependencies {
		b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", escapePipes(dep.ID), escapePipes(dep.DependsOn), escapePipes(dep.Why)))
	}
	b.WriteString("\n")

	b.WriteString("## Dependency Flow\n\n")
	b.WriteString("```mermaid\n")
	b.WriteString(generateDependencyFlow(data))
	b.WriteString("\n```\n\n")

	b.WriteString("## Priority Flow\n\n")
	b.WriteString("```mermaid\n")
	b.WriteString(generatePriorityFlow(data))
	b.WriteString("\n```\n\n")

	b.WriteString("## Recently Completed\n\n")
	for _, line := range data.RecentlyCompleted {
		b.WriteString("- " + line + "\n")
	}
	b.WriteString("\n")

	b.WriteString("## Next Batch Queue (Priority Pass)\n\n")
	for i, item := range data.NextBatchQueue {
		if item.ID != "" {
			b.WriteString(fmt.Sprintf("%d. `%s`: %s\n", i+1, item.ID, item.Summary))
			continue
		}
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, item.Summary))
	}
	b.WriteString("\n")

	b.WriteString("## Sanity Scan Cross-Reference (2026-03-05)\n\n")
	b.WriteString("| Sanity Scan Finding | Tracker Mapping | Coalesced Status |\n")
	b.WriteString("|---|---|---|\n")
	for _, row := range data.SanityCrossRef {
		b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", escapePipes(row.Finding), escapePipes(row.Mapping), escapePipes(row.Status)))
	}
	b.WriteString("\n")

	if len(data.UnmappedFindings) > 0 {
		b.WriteString("Unmapped findings intentionally left for follow-up review before adding new IDs:\n\n")
		for _, finding := range data.UnmappedFindings {
			b.WriteString("- " + finding + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Notes\n\n")
	for _, line := range data.Notes {
		b.WriteString("- " + line + "\n")
	}
	b.WriteString("\n")

	b.WriteString("## Documentation Canon\n\n")
	b.WriteString("- Live execution state (`todo`/`in-progress`/`blocked`/`done`) belongs only in this file.\n")
	if len(data.ReferenceDocs) > 0 {
		b.WriteString("- Point-in-time analysis and design snapshots are reference-only:\n")
		sortedRefs := append([]string(nil), data.ReferenceDocs...)
		sort.Strings(sortedRefs)
		for _, ref := range sortedRefs {
			b.WriteString("\t- `" + ref + "`\n")
		}
	}
	b.WriteString("- When a reference doc includes actionable findings, absorb them into existing tracker IDs first; add new IDs only if no current scope fits.\n")

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func generateDependencyFlow(data trackerData) string {
	stateByID := map[string]string{}
	priorityByID := map[string]string{}
	for _, item := range data.Items {
		stateByID[item.ID] = item.ActionState
		priorityByID[item.ID] = item.Priority
	}

	edges := [][2]string{}
	nodes := map[string]struct{}{}
	for _, dep := range data.Dependencies {
		target := strings.TrimSpace(dep.ID)
		if target == "" {
			continue
		}
		nodes[target] = struct{}{}
		for _, src := range splitCSV(dep.DependsOn) {
			if src == "" {
				continue
			}
			edges = append(edges, [2]string{src, target})
			nodes[src] = struct{}{}
			nodes[target] = struct{}{}
		}
	}

	var b strings.Builder
	b.WriteString("flowchart LR\n")
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		label := fmt.Sprintf("%s %s", priorityIcon(priorityByID[id]), id)
		b.WriteString(fmt.Sprintf("\t%s[%q]\n", nodeID(id), label))
	}
	for _, edge := range edges {
		b.WriteString(fmt.Sprintf("\t%s --> %s\n", nodeID(edge[0]), nodeID(edge[1])))
	}

	b.WriteString("\n\tclassDef done fill:#173b2f,stroke:#4dd6a7,stroke-width:2px,color:#e9fff7;\n")
	b.WriteString("\tclassDef inProgress fill:#132f4d,stroke:#68b5ff,stroke-width:2px,color:#ecf6ff;\n")
	b.WriteString("\tclassDef todo fill:#3b3212,stroke:#f0d46a,stroke-width:2px,color:#fff8de;\n")
	b.WriteString("\tclassDef blocked fill:#4a1f29,stroke:#ff8fa6,stroke-width:2px,color:#ffe9ee;\n\n")

	for _, id := range ids {
		klass := stateClass(stateByID[id])
		if klass == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("\tclass %s %s;\n", nodeID(id), klass))
	}
	return b.String()
}

func generatePriorityFlow(data trackerData) string {
	openByPriority := map[string][]string{"P0": {}, "P1": {}, "P2": {}}
	for _, item := range data.Items {
		state := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(item.ActionState, "🟨")))
		state = strings.TrimSpace(strings.TrimPrefix(state, "🟦"))
		state = strings.TrimSpace(strings.TrimPrefix(state, "🟥"))
		state = strings.TrimSpace(strings.TrimPrefix(state, "🟩"))
		if strings.Contains(strings.ToLower(item.ActionState), "done") {
			continue
		}
		if _, ok := openByPriority[item.Priority]; !ok {
			continue
		}
		openByPriority[item.Priority] = append(openByPriority[item.Priority], item.ID)
		_ = state
	}
	for key := range openByPriority {
		sort.Strings(openByPriority[key])
	}

	var b strings.Builder
	b.WriteString("flowchart TB\n")
	b.WriteString("\tP0Q[\"🔴 P0 Queue\"] --> P1Q[\"🟠 P1 Queue\"] --> P2Q[\"🟡 P2 Queue\"]\n")
	for _, id := range openByPriority["P0"] {
		b.WriteString(fmt.Sprintf("\tP0Q --> %s[%q]\n", nodeID(id), id))
	}
	for _, id := range openByPriority["P1"] {
		b.WriteString(fmt.Sprintf("\tP1Q --> %s[%q]\n", nodeID(id), id))
	}
	for _, id := range openByPriority["P2"] {
		b.WriteString(fmt.Sprintf("\tP2Q --> %s[%q]\n", nodeID(id), id))
	}
	return b.String()
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func nodeID(id string) string {
	replacer := strings.NewReplacer("-", "_", ":", "_", "/", "_")
	return replacer.Replace(id)
}

func priorityIcon(priority string) string {
	switch strings.TrimSpace(priority) {
	case "P0":
		return "🔴"
	case "P1":
		return "🟠"
	case "P2":
		return "🟡"
	default:
		return "⚪"
	}
}

func stateClass(state string) string {
	s := strings.ToLower(state)
	switch {
	case strings.Contains(s, "done"):
		return "done"
	case strings.Contains(s, "in-progress"):
		return "inProgress"
	case strings.Contains(s, "blocked"):
		return "blocked"
	case strings.Contains(s, "todo"):
		return "todo"
	default:
		return ""
	}
}

func escapePipes(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}
