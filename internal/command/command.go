// Package command implements helpers useful for when building cobra commands.
package command

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/blang/semver"
	"github.com/logrusorgru/aurora"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/superfly/flyctl/api"
	"github.com/superfly/flyctl/iostreams"

	"github.com/superfly/flyctl/client"
	"github.com/superfly/flyctl/internal/appconfig"
	"github.com/superfly/flyctl/internal/buildinfo"
	"github.com/superfly/flyctl/internal/cache"
	"github.com/superfly/flyctl/internal/config"
	"github.com/superfly/flyctl/internal/env"
	"github.com/superfly/flyctl/internal/flag"
	"github.com/superfly/flyctl/internal/httptracing"
	"github.com/superfly/flyctl/internal/instrument"
	"github.com/superfly/flyctl/internal/logger"
	"github.com/superfly/flyctl/internal/metrics"
	"github.com/superfly/flyctl/internal/state"
	"github.com/superfly/flyctl/internal/task"
	"github.com/superfly/flyctl/internal/update"
)

type (
	Preparer func(context.Context) (context.Context, error)

	Runner func(context.Context) error
)

func New(usage, short, long string, fn Runner, p ...Preparer) *cobra.Command {
	return &cobra.Command{
		Use:   usage,
		Short: short,
		Long:  long,
		RunE:  newRunE(fn, p...),
	}
}

var commonPreparers = []Preparer{
	applyAliases,
	determineHostname,
	determineWorkingDir,
	determineUserHomeDir,
	determineConfigDir,
	ensureConfigDirExists,
	ensureConfigDirPerms,
	loadCache,
	loadConfig,
	initTaskManager,
	startQueryingForNewRelease,
	promptToUpdate,
	initClient,
	killOldAgent,
	recordMetricsCommandContext,
}

func sendOsMetric(ctx context.Context, state string) {
	// Send /runs/[os_name]/[state]
	osName := ""
	switch runtime.GOOS {
	case "darwin":
		osName = "macos"
	case "linux":
		osName = "linux"
	case "windows":
		osName = "windows"
	default:
		osName = "other"
	}
	metrics.SendNoData(ctx, fmt.Sprintf("runs/%s/%s", osName, state))
}

func newRunE(fn Runner, preparers ...Preparer) func(*cobra.Command, []string) error {
	if fn == nil {
		return nil
	}

	return func(cmd *cobra.Command, _ []string) (err error) {
		ctx := cmd.Context()
		ctx = NewContext(ctx, cmd)
		ctx = flag.NewContext(ctx, cmd.Flags())

		// run the common preparers
		if ctx, err = prepare(ctx, commonPreparers...); err != nil {
			return
		}

		sendOsMetric(ctx, "started")
		defer func() {
			if err == nil {
				sendOsMetric(ctx, "successful")
			}
		}()

		// run the preparers specific to the command
		if ctx, err = prepare(ctx, preparers...); err != nil {
			return
		}

		// run the command
		if err = fn(ctx); err == nil {
			// and finally, run the finalizer
			finalize(ctx)
		}

		return
	}
}

func prepare(parent context.Context, preparers ...Preparer) (ctx context.Context, err error) {
	ctx = parent

	for _, p := range preparers {
		if ctx, err = p(ctx); err != nil {
			break
		}
	}

	return
}

func finalize(ctx context.Context) {
	// shutdown async tasks
	task.FromContext(ctx).Shutdown()

	// flush the cache to disk if required
	if c := cache.FromContext(ctx); c.Dirty() {
		path := filepath.Join(state.ConfigDirectory(ctx), cache.FileName)

		if err := c.Save(path); err != nil {
			logger.FromContext(ctx).
				Warnf("failed saving cache to %s: %v", path, err)
		}
	}
}

