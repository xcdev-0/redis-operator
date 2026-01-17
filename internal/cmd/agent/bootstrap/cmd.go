package bootstrap

import (
	"github.com/spf13/cobra"
	redisbootstrap "github.com/xcdev-0/redis-operator/internal/agent/bootstrap/redis"
)

func CMD() *cobra.Command {
	bootstrapCmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Bootstrap do some init work before run redisutils",
		RunE: func(cmd *cobra.Command, args []string) error {
			return redisbootstrap.GenerateConfig()
		},
	}
	return bootstrapCmd
}
