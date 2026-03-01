package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/db"
	"github.com/asciifaceman/lived/pkg/migrations"
	"github.com/spf13/cobra"
)

var recreateDatabase bool

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database development helpers",
}

var dbSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Create or recreate the configured Postgres database",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.LoadFromEnv()

		status, err := db.EnsureDatabase(cmd.Context(), cfg.Postgres, cfg.PostgresAdminDB, recreateDatabase)
		if err != nil {
			return err
		}

		_, err = fmt.Fprintf(
			cmd.OutOrStdout(),
			"database %q on %s:%s %s\n",
			cfg.Postgres.DBName,
			cfg.Postgres.Host,
			cfg.Postgres.Port,
			status,
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

	dbCmd.AddCommand(dbSetupCmd)
	dbCmd.AddCommand(dbMigrateCmd)
	dbCmd.AddCommand(dbVerifyCmd)
	rootCmd.AddCommand(dbCmd)
}
