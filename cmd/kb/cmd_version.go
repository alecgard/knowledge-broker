package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

func versionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(cmd *cobra.Command, args []string) error {
			remote, _ := cmd.Flags().GetString("remote")
			if remote != "" {
				remote = strings.TrimRight(remote, "/")
				var resp map[string]string
				if err := remoteJSON(context.Background(), http.MethodGet, remote+"/v1/version", nil, &resp); err != nil {
					return err
				}
				fmt.Printf("kb %s (remote: %s)\n", resp["version"], remote)
				return nil
			}
			fmt.Printf("kb %s\n", version)
			return nil
		},
	}
	cmd.Flags().String("remote", "", "URL of a remote KB server")
	return cmd
}
