package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/secrets"
)

var secretRotateCmd = &cobra.Command{
	Use:   "rotate-key",
	Short: "Rotate the encryption key for local secrets store",
	Long: `Generates a new AES-256-GCM encryption key and re-encrypts all stored secrets.
The old key is backed up as .key.old in the data directory.

Does not require an app argument because it rotates the global key for the environment.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)

		sm, err := secrets.NewManager(dataDir, env)
		if err != nil {
			return fmt.Errorf("secrets manager: %w", err)
		}

		localProvider, ok := sm.Provider().(*secrets.LocalProvider)
		if !ok {
			return fmt.Errorf("key rotation is only supported for the local provider (current: %s)", sm.Provider().Name())
		}

		if err := localProvider.RotateKey(); err != nil {
			return fmt.Errorf("rotate key: %w", err)
		}

		fmt.Printf("[tengiz] encryption key rotated for environment %s\n", env)
		fmt.Println("[tengiz] old key backed up to ~/.tengiz/.key.old")
		return nil
	},
}
