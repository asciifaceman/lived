package migrations

import (
	"context"
	"fmt"
	"strings"

	"github.com/asciifaceman/lived/pkg/dal"
	"gorm.io/gorm"
)

type VerificationReport struct {
	MissingIndexes  []string         `json:"missingIndexes"`
	DuplicateGroups map[string]int64 `json:"duplicateGroups"`
	InvalidRealmIDs map[string]int64 `json:"invalidRealmIds"`
}

func (r VerificationReport) IsHealthy() bool {
	if len(r.MissingIndexes) > 0 {
		return false
	}
	for _, count := range r.DuplicateGroups {
		if count > 0 {
			return false
		}
	}
	for _, count := range r.InvalidRealmIDs {
		if count > 0 {
			return false
		}
	}
	return true
}

func (r VerificationReport) Summary() string {
	missing := len(r.MissingIndexes)
	duplicateProblems := 0
	for _, count := range r.DuplicateGroups {
		if count > 0 {
			duplicateProblems++
		}
	}
	invalidRealmProblems := 0
	for _, count := range r.InvalidRealmIDs {
		if count > 0 {
			invalidRealmProblems++
		}
	}

	return fmt.Sprintf("missingIndexes=%d duplicateGroupsWithConflicts=%d invalidRealmTables=%d", missing, duplicateProblems, invalidRealmProblems)
}

