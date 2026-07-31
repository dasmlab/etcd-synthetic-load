package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/dasmlab/etcd-synthetic-load/internal/api"
	"github.com/dasmlab/etcd-synthetic-load/internal/config"
	"github.com/dasmlab/etcd-synthetic-load/internal/runs"
	"github.com/dasmlab/etcd-synthetic-load/internal/target"
)

//go:embed all:static
var staticEmbed embed.FS

func newServeCmd() *cobra.Command {
	var runtimePath string
	var addr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the web UI + API (generate / load / results)",
		Long: `serve embeds the Vue UI and exposes /api/v1 for configure/generate/load flows.

Mount a host directory at the data path so plans/runs survive container exit:
  docker run --rm -p 8080:8080 -v "$PWD/data:/data" \
    -e OC_SERVER -e OC_USER -e OC_PASSWORD \
    IMAGE serve --runtime /data/runtime.yaml

WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := config.LoadRuntime(runtimePath)
			if err != nil {
				return err
			}
			if err := config.EnsureDirs(rt); err != nil {
				return err
			}
			runStore, err := runs.New(rt.Paths.RunsDir)
			if err != nil {
				return err
			}
			targetStore, err := target.NewStore(rt.Paths.DataDir)
			if err != nil {
				return err
			}

			var staticHandler http.Handler
			sub, err := fs.Sub(staticEmbed, "static")
			if err == nil {
				staticHandler = api.StaticFS{Root: http.FS(sub)}
			} else {
				fmt.Fprintln(os.Stderr, "warning: no embedded UI (static/); API-only mode")
			}

			if addr == "" {
				addr = rt.Server.ListenAddr
			}
			srv := api.New(rt, runStore, targetStore, version, staticHandler)
			fmt.Fprintf(os.Stderr, "WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT\n")
			fmt.Fprintf(os.Stderr, "serving UI+API on %s (data=%s cluster=%s)\n", addr, rt.Paths.DataDir, rt.Cluster.DisplayName)
			return api.ListenAndServe(addr, srv.Handler())
		},
	}
	cmd.Flags().StringVar(&runtimePath, "runtime", "", "path to runtime.yaml")
	cmd.Flags().StringVar(&addr, "listen", "", "listen address (default from runtime.yaml / :8080)")
	return cmd
}