// applyAliases consolidates flags with aliases into a single source-of-truth flag.
// After calling this, the main flags will have their values set as follows:
//   - If the main flag was already set, it will keep its value.
//   - If it was not set, but an alias was, it will take the value of the first specified alias flag.
//     This will set flag.Changed to true, as if it were specified manually.
//   - If none of the flags were set, the main flag will remain its default value.
func applyAliases(ctx context.Context) (context.Context, error) {

	var (
		invalidFlagNames []string
		invalidTypes     []string

		flags = flag.FromContext(ctx)
	)
	flags.VisitAll(func(f *pflag.Flag) {
		aliases, ok := f.Annotations["flyctl_alias"]
		if !ok {
			return
		}

		name := f.Name
		gotValue := false
		origFlag := flags.Lookup(name)

		if origFlag == nil {
			invalidFlagNames = append(invalidFlagNames, name)
		} else {
			gotValue = origFlag.Changed
		}

		for _, alias := range aliases {
			aliasFlag := flags.Lookup(alias)
			if aliasFlag == nil {
				invalidFlagNames = append(invalidFlagNames, alias)
				continue
			}
			if origFlag == nil {
				continue // nothing left to do here if we have no root flag
			}
			if aliasFlag.Value.Type() != origFlag.Value.Type() {
				invalidTypes = append(invalidTypes, fmt.Sprintf("%s (%s) and %s (%s)", name, origFlag.Value.Type(), alias, aliasFlag.Value.Type()))
			}
			if !gotValue && aliasFlag.Changed {
				err := origFlag.Value.Set(aliasFlag.Value.String())
				if err != nil {
					panic(err)
				}
				origFlag.Changed = true
			}
		}
	})

	var err error
	{
		var errorMessages []string
		if len(invalidFlagNames) > 0 {
			errorMessages = append(errorMessages, fmt.Sprintf("flags '%v' are not valid flags", invalidFlagNames))
		}
		if len(invalidTypes) > 0 {
			errorMessages = append(errorMessages, fmt.Sprintf("flags '%v' have different types", invalidTypes))
		}
		if len(errorMessages) > 1 {
			err = fmt.Errorf("multiple errors occured:\n > %s\n", strings.Join(errorMessages, "\n > "))
		} else if len(errorMessages) == 1 {
			err = fmt.Errorf("%s", errorMessages[0])
		}
	}
	return ctx, err
}

func determineHostname(ctx context.Context) (context.Context, error) {
	h, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed determining hostname: %w", err)
	}

	logger.FromContext(ctx).
		Debugf("determined hostname: %q", h)

	return state.WithHostname(ctx, h), nil
}

func determineWorkingDir(ctx context.Context) (context.Context, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed determining working directory: %w", err)
	}

	logger.FromContext(ctx).
		Debugf("determined working directory: %q", wd)

	return state.WithWorkingDirectory(ctx, wd), nil
}

func determineUserHomeDir(ctx context.Context) (context.Context, error) {
	wd, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed determining user home directory: %w", err)
	}

	logger.FromContext(ctx).
		Debugf("determined user home directory: %q", wd)

	return state.WithUserHomeDirectory(ctx, wd), nil
}

func determineConfigDir(ctx context.Context) (context.Context, error) {
	dir := filepath.Join(state.UserHomeDirectory(ctx), ".fly")

	logger.FromContext(ctx).
		Debugf("determined config directory: %q", dir)

	return state.WithConfigDirectory(ctx, dir), nil
}

