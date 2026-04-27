package cmd

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"telecom-news-cli/internal/server"

	"github.com/spf13/cobra"
)

var serveAddr string
var serveReadTimeout time.Duration
var serveWriteTimeout time.Duration
var serveWebUser string
var serveWebPassword string

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

		webUser := firstNonEmpty(serveWebUser, os.Getenv("TELECOM_NEWS_WEB_USER"))
		webPassword := firstNonEmpty(serveWebPassword, os.Getenv("TELECOM_NEWS_WEB_PASSWORD"))
		if (webUser == "") != (webPassword == "") {
			return fmt.Errorf("web auth requires both --web-user and --web-password, or TELECOM_NEWS_WEB_USER and TELECOM_NEWS_WEB_PASSWORD")
		}

		app := server.NewWithCatalogPaths(database, creds, sourcesPath, categoriesPath)
		app.SetWebAuth(webUser, webPassword)
		fmt.Printf("API server listening on %s\n", serveAddr)
		if webUser != "" {
			fmt.Printf("Web dashboard authentication enabled for user %q\n", webUser)
		}
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
	serveCmd.Flags().DurationVar(&serveWriteTimeout, "write-timeout", 10*time.Minute, "HTTP write timeout")
	serveCmd.Flags().StringVar(&serveWebUser, "web-user", "", "Username for HTTP Basic Auth on dashboard and API")
	serveCmd.Flags().StringVar(&serveWebPassword, "web-password", "", "Password for HTTP Basic Auth on dashboard and API")
	rootCmd.AddCommand(serveCmd)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
