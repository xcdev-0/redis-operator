package bootstrap

import (
	"github.com/spf13/cobra"
	"github.com/xcdev-0/redis-operator/internal/agent/bootstrap"
)

func CMD() *cobra.Command {
	bootstrapCmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Bootstrap do some init work before run redis",
		RunE: func(cmd *cobra.Command, args []string) error {
			return bootstrap.Run()
		},
	}
	return bootstrapCmd
}
