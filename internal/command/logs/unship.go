package logs

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vektah/gqlparser/gqlerror"

	"github.com/superfly/flyctl/gql"
	"github.com/superfly/flyctl/iostreams"

	"github.com/superfly/flyctl/client"
	"github.com/superfly/flyctl/internal/appconfig"
	"github.com/superfly/flyctl/internal/command"
	"github.com/superfly/flyctl/internal/flag"
)

func newUnship() (cmd *cobra.Command) {
	const (
		short = "Stop shipping application logs to Logtail"
		long  = short + "\n"
	)

	cmd = command.New("unship", short, long, runUnship, command.RequireSession, command.RequireAppName)
	flag.Add(cmd,
		flag.App(),
		flag.AppConfig(),
	)
	return cmd
}

func runUnship(ctx context.Context) (err error) {

	var (
		out    = iostreams.FromContext(ctx).Out
		client = client.FromContext(ctx).API().GenqClient
	)

	appName := appconfig.NameFromContext(ctx)
	_, err = gql.GetApp(ctx, client, appName)

	if err != nil {
		return err
	}
	//	getAddOnResponse, err := gql.GetAddOn(ctx, client, LoggerAddOnName(appName, "logtail"))

	for err != nil {
		if errList, ok := err.(gqlerror.List); ok {
			for _, gqlErr := range errList {
				fmt.Println(gqlErr)
			}
			// input := gql.CreateAddOnInput{
			// 	OrganizationId: targetApp.Organization.Id,
			// 	Name:           addOnName,
			// 	AppId:          targetApp.Id,
			// 	Type:           gql.AddOnTypes[provider],
			// }

			//			createAddOnResponse, err := gql.CreateAddOn(ctx, client, input)

			if err != nil {
				//			return "", err
			}

			//	token = createAddOnResponse.CreateAddOn.AddOn.Token

			break

		} else {
			// return "", err
		}

		err = errors.Unwrap(err)
	}
	_, err = gql.DeleteAddOn(ctx, client, appName+"-log-shipper")

	if err != nil {
		return
	}

	fmt.Fprintf(out, "Logs for %s are no longer being shipped, but older logs are still preserved in Logtail.\n", appName)
	return
}
