package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/knowledge-broker/knowledge-broker/internal/config"
)

func configCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show resolved configuration values and their sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved := loadConfig(cmd)

			// Print config files status.
			fmt.Println("Config files:")
			for _, f := range resolved.Files {
				label := f.Path
				status := "not found"
				if f.Path == "--config" {
					status = "(not specified)"
				} else if f.Found {
					status = "found"
				}
				fmt.Printf("  %-40s %s\n", label, status)
			}
			fmt.Println()

			// Print config values table.
			fmt.Printf("%-25s %-35s %s\n", "KEY", "VALUE", "SOURCE")
			fields := config.Fields()
			for _, f := range fields {
				info, ok := resolved.Origins[f.EnvVar]
				if !ok {
					continue
				}
				displayValue := info.Value
				if displayValue == "" {
					displayValue = "(not set)"
				} else if isSensitiveKey(f.EnvVar) {
					displayValue = maskSecret(displayValue)
				}
				fmt.Printf("%-25s %-35s %s\n", f.EnvVar, displayValue, info.Source)
			}

			return nil
		},
	}
}

// isSensitiveKey returns true if the key name suggests it holds a secret.
func isSensitiveKey(key string) bool {
	upper := strings.ToUpper(key)
	return strings.Contains(upper, "KEY") ||
		strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "TOKEN")
}

// maskSecret masks a secret value, showing first 8 chars + "****",
// or just "****" if the value is shorter than 8 chars.
func maskSecret(value string) string {
	if len(value) < 8 {
		return "****"
	}
	return value[:8] + "****"
}