func ensureConfigDirExists(ctx context.Context) (context.Context, error) {
	dir := state.ConfigDirectory(ctx)

	switch fi, err := os.Stat(dir); {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("failed creating config directory: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("failed stat-ing config directory: %w", err)
	case !fi.IsDir():
		return nil, fmt.Errorf("the path to the config directory (%s) is occupied by not a directory", dir)
	}

	logger.FromContext(ctx).
		Debug("ensured config directory exists.")

	return ctx, nil
}

func ensureConfigDirPerms(parent context.Context) (ctx context.Context, err error) {
	defer func() {
		if err != nil {
			ctx = nil
			err = fmt.Errorf("failed ensuring config directory perms: %w", err)

			return
		}

		logger.FromContext(ctx).
			Debug("ensured config directory perms.")
	}()

	ctx = parent
	dir := state.ConfigDirectory(parent)

	var f *os.File
	if f, err = os.CreateTemp(dir, "perms.*"); err != nil {
		return
	}
	defer func() {
		if e := os.Remove(f.Name()); err == nil {
			err = e
		}
	}()

	err = f.Close()

	return
}

func loadCache(ctx context.Context) (context.Context, error) {
	logger := logger.FromContext(ctx)

	path := filepath.Join(state.ConfigDirectory(ctx), cache.FileName)

	c, err := cache.Load(path)
	if err != nil {
		c = cache.New()

		if !errors.Is(err, fs.ErrNotExist) {
			logger.Warnf("failed loading cache file from %s: %v", path, err)
		}
	}

	logger.Debug("cache loaded.")

	return cache.NewContext(ctx, c), nil
}

func loadConfig(ctx context.Context) (context.Context, error) {
	logger := logger.FromContext(ctx)

	cfg := config.New()

	// Apply config from the config file, if it exists
	path := filepath.Join(state.ConfigDirectory(ctx), config.FileName)
	if err := cfg.ApplyFile(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	// Apply config from the environment, overriding anything from the file
	cfg.ApplyEnv()

	// Finally, apply command line options, overriding any previous setting
	cfg.ApplyFlags(flag.FromContext(ctx))

	logger.Debug("config initialized.")

	return config.NewContext(ctx, cfg), nil
}

func initClient(ctx context.Context) (context.Context, error) {
	logger := logger.FromContext(ctx)
	cfg := config.FromContext(ctx)

	// TODO: refactor so that api package does NOT depend on global state
	api.SetBaseURL(cfg.APIBaseURL)
	api.SetErrorLog(cfg.LogGQLErrors)
	api.SetInstrumenter(instrument.ApiAdapter)
	api.SetTransport(httptracing.NewTransport(http.DefaultTransport))

	c := client.FromToken(cfg.AccessToken)
	logger.Debug("client initialized.")

	return client.NewContext(ctx, c), nil
}

func initTaskManager(ctx context.Context) (context.Context, error) {
	tm := task.New(ctx)

	logger.FromContext(ctx).Debug("initialized task manager.")

	return task.NewContext(ctx, tm), nil
}

func IsMachinesPlatform(ctx context.Context, appName string) (bool, error) {
	apiClient := client.FromContext(ctx).API()
	app, err := apiClient.GetAppBasic(ctx, appName)
	if err != nil {
		return false, fmt.Errorf("failed to retrieve app: %w", err)
	}

	return app.PlatformVersion == appconfig.MachinesPlatform, nil
}

func startQueryingForNewRelease(ctx context.Context) (context.Context, error) {
	logger := logger.FromContext(ctx)

	cache := cache.FromContext(ctx)
	if !update.Check() || time.Since(cache.LastCheckedAt()) < time.Hour {
		logger.Debug("skipped querying for new release")

		return ctx, nil
	}

	channel := cache.Channel()
	tm := task.FromContext(ctx)

	tm.Run(func(parent context.Context) {
		ctx, cancel := context.WithTimeout(parent, time.Second)
		defer cancel()

		switch r, err := update.LatestRelease(ctx, channel); {
		case err == nil:
			if r == nil {
				break
			}

			cache.SetLatestRelease(channel, r)

			logger.Debugf("querying for release resulted to %v", r.Version)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			break
		default:
			logger.Warnf("failed querying for new release: %v", err)
		}
	})

	logger.Debug("started querying for new release")

	return ctx, nil
}

// shouldIgnore allows a preparer to disable itself for specific commands
// E.g. `shouldIgnore([][]string{{"version", "upgrade"}, {"machine", "status"}})`
// would return true for "fly version upgrade" and "fly machine status"
func shouldIgnore(ctx context.Context, cmds [][]string) bool {
	cmd := FromContext(ctx)
	for _, ignoredCmd := range cmds {
		match := true
		currentCmd := cmd
		// The shape of the ignoredCmd slice is something like ["version", "upgrade"],
		// but we're walking up the tree from the end, so we have to iterate that in reverse
		for i := len(ignoredCmd) - 1; i >= 0; i-- {
			if !currentCmd.HasParent() || currentCmd.Use != ignoredCmd[i] {
				match = false
				break
			}
			currentCmd = currentCmd.Parent()
		}
		// Ensure that we have the root node, so that a filter on ["y"] wouldn't be tripped by "fly x y"
		if match {
			if !currentCmd.HasParent() {
				return true
			}
		}
	}
	return false
}

func promptToUpdate(ctx context.Context) (context.Context, error) {
	cfg := config.FromContext(ctx)
	if cfg.JSONOutput || shouldIgnore(ctx, [][]string{
		{"version", "upgrade"},
	}) {
		return ctx, nil
	}

	if !update.Check() {
		return ctx, nil
	}

	c := cache.FromContext(ctx)

	r := c.LatestRelease()
	if r == nil {
		return ctx, nil
	}

	logger := logger.FromContext(ctx)

	current := buildinfo.Info().Version

	switch latest, err := semver.ParseTolerant(r.Version); {
	case err != nil:
		logger.Warnf("error parsing version number '%s': %s", r.Version, err)

		return ctx, nil
	case latest.LTE(current):
		return ctx, nil
	}

	io := iostreams.FromContext(ctx)
	colorize := io.ColorScheme()

	msg := fmt.Sprintf("Update available %s -> %s.\nRun \"%s\" to upgrade.",
		current,
		r.Version,
		colorize.Bold(buildinfo.Name()+" version upgrade"),
	)

	fmt.Fprintln(io.ErrOut, colorize.Yellow(msg))

	return ctx, nil
}

func PromptToMigrate(ctx context.Context, app *api.AppCompact) {
	if app.PlatformVersion == "nomad" {
		config := appconfig.ConfigFromContext(ctx)
		if config != nil {
			io := iostreams.FromContext(ctx)
			fmt.Fprintf(io.ErrOut, "%s Apps v1 Platform is deprecated. We recommend migrating your app with:\nfly migrate-to-v2 -c %s\n", aurora.Yellow("WARN"), config.ConfigFilePath())
		}
	}
}

func killOldAgent(ctx context.Context) (context.Context, error) {
	path := filepath.Join(state.ConfigDirectory(ctx), "agent.pid")

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ctx, nil // no old agent running or can't access that file
	} else if err != nil {
		return nil, fmt.Errorf("failed reading old agent's PID file: %w", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed determining old agent's PID: %w", err)
	}

	logger := logger.FromContext(ctx)
	unlink := func() (err error) {
		if err = os.Remove(path); err != nil {
			err = fmt.Errorf("failed removing old agent's PID file: %w", err)

			return
		}

		logger.Debug("removed old agent's PID file.")

		return
	}

	p, err := os.FindProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("failed retrieving old agent's process: %w", err)
	} else if p == nil {
		return ctx, unlink()
	}

	if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return nil, fmt.Errorf("failed killing old agent process: %w", err)
	}

	logger.Debugf("killed old agent (PID: %d)", pid)

	if err := unlink(); err != nil {
		return nil, err
	}

	time.Sleep(time.Second) // we've killed and removed the pid file

	return ctx, nil
}

