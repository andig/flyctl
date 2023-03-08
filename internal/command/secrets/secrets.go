package secrets

import (
	"context"
	"errors"
	"fmt"

	"github.com/superfly/flyctl/api"
	"github.com/superfly/flyctl/internal/command"
	"github.com/superfly/flyctl/internal/command/deploy"
	"github.com/superfly/flyctl/internal/flag"
	"github.com/superfly/flyctl/internal/sentry"
	"github.com/superfly/flyctl/internal/watch"
	"github.com/superfly/flyctl/iostreams"

	"github.com/spf13/cobra"
)

var sharedFlags = flag.Set{
	flag.App(),
	flag.AppConfig(),
	flag.Detach(),
	flag.Bool{
		Name:        "stage",
		Description: "Set secrets but skip deployment for machine apps",
	},
}

func New() *cobra.Command {
	const (
		long = `Secrets are provided to applications at runtime as ENV variables. Names are
		case sensitive and stored as-is, so ensure names are appropriate for
		the application and vm environment.
		`

		short = "Manage application secrets with the set and unset commands."
	)

	secrets := command.New("secrets", short, long, nil)

	secrets.AddCommand(
		newList(),
		newSet(),
		newUnset(),
		newImport(),
	)

	return secrets
}

func deployForSecrets(ctx context.Context, app *api.AppCompact, release *api.Release) (err error) {
	out := iostreams.FromContext(ctx).Out

	if flag.GetBool(ctx, "stage") {

		if app.PlatformVersion != "machines" {
			return errors.New("--stage isn't available for Nomad apps")
		}

		fmt.Fprint(out, "Secrets have been staged, but not set on VMs. Deploy or update machines in this app for the secrets to take effect.\n")
		return
	}

	if !app.Deployed {
		fmt.Fprint(out, "Secrets are staged for the first deployment")
		return
	}

	if app.PlatformVersion == "machines" {
		ctx, err = command.LoadAppConfigIfPresent(ctx)
		if err != nil {
			return fmt.Errorf("error loading appv2 config: %w", err)
		}
		md, err := deploy.NewMachineDeployment(ctx, deploy.MachineDeploymentArgs{
			AppCompact:       app,
			RestartOnly:      true,
			SkipHealthChecks: flag.GetBool(ctx, "detach"),
		})
		if err != nil {
			sentry.CaptureExceptionWithAppInfo(err, "secrets", app)
			return err
		}
		err = md.DeployMachinesApp(ctx)
		if err != nil {
			sentry.CaptureExceptionWithAppInfo(err, "secrets", app)
		}
		return err
	}

	fmt.Fprintf(out, "Release v%d created\n", release.Version)

	if flag.GetBool(ctx, "detach") {
		return
	}

	err = watch.Deployment(ctx, app.Name, release.EvaluationID)

	return err
}
