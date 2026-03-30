package agent

import (
	"github.com/spf13/cobra"
	"github.com/xcdev-0/redis-operator/internal/cmd/agent/bootstrap"
)

// CMD creates a cobra command for the agent
func CMD() *cobra.Command {
	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Agent runs as an init/sidecar container alongside Redis",
	}
	agentCmd.AddCommand(bootstrap.CMD())
	return agentCmd
}