func recordMetricsCommandContext(ctx context.Context) (context.Context, error) {
	metrics.RecordCommandContext(ctx)
	return ctx, nil
}

func ExcludeFromMetrics(ctx context.Context) (context.Context, error) {
	metrics.Enabled = false
	return ctx, nil
}

// RequireSession is a Preparer which makes sure a session exists.
func RequireSession(ctx context.Context) (context.Context, error) {
	if !client.FromContext(ctx).Authenticated() {
		return nil, client.ErrNoAuthToken
	}

	return ctx, nil
}

// LoadAppConfigIfPresent is a Preparer which loads the application's
// configuration file from the path the user has selected via command line args
// or the current working directory.
func LoadAppConfigIfPresent(ctx context.Context) (context.Context, error) {
	// Shortcut to avoid unmarshaling and querying Web when
	// LoadAppConfigIfPresent is chained with RequireAppName
	if cfg := appconfig.ConfigFromContext(ctx); cfg != nil {
		return ctx, nil
	}

	logger := logger.FromContext(ctx)
	for _, path := range appConfigFilePaths(ctx) {
		switch cfg, err := appconfig.LoadConfig(path); {
		case err == nil:
			logger.Debugf("app config loaded from %s", path)

			// Query Web API for platform version
			platformVersion, _ := determinePlatform(ctx, cfg.AppName)
			if platformVersion != "" {
				err := cfg.SetPlatformVersion(platformVersion)
				if err != nil {
					logger.Warnf("WARNING the config file at '%s' is not valid: %s", path, err)
				}
			}

			return appconfig.WithConfig(ctx, cfg), nil // we loaded a configuration file
		case errors.Is(err, fs.ErrNotExist):
			logger.Debugf("no app config found at %s; skipped.", path)
			continue
		default:
			return nil, fmt.Errorf("failed loading app config from %s: %w", path, err)
		}
	}

	return ctx, nil
}

