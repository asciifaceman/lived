package cmd

import (
	"fmt"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/db"
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

func init() {
	dbSetupCmd.Flags().BoolVar(&recreateDatabase, "recreate", false, "Drop and recreate the database if it already exists")

	dbCmd.AddCommand(dbSetupCmd)
	rootCmd.AddCommand(dbCmd)
}
