package cmd

import (
	"os/signal"
	"syscall"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/src/app"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the Lived API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		cfg := config.LoadFromEnv()
		return app.Run(ctx, cfg)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}
