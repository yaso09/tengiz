package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tengiz",
	Short: "Tengiz - Serverless deployment platform",
	Long:  "Tengiz is a Vercel alternative. Deploy any app with scale-to-zero.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
