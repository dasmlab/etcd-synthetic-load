package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cln "github.com/dasmlab/etcd-synthetic-load/internal/cleanup"
	"github.com/dasmlab/etcd-synthetic-load/internal/k8sclient"
)

func newCleanupCmd(gf *globalFlags) *cobra.Command {
	var profileID string
	var dryRun bool
	var yes bool
	var wait bool
	var waitTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Delete all namespaces/objects labeled as belonging to this harness",
		Long: `cleanup deletes every namespace labeled
etcd-synthetic-load.dasmlab.org/managed=true (optionally scoped to a single
--profile-id). Deleting a namespace cascades to all Secrets/ConfigMaps
inside it, so no separate object-level deletion is needed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := k8sclient.New(k8sclient.OptionsFromEnv(gf.kubeconfig))
			if err != nil {
				return fmt.Errorf("build kubernetes client: %w", err)
			}

			dryOpts := cln.Options{Client: client, ProfileID: profileID, DryRun: true}
			preview, err := cln.Run(context.Background(), dryOpts)
			if err != nil {
				return err
			}
			if len(preview.NamespacesDeleted) == 0 {
				fmt.Println("Nothing to clean up.")
				return nil
			}

			fmt.Printf("The following %d namespace(s) will be deleted:\n", len(preview.NamespacesDeleted))
			for _, n := range preview.NamespacesDeleted {
				fmt.Printf("  - %s\n", n)
			}

			if dryRun {
				fmt.Println("\n(dry-run: no changes made)")
				return nil
			}

			if !yes {
				if !confirm(fmt.Sprintf("Delete these %d namespace(s)? [y/N]: ", len(preview.NamespacesDeleted))) {
					fmt.Println("Aborted.")
					return nil
				}
			}

			res, err := cln.Run(context.Background(), cln.Options{
				Client:      client,
				ProfileID:   profileID,
				DryRun:      false,
				Wait:        wait,
				WaitTimeout: waitTimeout,
			})
			if err != nil {
				return err
			}

			fmt.Printf("Deleted (namespace delete requested for) %d namespace(s).\n", len(res.NamespacesDeleted))
			if wait {
				fmt.Println("All namespaces confirmed terminated.")
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&profileID, "profile-id", "", "only delete objects belonging to this profile id (default: all etcd-synthetic-load namespaces)")
	f.BoolVar(&dryRun, "dry-run", false, "list what would be deleted without deleting")
	f.BoolVarP(&yes, "yes", "y", false, "skip the interactive confirmation prompt")
	f.BoolVar(&wait, "wait", false, "wait for namespaces to fully terminate before returning")
	f.DurationVar(&waitTimeout, "wait-timeout", 10*time.Minute, "max time to wait when --wait is set")

	return cmd
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