func Run(ctx context.Context, database *gorm.DB) error {
	if err := database.WithContext(ctx).AutoMigrate(dal.Models()...); err != nil {
		return err
	}

	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := backfillRealmIDs(tx); err != nil {
			return err
		}
		if err := normalizeRealmScopedDuplicates(tx); err != nil {
			return err
		}
		if err := dropLegacySingleColumnUniqueIndexes(tx); err != nil {
			return err
		}
		if err := ensureRealmScopedUniqueIndexes(tx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	report, err := VerifyRealmScoping(ctx, database)
	if err != nil {
		return err
	}
	if !report.IsHealthy() {
		return fmt.Errorf("realm-scoping migration verification failed: %s", report.Summary())
	}

	return nil
}

func VerifyRealmScoping(ctx context.Context, database *gorm.DB) (VerificationReport, error) {
	report := VerificationReport{
		MissingIndexes: make([]string, 0),
		DuplicateGroups: map[string]int64{
			"world_states.realm_id":               0,
			"world_runtime_states.(realm_id,key)": 0,
			"market_prices.(realm_id,item_key)":   0,
			"ascension_states.(realm_id,key)":     0,
		},
		InvalidRealmIDs: map[string]int64{
			"world_states":         0,
			"world_runtime_states": 0,
			"inventory_entries":    0,
			"player_unlocks":       0,
			"player_stats":         0,
			"behavior_instances":   0,
			"world_events":         0,
			"market_prices":        0,
			"market_histories":     0,
			"ascension_states":     0,
		},
	}

	indexChecks := []string{
		"idx_world_state_realm",
		"idx_world_runtime_realm_key",
		"idx_market_price_realm_item",
		"idx_ascension_realm_key",
	}
	for _, indexName := range indexChecks {
		exists, err := indexExists(database.WithContext(ctx), indexName)
		if err != nil {
			return report, err
		}
		if !exists {
			report.MissingIndexes = append(report.MissingIndexes, indexName)
		}
	}

	duplicateQueries := map[string]string{
		"world_states.realm_id":               `SELECT COUNT(*) FROM (SELECT realm_id FROM world_states GROUP BY realm_id HAVING COUNT(*) > 1) t`,
		"world_runtime_states.(realm_id,key)": `SELECT COUNT(*) FROM (SELECT realm_id, key FROM world_runtime_states GROUP BY realm_id, key HAVING COUNT(*) > 1) t`,
		"market_prices.(realm_id,item_key)":   `SELECT COUNT(*) FROM (SELECT realm_id, item_key FROM market_prices GROUP BY realm_id, item_key HAVING COUNT(*) > 1) t`,
		"ascension_states.(realm_id,key)":     `SELECT COUNT(*) FROM (SELECT realm_id, key FROM ascension_states GROUP BY realm_id, key HAVING COUNT(*) > 1) t`,
	}
	for key, query := range duplicateQueries {
		var value int64
		if err := database.WithContext(ctx).Raw(query).Scan(&value).Error; err != nil {
			return report, err
		}
		report.DuplicateGroups[key] = value
	}

	for table := range report.InvalidRealmIDs {
		var value int64
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE realm_id IS NULL OR realm_id = 0", quoteIdentifier(table))
		if err := database.WithContext(ctx).Raw(query).Scan(&value).Error; err != nil {
			return report, err
		}
		report.InvalidRealmIDs[table] = value
	}

	return report, nil
}

func indexExists(database *gorm.DB, indexName string) (bool, error) {
	var count int64
	query := `
		SELECT COUNT(*)
		FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND indexname = ?
	`
	if err := database.Raw(query, indexName).Scan(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func backfillRealmIDs(database *gorm.DB) error {
	tables := []string{
		"world_states",
		"world_runtime_states",
		"inventory_entries",
		"player_unlocks",
		"player_stats",
		"behavior_instances",
		"world_events",
		"market_prices",
		"market_histories",
		"ascension_states",
	}

	for _, table := range tables {
		if err := database.Exec(fmt.Sprintf("UPDATE %s SET realm_id = 1 WHERE realm_id IS NULL OR realm_id = 0", quoteIdentifier(table))).Error; err != nil {
			return err
		}
	}

	return nil
}

func normalizeRealmScopedDuplicates(database *gorm.DB) error {
	queries := []string{
		`
		WITH ranked AS (
			SELECT id,
				ROW_NUMBER() OVER (PARTITION BY realm_id ORDER BY simulation_tick DESC, id DESC) AS rn
			FROM world_states
		)
		DELETE FROM world_states
		WHERE id IN (SELECT id FROM ranked WHERE rn > 1)
		`,
		`
		WITH ranked AS (
			SELECT id,
				ROW_NUMBER() OVER (PARTITION BY realm_id, key ORDER BY updated_at DESC, id DESC) AS rn
			FROM world_runtime_states
		)
		DELETE FROM world_runtime_states
		WHERE id IN (SELECT id FROM ranked WHERE rn > 1)
		`,
		`
		WITH ranked AS (
			SELECT id,
				ROW_NUMBER() OVER (PARTITION BY realm_id, item_key ORDER BY updated_tick DESC, id DESC) AS rn
			FROM market_prices
		)
		DELETE FROM market_prices
		WHERE id IN (SELECT id FROM ranked WHERE rn > 1)
		`,
		`
		WITH ranked AS (
			SELECT id,
				ROW_NUMBER() OVER (PARTITION BY realm_id, key ORDER BY updated_at DESC, id DESC) AS rn
			FROM ascension_states
		)
		DELETE FROM ascension_states
		WHERE id IN (SELECT id FROM ranked WHERE rn > 1)
		`,
	}

	for _, query := range queries {
		if err := database.Exec(query).Error; err != nil {
			return err
		}
	}

	return nil
}

func dropLegacySingleColumnUniqueIndexes(database *gorm.DB) error {
	type tableColumnPair struct {
		table  string
		column string
	}

	legacy := []tableColumnPair{
		{table: "market_prices", column: "item_key"},
		{table: "world_runtime_states", column: "key"},
		{table: "ascension_states", column: "key"},
		{table: "world_states", column: "realm_id"},
	}

	for _, pair := range legacy {
		indexNames, err := findSingleColumnUniqueIndexes(database, pair.table, pair.column)
		if err != nil {
			return err
		}

		for _, indexName := range indexNames {
			if err := database.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", quoteIdentifier(indexName))).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

func findSingleColumnUniqueIndexes(database *gorm.DB, tableName, columnName string) ([]string, error) {
	type indexRow struct {
		Name string `gorm:"column:name"`
	}

	rows := make([]indexRow, 0)
	query := `
		SELECT indexname AS name
		FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND tablename = ?
		  AND indexdef ILIKE 'CREATE UNIQUE INDEX %'
		  AND indexdef ILIKE ?
	`
	columnPattern := fmt.Sprintf("%%(%s)%%", columnName)
	if err := database.Raw(query, tableName, columnPattern).Scan(&rows).Error; err != nil {
		return nil, err
	}

	results := make([]string, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		results = append(results, name)
	}

	return results, nil
}

func ensureRealmScopedUniqueIndexes(database *gorm.DB) error {
	queries := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_world_state_realm ON world_states (realm_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_world_runtime_realm_key ON world_runtime_states (realm_id, key)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_market_price_realm_item ON market_prices (realm_id, item_key)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ascension_realm_key ON ascension_states (realm_id, key)`,
	}

	for _, query := range queries {
		if err := database.Exec(query).Error; err != nil {
			return err
		}
	}

	return nil
}

func quoteIdentifier(identifier string) string {
	escaped := strings.ReplaceAll(identifier, `"`, `""`)
	return `"` + escaped + `"`
}
