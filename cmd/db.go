package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/asciifaceman/lived/pkg/db"
	"github.com/asciifaceman/lived/pkg/migrations"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var recreateDatabase bool
var setAdminUsername string
var setAdminAccountID uint

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database development helpers",
}

var dbSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Create or recreate the configured Postgres database",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.LoadFromEnv()
		postgresCfg := postgresConfigForProvisioning(cfg)

		status, err := db.EnsureDatabase(cmd.Context(), postgresCfg, cfg.PostgresAdminDB, recreateDatabase)
		if err != nil {
			return err
		}

		_, err = fmt.Fprintf(
			cmd.OutOrStdout(),
			"database %q on %s:%s %s\n",
			postgresCfg.DBName,
			postgresCfg.Host,
			postgresCfg.Port,
			status,
		)
		return err
	},
}

var dbSetAdminCmd = &cobra.Command{
	Use:   "set-admin",
	Short: "Grant admin role to an account",
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(setAdminUsername) == "" && setAdminAccountID == 0 {
			return fmt.Errorf("provide --username or --account-id")
		}
		if strings.TrimSpace(setAdminUsername) != "" && setAdminAccountID != 0 {
			return fmt.Errorf("provide only one of --username or --account-id")
		}

		cfg := config.LoadFromEnv()
		database, err := db.Open(cmd.Context(), cfg.DatabaseURL)
		if err != nil {
			return err
		}

		account, err := findAccountForAdminGrant(cmd, database)
		if err != nil {
			return err
		}

		granted, err := ensureAccountRole(cmd, database, account.ID, "admin")
		if err != nil {
			return err
		}

		status := "already had"
		if granted {
			status = "granted"
		}

		_, err = fmt.Fprintf(
			cmd.OutOrStdout(),
			"admin role %s for account id=%d username=%q status=%q\n",
			status,
			account.ID,
			account.Username,
			account.Status,
		)
		return err
	},
}

var dbMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.LoadFromEnv()

		database, err := db.Open(cmd.Context(), cfg.DatabaseURL)
		if err != nil {
			return err
		}

		if err := migrations.Run(cmd.Context(), database); err != nil {
			return err
		}

		_, err = fmt.Fprintln(cmd.OutOrStdout(), "database migrations applied")
		return err
	},
}

var dbVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify realm-scoped migration/index health",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.LoadFromEnv()

		database, err := db.Open(cmd.Context(), cfg.DatabaseURL)
		if err != nil {
			return err
		}

		report, err := migrations.VerifyRealmScoping(cmd.Context(), database)
		if err != nil {
			return err
		}

		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}

		if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(encoded)); err != nil {
			return err
		}

		if !report.IsHealthy() {
			return fmt.Errorf("realm migration verification failed: %s", report.Summary())
		}

		_, err = fmt.Fprintln(cmd.OutOrStdout(), "realm migration verification passed")
		return err
	},
}

func init() {
	dbSetupCmd.Flags().BoolVar(&recreateDatabase, "recreate", false, "Drop and recreate the database if it already exists")
	dbSetAdminCmd.Flags().StringVar(&setAdminUsername, "username", "", "Username of account to grant admin role")
	dbSetAdminCmd.Flags().UintVar(&setAdminAccountID, "account-id", 0, "Numeric account ID to grant admin role")

	dbCmd.AddCommand(dbSetupCmd)
	dbCmd.AddCommand(dbMigrateCmd)
	dbCmd.AddCommand(dbVerifyCmd)
	dbCmd.AddCommand(dbSetAdminCmd)
	rootCmd.AddCommand(dbCmd)
}

func postgresConfigForProvisioning(cfg config.Config) config.PostgresConfig {
	resolved := cfg.Postgres
	databaseURL := strings.TrimSpace(cfg.DatabaseURL)
	if databaseURL == "" {
		return resolved
	}

	if strings.Contains(databaseURL, "://") {
		if parsed, err := url.Parse(databaseURL); err == nil {
			if host := parsed.Hostname(); host != "" {
				resolved.Host = host
			}
			if port := parsed.Port(); port != "" {
				resolved.Port = port
			}
			if user := parsed.User.Username(); user != "" {
				resolved.User = user
			}
			if password, ok := parsed.User.Password(); ok {
				resolved.Password = password
			}
			if dbName := strings.TrimPrefix(strings.TrimSpace(parsed.Path), "/"); dbName != "" {
				resolved.DBName = dbName
			}
			queryValues := parsed.Query()
			if sslMode := strings.TrimSpace(queryValues.Get("sslmode")); sslMode != "" {
				resolved.SSLMode = sslMode
			}
			if timezone := strings.TrimSpace(queryValues.Get("TimeZone")); timezone != "" {
				resolved.TimeZone = timezone
			}
		}
		return resolved
	}

	for _, token := range strings.Fields(databaseURL) {
		parts := strings.SplitN(token, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.Trim(strings.TrimSpace(parts[1]), "'\"")
		if value == "" {
			continue
		}

		switch key {
		case "host":
			resolved.Host = value
		case "port":
			resolved.Port = value
		case "user", "username":
			resolved.User = value
		case "password":
			resolved.Password = value
		case "dbname", "database":
			resolved.DBName = value
		case "sslmode":
			resolved.SSLMode = value
		case "timezone":
			resolved.TimeZone = value
		}
	}

	return resolved
}

func findAccountForAdminGrant(cmd *cobra.Command, database *gorm.DB) (dal.Account, error) {
	if setAdminAccountID != 0 {
		account := dal.Account{}
		result := database.WithContext(cmd.Context()).Where("id = ?", setAdminAccountID).Limit(1).Find(&account)
		if result.Error != nil {
			return dal.Account{}, result.Error
		}
		if result.RowsAffected == 0 {
			return dal.Account{}, fmt.Errorf("account %d not found", setAdminAccountID)
		}
		return account, nil
	}

	username := strings.TrimSpace(setAdminUsername)
	account := dal.Account{}
	result := database.WithContext(cmd.Context()).Where("username = ?", username).Limit(1).Find(&account)
	if result.Error != nil {
		return dal.Account{}, result.Error
	}
	if result.RowsAffected == 0 {
		return dal.Account{}, fmt.Errorf("account %q not found", username)
	}

	return account, nil
}

func ensureAccountRole(cmd *cobra.Command, database *gorm.DB, accountID uint, roleKey string) (bool, error) {
	granted := false
	err := database.WithContext(cmd.Context()).Transaction(func(tx *gorm.DB) error {
		role := dal.AccountRole{}
		result := tx.WithContext(cmd.Context()).
			Where("account_id = ? AND role_key = ?", accountID, roleKey).
			Limit(1).
			Find(&role)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return nil
		}

		if err := tx.WithContext(cmd.Context()).Create(&dal.AccountRole{AccountID: accountID, RoleKey: roleKey}).Error; err != nil {
			return err
		}

		granted = true
		return nil
	})
	if err != nil {
		return false, err
	}

	return granted, nil
}
