package agent

import (
	"flag"
	"fmt"
	"time"

	"github.com/mitchellh/cli"
	agentv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/server/agent/v1"
	"github.com/spiffe/spire-api-sdk/proto/spire/api/types"
	"github.com/spiffe/spire/cmd/spire-server/util"
	commoncli "github.com/spiffe/spire/pkg/common/cli"
	"github.com/spiffe/spire/pkg/common/cliprinter"
	"golang.org/x/net/context"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type purgeCommand struct {
	env          *commoncli.Env
	expiredAfter string
	printer      cliprinter.Printer
}

func NewPurgeCommand() cli.Command {
	return NewPurgeCommandWithEnv(commoncli.DefaultEnv)
}

func NewPurgeCommandWithEnv(env *commoncli.Env) cli.Command {
	return util.AdaptCommand(env, &purgeCommand{env: env})
}

func (*purgeCommand) Name() string {
	return "agent purge"
}

func (*purgeCommand) Synopsis() string {
	return "Evict expired agents based on a given time"
}

func (c *purgeCommand) Run(ctx context.Context, _ *commoncli.Env, serverClient util.ServerClient) error {
	expiredAfter, err := time.ParseDuration(c.expiredAfter)
	if err != nil {
		return fmt.Errorf("error parsing time: %v", err)
	}
	agentClient := serverClient.NewAgentClient()
	resp, err := agentClient.ListAgents(ctx, &agentv1.ListAgentsRequest{
		Filter:     &agentv1.ListAgentsRequest_Filter{ByCanReAttest: wrapperspb.Bool(true)},
		OutputMask: &types.AgentMask{X509SvidExpiresAt: true},
	})
	if err != nil {
		return err
	}

	agents := resp.GetAgents()

	for _, agent := range agents {
		expirationTime := time.Unix(agent.X509SvidExpiresAt, 0)

		if time.Now().Sub(expirationTime) > expiredAfter {
			_, err := agentClient.DeleteAgent(ctx, &agentv1.DeleteAgentRequest{Id: agent.Id})
			if err != nil {
				return err
			}
		}
	}

	return c.printer.PrintStruct(resp)
}

func (c *purgeCommand) AppendFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.expiredAfter, "expiredAfter", "0", "Indicates the time after which agents are considered expired. Defaults to now.")
	cliprinter.AppendFlagWithCustomPretty(&c.printer, fs, c.env, prettyPrintPurgeResult)
}

type PurgedAgents struct {
	Agents map[string]string `json:"agents"`
}

func prettyPrintPurgeResult(env *commoncli.Env, _ ...interface{}) error {
	env.Println("Agents purged successfully")
	return nil
}
