package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/asciifaceman/lived/pkg/config"
)

func EnsureDatabase(ctx context.Context, postgresCfg config.PostgresConfig, adminDBName string, recreate bool) (string, error) {
	adminCfg := postgresCfg
	adminCfg.DBName = adminDBName

	adminConn, err := Open(ctx, adminCfg.DSN())
	if err != nil {
		return "", err
	}

	var exists bool
	err = adminConn.WithContext(ctx).
		Raw("SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = ?)", postgresCfg.DBName).
		Scan(&exists).Error
	if err != nil {
		return "", err
	}

	if exists && !recreate {
		return "already exists", nil
	}

	if exists && recreate {
		if err := adminConn.WithContext(ctx).
			Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = ? AND pid <> pg_backend_pid()", postgresCfg.DBName).
			Error; err != nil {
			return "", err
		}

		dropSQL := fmt.Sprintf("DROP DATABASE %s", quoteIdentifier(postgresCfg.DBName))
		if err := adminConn.WithContext(ctx).Exec(dropSQL).Error; err != nil {
			return "", err
		}
	}

	createSQL := fmt.Sprintf(
		"CREATE DATABASE %s OWNER %s",
		quoteIdentifier(postgresCfg.DBName),
		quoteIdentifier(postgresCfg.User),
	)
	if err := adminConn.WithContext(ctx).Exec(createSQL).Error; err != nil {
		return "", err
	}

	if exists {
		return "recreated", nil
	}

	return "created", nil
}

func quoteIdentifier(input string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(input, `"`, `""`))
}