func determinePlatform(ctx context.Context, appName string) (string, error) {
	client := client.FromContext(ctx)
	if appName == "" {
		return "", fmt.Errorf("Can't determine platform without an application name")
	}

	basicApp, err := client.API().GetAppBasic(ctx, appName)
	if err != nil {
		return "", err
	}
	return basicApp.PlatformVersion, nil
}

// appConfigFilePaths returns the possible paths at which we may find a fly.toml
// in order of preference. it takes into consideration whether the user has
// specified a command-line path to a config file.
func appConfigFilePaths(ctx context.Context) (paths []string) {
	if p := flag.GetAppConfigFilePath(ctx); p != "" {
		paths = append(paths, p, filepath.Join(p, appconfig.DefaultConfigFileName))

		return
	}

	wd := state.WorkingDirectory(ctx)
	paths = append(paths, filepath.Join(wd, appconfig.DefaultConfigFileName))

	return
}

var ErrRequireAppName = fmt.Errorf("the config for your app is missing an app name, add an app field to the fly.toml file or specify with the -a flag`")

// RequireAppName is a Preparer which makes sure the user has selected an
// application name via command line arguments, the environment or an application
// config file (fly.toml). It embeds LoadAppConfigIfPresent.
func RequireAppName(ctx context.Context) (context.Context, error) {
	ctx, err := LoadAppConfigIfPresent(ctx)
	if err != nil {
		return nil, err
	}

	name := flag.GetApp(ctx)
	if name == "" {
		// if there's no flag present, first consult with the environment
		if name = env.First("FLY_APP"); name == "" {
			// and then with the config file (if any)
			if cfg := appconfig.ConfigFromContext(ctx); cfg != nil {
				name = cfg.AppName
			}
		}
	}

	if name == "" {
		return nil, ErrRequireAppName
	}

	return appconfig.WithName(ctx, name), nil
}

// LoadAppNameIfPresent is a Preparer which adds app name if the user has used --app or there appConfig
// but unlike RequireAppName it does not error if the user has not specified an app name.
func LoadAppNameIfPresent(ctx context.Context) (context.Context, error) {
	localCtx, err := RequireAppName(ctx)

	if errors.Is(err, ErrRequireAppName) {
		return appconfig.WithName(ctx, ""), nil
	}

	return localCtx, err
}

func ChangeWorkingDirectoryToFirstArgIfPresent(ctx context.Context) (context.Context, error) {
	wd := flag.FirstArg(ctx)
	if wd == "" {
		return ctx, nil
	}
	return ChangeWorkingDirectory(ctx, wd)
}

func ChangeWorkingDirectory(ctx context.Context, wd string) (context.Context, error) {
	if !filepath.IsAbs(wd) {
		p, err := filepath.Abs(wd)
		if err != nil {
			return nil, fmt.Errorf("failed converting %s to an absolute path: %w", wd, err)
		}
		wd = p
	}

	if err := os.Chdir(wd); err != nil {
		return nil, fmt.Errorf("failed changing working directory: %w", err)
	}

	return state.WithWorkingDirectory(ctx, wd), nil
}
