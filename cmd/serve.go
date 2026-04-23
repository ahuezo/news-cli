package cmd

import (
	"fmt"
	"net/http"
	"time"

	"telecom-news-cli/internal/server"

	"github.com/spf13/cobra"
)

var serveAddr string
var serveReadTimeout time.Duration
var serveWriteTimeout time.Duration

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the HTTP API server",
	Long: `Run a JSON HTTP API that exposes the same functionality as the CLI.

Examples:
  telecom-news serve
  telecom-news serve --addr :9090`,
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		creds, err := loadCreds()
		if err != nil {
			return err
		}

		app := server.New(database, creds)
		fmt.Printf("API server listening on %s\n", serveAddr)
		srv := &http.Server{
			Addr:         serveAddr,
			Handler:      app.Router(),
			ReadTimeout:  serveReadTimeout,
			WriteTimeout: serveWriteTimeout,
			IdleTimeout:  60 * time.Second,
		}
		return srv.ListenAndServe()
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", ":8080", "HTTP listen address")
	serveCmd.Flags().DurationVar(&serveReadTimeout, "read-timeout", 10*time.Second, "HTTP read timeout")
	serveCmd.Flags().DurationVar(&serveWriteTimeout, "write-timeout", 20*time.Second, "HTTP write timeout")
	rootCmd.AddCommand(serveCmd)
}
