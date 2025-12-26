package agent

import (
	"github.com/spf13/cobra"
	"github.com/xc/redis-operator/internal/cmd/agent/bootstrap"
)

// CMD creates a cobra command for the agent
func CMD() *cobra.Command {
	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Agent is a tool which run as a init/sidecar container along with redis/sentinel",
	}
	agentCmd.AddCommand(bootstrap.CMD())
	return agentCmd
}
