package migrate_to_v2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/Khan/genqlient/graphql"
	"github.com/briandowns/spinner"
	"github.com/jpillora/backoff"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/superfly/flyctl/api"
	"github.com/superfly/flyctl/client"
	"github.com/superfly/flyctl/flaps"
	"github.com/superfly/flyctl/gql"
	"github.com/superfly/flyctl/internal/appconfig"
	"github.com/superfly/flyctl/internal/command"
	"github.com/superfly/flyctl/internal/command/deploy"
	"github.com/superfly/flyctl/internal/flag"
	"github.com/superfly/flyctl/internal/machine"
	"github.com/superfly/flyctl/internal/prompt"
	"github.com/superfly/flyctl/internal/render"
	"github.com/superfly/flyctl/internal/sentry"
	"github.com/superfly/flyctl/internal/state"
	"github.com/superfly/flyctl/iostreams"
	"github.com/superfly/flyctl/terminal"
)

func New() *cobra.Command {
	return newMigrateToV2()
}

func newMigrateToV2() *cobra.Command {
	const (
		usage = `migrate-to-v2`
		short = `Migrate a v1 app to v2`
		long  = `Migrates an Apps V1 (Nomad) app to the Apps V2 (machines) platform`
	)
	cmd := command.New(
		usage, short, long, runMigrateToV2,
		command.RequireSession, command.RequireAppName,
	)
	cmd.Hidden = true // FIXME: remove this when we're ready to announce
	cmd.Args = cobra.NoArgs
	flag.Add(cmd,
		flag.Yes(),
		flag.App(),
		flag.AppConfig(),
	)
	return cmd
}

func runMigrateToV2(ctx context.Context) error {
	var (
		err error

		appName = appconfig.NameFromContext(ctx)
	)
	migrator, err := NewV2PlatformMigrator(ctx, appName)
	if err != nil {
		return err
	}
	if !flag.GetYes(ctx) {
		confirm, err := migrator.ConfirmChanges(ctx)
		if err != nil {
			return err
		}
		if !confirm {
			return nil
		}
	}
	err = migrator.Migrate(ctx)
	if err != nil {
		return err
	}
	return nil
}

type V2PlatformMigrator interface {
	ConfirmChanges(ctx context.Context) (bool, error)
	Migrate(ctx context.Context) error
}

// FIXME: a lot of stuff is shared with MachineDeployment... might be useful to consolidate the shared stuff into another library/package/something
type v2PlatformMigrator struct {
	apiClient         *api.Client
	flapsClient       *flaps.Client
	gqlClient         graphql.Client
	io                *iostreams.IOStreams
	colorize          *iostreams.ColorScheme
	leaseTimeout      time.Duration
	leaseDelayBetween time.Duration
	appCompact        *api.AppCompact
	appFull           *api.App
	appConfig         *appconfig.Config
	configPath        string
	autoscaleConfig   *api.AutoscalingConfig
	volumeDestination string
	processConfigs    map[string]*appconfig.ProcessConfig
	img               string
	appLock           string
	releaseId         string
	releaseVersion    int
	oldAllocs         []*api.AllocationStatus
	machineGuest      *api.MachineGuest
	oldVmCounts       map[string]int
	newMachinesInput  []*api.LaunchMachineInput
	newMachines       machine.MachineSet
	canAvoidDowntime  bool
	recovery          recoveryState
}

type recoveryState struct {
	machinesCreated        []*api.Machine
	appLocked              bool
	scaledToZero           bool
	platformVersion        string
	onlyPromptToConfigSave bool
}

func NewV2PlatformMigrator(ctx context.Context, appName string) (V2PlatformMigrator, error) {
	var (
		apiClient = client.FromContext(ctx).API()
		io        = iostreams.FromContext(ctx)
		colorize  = io.ColorScheme()
	)
	appCompact, err := apiClient.GetAppCompact(ctx, appName)
	if err != nil {
		return nil, err
	}
	flapsClient, err := flaps.New(ctx, appCompact)
	if err != nil {
		return nil, err
	}
	appFull, err := apiClient.GetApp(ctx, appName)
	if err != nil {
		return nil, err
	}
	if appFull.PlatformVersion == "machines" {
		return nil, fmt.Errorf("the app '%s' is already on the apps v2 platform", appName)
	}
	appConfig, err := determineAppConfigForMachines(ctx)
	if err != nil {
		return nil, err
	}
	processConfigs, err := appConfig.GetProcessConfigs()
	if err != nil {
		return nil, err
	}
	autoscaleConfig, err := apiClient.AppAutoscalingConfig(ctx, appName)
	if err != nil {
		return nil, err
	}
	imageInfo, err := apiClient.GetImageInfo(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("failed to get image info: %w", err)
	}
	img := fmt.Sprintf("%s/%s:%s", imageInfo.ImageDetails.Registry, imageInfo.ImageDetails.Repository, imageInfo.ImageDetails.Tag)
	allocs, err := apiClient.GetAllocations(ctx, appName, false)
	if err != nil {
		return nil, err
	}
	vmSize, _, _, err := apiClient.AppVMResources(ctx, appName)
	if err != nil {
		return nil, err
	}
	machineGuest, err := determineVmSpecs(vmSize)
	if err != nil {
		return nil, err
	}
	leaseTimeout := 13 * time.Second
	leaseDelayBetween := (leaseTimeout - 1*time.Second) / 3
	migrator := &v2PlatformMigrator{
		apiClient:         apiClient,
		flapsClient:       flapsClient,
		gqlClient:         apiClient.GenqClient,
		io:                io,
		colorize:          colorize,
		leaseTimeout:      leaseTimeout,
		leaseDelayBetween: leaseDelayBetween,
		appCompact:        appCompact,
		appFull:           appFull,
		appConfig:         appConfig,
		autoscaleConfig:   autoscaleConfig,
		volumeDestination: appConfig.MountsDestination(),
		processConfigs:    processConfigs,
		img:               img,
		oldAllocs:         allocs,
		machineGuest:      machineGuest,
		canAvoidDowntime:  true,
		recovery: recoveryState{
			platformVersion: appFull.PlatformVersion,
		},
	}
	err = migrator.validate(ctx)
	if err != nil {
		return nil, err
	}
	err = migrator.determinePrimaryRegion(ctx)
	if err != nil {
		return nil, err
	}
	err = migrator.prepMachinesToCreate(ctx)
	if err != nil {
		return nil, err
	}
	err = migrator.determineConfigPath(ctx)
	if err != nil {
		return nil, err
	}
	migrator.resolveProcessGroups(ctx)
	return migrator, nil
}

func (m *v2PlatformMigrator) rollback(ctx context.Context, tb *render.TextBlock) error {

	defer func() {
		if m.recovery.appLocked {
			tb.Detail("Unlocking application")
			err := m.unlockApp(ctx)
			if err != nil {
				fmt.Fprintf(m.io.ErrOut, "failed to unlock app: %v\n", err)
			}
		}
		// HACK: machines apps use the suspended state to describe an app with no machines.
		//       this means something different in nomad-land, so we resume just in case this got set.
		_, err := m.apiClient.ResumeApp(ctx, m.appCompact.Name)
		if err != nil {
			if !strings.Contains(err.Error(), "not suspended") {
				fmt.Fprintf(m.io.ErrOut, "failed to unsuspend app: %v\n", err)
			}
		}
	}()

	if len(m.recovery.machinesCreated) > 0 {
		tb.Detailf("Removing machines")
		for _, mach := range m.recovery.machinesCreated {

			input := api.RemoveMachineInput{
				AppID: m.appFull.Name,
				ID:    mach.ID,
				Kill:  true,
			}
			err := m.flapsClient.Destroy(ctx, input)
			if err != nil {
				return err
			}
		}
	}
	if m.recovery.platformVersion != "nomad" {

		tb.Detailf("Setting platform version to 'nomad'")
		err := m.updateAppPlatformVersion(ctx, "nomad")
		if err != nil {
			return err
		}
	}
	if m.recovery.scaledToZero && len(m.oldAllocs) > 0 {
		// Restore nomad allocs
		tb.Detail("Restoring nomad allocs to their previous state")

		input := gql.SetVMCountInput{
			AppId: m.appConfig.AppName,
			GroupCounts: lo.MapToSlice(m.oldVmCounts, func(name string, count int) gql.VMCountInput {
				return gql.VMCountInput{Group: name, Count: count}
			}),
			LockId: lo.Ternary(m.recovery.appLocked, m.appLock, ""),
		}

		_, err := gql.SetNomadVMCount(ctx, m.gqlClient, input)
		if err != nil {
			return err
		}
	}
	tb.Detail("Successfully recovered")
	return nil
}

func (m *v2PlatformMigrator) Migrate(ctx context.Context) (err error) {

	tb := render.NewTextBlock(ctx, fmt.Sprintf("Migrating %s to the V2 platform", m.appCompact.Name))

	m.recovery.platformVersion = m.appFull.PlatformVersion

	abortedErr := errors.New("migration aborted by user")
	defer func() {
		if err != nil {

			if m.recovery.onlyPromptToConfigSave {
				fmt.Fprintf(m.io.ErrOut, "Failed to save application config to disk, but migration was successful.\n")
				fmt.Fprintf(m.io.ErrOut, "Please run `fly config save` before further interacting with your app via flyctl.\n")
				return
			}

			header := ""
			if err == abortedErr {
				header = "(!) Received abort signal, restoring application to stable state..."
			} else {
				header = "(!) An error has occurred. Attempting to rollback changes..."
			}
			recoveryBlock := render.NewTextBlock(ctx, header)
			if recoveryErr := m.rollback(ctx, recoveryBlock); recoveryErr != nil {
				fmt.Fprintf(m.io.ErrOut, "failed while rolling back application: %v\n", recoveryErr)
			}
		}
	}()

	aborted := atomic.Bool{}
	// Hook into Ctrl+C so that aborting the migration
	// leaves the app in a stable, unlocked, non-detached state
	{
		signalCh := make(chan os.Signal, 1)
		abortSignals := []os.Signal{os.Interrupt}
		if runtime.GOOS != "windows" {
			abortSignals = append(abortSignals, syscall.SIGTERM)
		}
		// Prevent ctx from being cancelled, we need it to do recovery operations
		signal.Reset(abortSignals...)
		signal.Notify(signalCh, abortSignals...)
		go func() {
			<-signalCh
			// most terminals print ^C, this makes things easier to read.
			fmt.Fprintf(m.io.ErrOut, "\n")
			aborted.Store(true)
		}()
	}

	tb.Detail("Locking app to prevent changes during the migration")
	err = m.lockAppForMigration(ctx)
	if err != nil {
		return err
	}
	if aborted.Load() {
		return abortedErr
	}

	if !m.canAvoidDowntime {
		tb.Detail("Scaling down to zero VMs. This will cause temporary downtime until new VMs come up.")

		err = m.scaleNomadToZero(ctx)
		if err != nil {
			return err
		}
	}
	if aborted.Load() {
		return abortedErr
	}

	tb.Detail("Enabling machine creation on app")

	err = m.updateAppPlatformVersion(ctx, "detached")
	if err != nil {
		return err
	}
	if aborted.Load() {
		return abortedErr
	}

	tb.Detail("Creating an app release to register this migration")

	err = m.createRelease(ctx)
	if err != nil {
		return err
	}
	if aborted.Load() {
		return abortedErr
	}

	tb.Detail("Starting machines")

	err = m.createMachines(ctx)
	if err != nil {
		return err
	}
	if aborted.Load() {
		return abortedErr
	}

	err = m.newMachines.AcquireLeases(ctx, m.leaseTimeout)
	defer func() {
		err := m.newMachines.ReleaseLeases(ctx)
		if err != nil {
			terminal.Warnf("error releasing leases on machines: %v\n", err)
		}
	}()
	if err != nil {
		return err
	}
	if aborted.Load() {
		return abortedErr
	}
	m.newMachines.StartBackgroundLeaseRefresh(ctx, m.leaseTimeout, m.leaseDelayBetween)

	err = m.unlockApp(ctx)
	if err != nil {
		return err
	}
	if aborted.Load() {
		return abortedErr
	}

	m.newMachines.ReleaseLeases(ctx)
	err = m.deployApp(ctx)
	if err != nil {
		return err
	}
	if aborted.Load() {
		return abortedErr
	}

	if m.canAvoidDowntime {
		tb.Detail("Scaling down to zero nomad VMs now that machines are running.")

		err = m.scaleNomadToZero(ctx)
		if err != nil {
			return err
		}
		if aborted.Load() {
			return abortedErr
		}
	}

	tb.Detail("Updating the app platform platform type from V1 to V2")

	err = m.updateAppPlatformVersion(ctx, "machines")
	if err != nil {
		return err
	}

	tb.Detail("Saving new configuration")

	m.recovery.onlyPromptToConfigSave = true
	err = m.appConfig.WriteToDisk(ctx, m.configPath)
	if err != nil {
		return err
	}

	tb.Done("Done")
	return nil
}

func (m *v2PlatformMigrator) validate(ctx context.Context) error {
	var err error
	err, _ = m.appConfig.ValidateForMachinesPlatform(ctx)
	if err != nil {
		return fmt.Errorf("failed to validate config for Apps V2 platform: %w", err)
	}
	err = m.validateScaling(ctx)
	if err != nil {
		return nil
	}
	err = m.validateVolumes(ctx)
	if err != nil {
		return err
	}
	err = m.validateProcessGroupsOnAllocs(ctx)
	if err != nil {
		return err
	}
	return nil
}

func (m *v2PlatformMigrator) validateScaling(ctx context.Context) error {
	// FIXME: for now we fail if there is autoscaling.. remove this once we create any extra machines based on autoscaling config
	if m.autoscaleConfig.Enabled {
		return fmt.Errorf("cannot migrate app %s with autoscaling config, yet; watch https://community.fly.io for announcements about autoscale support with migrations", m.appCompact.Name)
	}
	return nil
}

func (m *v2PlatformMigrator) validateVolumes(ctx context.Context) error {
	// FIXME: for now we fail if there are volumes... remove this once we figure out volumes
	// When we can migrate apps with volumes, you probably want to set `m.canAvoidDowntime`
	// to false when the app has volumes.
	if m.appConfig.Mounts != nil {
		return fmt.Errorf("cannot migrate app %s with [mounts] configuration, yet; watch https://community.fly.io for announcements about volume support with migrations", m.appCompact.Name)
	}
	for _, a := range m.oldAllocs {
		if len(a.AttachedVolumes.Nodes) > 0 {
			return fmt.Errorf("cannot migrate app %s because alloc %s has a volume attached; watch https://community.fly.io for announcements about volume support with migrations", m.appCompact.Name, a.IDShort)
		}
	}
	return nil
}

func (m *v2PlatformMigrator) validateProcessGroupsOnAllocs(ctx context.Context) error {
	knownProcGroupsStr := strings.Join(lo.Keys(m.processConfigs), ", ")
	for _, a := range m.oldAllocs {
		if _, exists := m.processConfigs[a.TaskName]; !exists {
			return fmt.Errorf("alloc %s has process group '%s' that is not present in app configuration fly.toml; known process groups are: %s", a.IDShort, a.TaskName, knownProcGroupsStr)
		}
	}
	return nil
}

func (m *v2PlatformMigrator) lockAppForMigration(ctx context.Context) error {
	_ = `# @genqlient
	mutation LockApp($input:LockAppInput!) {
        lockApp(input:$input) {
			lockId
			expiration
        }
	}
	`
	input := gql.LockAppInput{
		AppId: m.appConfig.AppName,
	}
	resp, err := gql.LockApp(ctx, m.gqlClient, input)
	if err != nil {
		return err
	}

	m.appLock = resp.LockApp.LockId
	m.recovery.appLocked = true
	return nil
}

func (m *v2PlatformMigrator) createRelease(ctx context.Context) error {
	_ = `# @genqlient
	mutation MigrateMachinesCreateRelease($input:CreateReleaseInput!) {
		createRelease(input:$input) {
			release {
				id
				version
			}
		}
	}
	`
	input := gql.CreateReleaseInput{
		AppId:           m.appConfig.AppName,
		PlatformVersion: "machines",
		Strategy:        gql.DeploymentStrategy(strings.ToUpper("simple")),
		Definition:      m.appConfig,
		Image:           m.img,
	}
	resp, err := gql.MigrateMachinesCreateRelease(ctx, m.gqlClient, input)
	if err != nil {
		return err
	}
	m.releaseId = resp.CreateRelease.Release.Id
	m.releaseVersion = resp.CreateRelease.Release.Version
	return nil
}

func (m *v2PlatformMigrator) resolveProcessGroups(ctx context.Context) {

	m.oldVmCounts = map[string]int{}
	for _, alloc := range m.oldAllocs {
		m.oldVmCounts[alloc.TaskName] += 1
	}
}

func (m *v2PlatformMigrator) scaleNomadToZero(ctx context.Context) error {

	input := gql.SetVMCountInput{
		AppId:  m.appConfig.AppName,
		LockId: m.appLock,
		GroupCounts: lo.MapToSlice(m.oldVmCounts, func(name string, count int) gql.VMCountInput {
			return gql.VMCountInput{Group: name, Count: 0}
		}),
	}

	if len(input.GroupCounts) > 0 {

		_, err := gql.SetNomadVMCount(ctx, m.gqlClient, input)
		if err != nil {
			return err
		}
	}
	err := m.waitForAllocsZero(ctx)
	if err != nil {
		return err
	}

	m.recovery.scaledToZero = true
	return nil
}

func (m *v2PlatformMigrator) waitForAllocsZero(ctx context.Context) error {

	s := spinner.New(spinner.CharSets[9], 200*time.Millisecond)
	s.Writer = m.io.ErrOut
	s.Prefix = fmt.Sprintf("Waiting for nomad allocs for '%s' to be destroyed ", m.appCompact.Name)
	s.Start()
	defer s.Stop()

	timeout := time.After(1 * time.Hour)
	b := &backoff.Backoff{
		Min:    2 * time.Second,
		Max:    5 * time.Minute,
		Factor: 1.2,
		Jitter: true,
	}
	for {
		select {
		case <-time.After(b.Duration()):
			currentAllocs, err := m.apiClient.GetAllocations(ctx, m.appCompact.Name, false)
			if err != nil {
				return err
			}
			if len(currentAllocs) == 0 {
				return nil
			}
		case <-timeout:
			return errors.New("nomad allocs never reached zero, timed out")
		}
	}
}

func (m *v2PlatformMigrator) updateAppPlatformVersion(ctx context.Context, platform string) error {
	_ = `# @genqlient
	mutation SetPlatformVersion($input:SetPlatformVersionInput!) {
		setPlatformVersion(input:$input) {
			app { id }
		}
	}
	`
	input := gql.SetPlatformVersionInput{
		AppId:           m.appConfig.AppName,
		PlatformVersion: platform,
		LockId:          m.appLock,
	}
	_, err := gql.SetPlatformVersion(ctx, m.gqlClient, input)
	if err != nil {
		return err
	}
	m.recovery.platformVersion = platform
	return nil
}

func (m *v2PlatformMigrator) createMachines(ctx context.Context) error {
	var newlyCreatedMachines []*api.Machine
	defer func() {
		m.recovery.machinesCreated = newlyCreatedMachines
	}()

	for _, machineInput := range m.newMachinesInput {
		newMachine, err := m.flapsClient.Launch(ctx, *machineInput)
		{ // Debug
			currentMachs, err := m.flapsClient.ListActive(ctx)
			currentMachsStr := ""
			if err != nil {
				currentMachsStr = fmt.Sprintf("failed to get machs: %v\n", err)
			} else {
				currentMachsStr = " > " + strings.Join(lo.Map(currentMachs, func(m *api.Machine, _ int) string {
					return m.ID
				}), "\n > ") + "\n"
				if len(currentMachsStr) < 4 {
					currentMachsStr = " none"
				}
			}
			fmt.Fprintf(m.io.Out, "created %v\ncurrent machines:\n%v", newMachine, currentMachsStr)
		}
		if err != nil {
			// FIXME: release app lock,
			return fmt.Errorf("failed creating a machine in region %s: %w", machineInput.Region, err)
		}
		newlyCreatedMachines = append(newlyCreatedMachines, newMachine)
	}
	m.newMachines = machine.NewMachineSet(m.flapsClient, m.io, newlyCreatedMachines)
	return nil
}

func (m *v2PlatformMigrator) unlockApp(ctx context.Context) error {
	_ = `# @genqlient
	mutation UnlockApp($input:UnlockAppInput!) {
		unlockApp(input:$input) {
			app { id }
		}
	}
	`
	input := gql.UnlockAppInput{
		AppId:  m.appConfig.AppName,
		LockId: m.appLock,
	}
	_, err := gql.UnlockApp(ctx, m.gqlClient, input)
	if err != nil {
		return err
	}
	m.recovery.appLocked = false
	return nil
}

func (m *v2PlatformMigrator) deployApp(ctx context.Context) error {
	md, err := deploy.NewMachineDeployment(ctx, deploy.MachineDeploymentArgs{
		AppCompact:  m.appCompact,
		RestartOnly: true,
	})
	if err != nil {
		sentry.CaptureExceptionWithAppInfo(err, "migrate-to-v2", m.appCompact)
		return err
	}
	err = md.DeployMachinesApp(ctx)
	if err != nil {
		sentry.CaptureExceptionWithAppInfo(err, "migrate-to-v2", m.appCompact)
	}
	return nil
}

func (m *v2PlatformMigrator) defaultMachineMetadata() map[string]string {
	res := map[string]string{
		api.MachineConfigMetadataKeyFlyPlatformVersion: api.MachineFlyPlatformVersion2,
		api.MachineConfigMetadataKeyFlyReleaseId:       m.releaseId,
		api.MachineConfigMetadataKeyFlyReleaseVersion:  strconv.Itoa(m.releaseVersion),
		api.MachineConfigMetadataKeyFlyProcessGroup:    api.MachineProcessGroupApp,
	}
	if m.appCompact.IsPostgresApp() {
		res[api.MachineConfigMetadataKeyFlyManagedPostgres] = "true"
	}
	return res
}

func (m *v2PlatformMigrator) prepMachinesToCreate(ctx context.Context) error {
	var err error
	m.newMachinesInput, err = m.resolveMachinesFromAllocs()
	// FIXME: add extra machines that are stopped by default, based on scaling/autoscaling config for the app
	return err
}

func (m *v2PlatformMigrator) resolveMachinesFromAllocs() ([]*api.LaunchMachineInput, error) {
	var res []*api.LaunchMachineInput
	for _, alloc := range m.oldAllocs {
		mach, err := m.resolveMachineFromAlloc(alloc)
		if err != nil {
			return nil, err
		}
		res = append(res, mach)
	}
	return res, nil
}

func (m *v2PlatformMigrator) resolveMachineFromAlloc(alloc *api.AllocationStatus) (*api.LaunchMachineInput, error) {
	launchInput := &api.LaunchMachineInput{
		AppID:   m.appFull.Name,
		OrgSlug: m.appFull.Organization.ID,
		Region:  alloc.Region,
		Config:  &api.MachineConfig{},
	}

	launchInput.Config.Guest = m.machineGuest

	launchInput.Config.Metadata = m.defaultMachineMetadata()
	launchInput.Config.Image = m.img
	launchInput.Config.Env = lo.Assign(m.appConfig.Env)
	if m.appConfig.PrimaryRegion != "" {
		launchInput.Config.Env["PRIMARY_REGION"] = m.appConfig.PrimaryRegion
	}
	launchInput.Config.Metrics = m.appConfig.Metrics
	for _, s := range m.appConfig.Statics {
		launchInput.Config.Statics = append(launchInput.Config.Statics, &api.Static{
			GuestPath: s.GuestPath,
			UrlPrefix: s.UrlPrefix,
		})
	}
	// FIXME: we should probably error out if there are more than 1 vols attached
	if len(alloc.AttachedVolumes.Nodes) == 1 {
		vol := alloc.AttachedVolumes.Nodes[0]
		launchInput.Config.Mounts = []api.MachineMount{{
			Path:   m.volumeDestination,
			Volume: vol.ID,
		}}
	}

	processConfig := m.processConfigs[alloc.TaskName]
	launchInput.Config.Metadata[api.MachineConfigMetadataKeyFlyProcessGroup] = alloc.TaskName
	launchInput.Config.Services = processConfig.Services
	launchInput.Config.Checks = processConfig.Checks
	launchInput.Config.Init.Cmd = lo.Ternary(len(processConfig.Cmd) > 0, processConfig.Cmd, nil)
	return launchInput, nil
}

func (m *v2PlatformMigrator) determinePrimaryRegion(ctx context.Context) error {

	if val, ok := m.appConfig.Env["PRIMARY_REGION"]; ok {
		m.appConfig.PrimaryRegion = val
		return nil
	}

	// TODO: If this ends up used by postgres migrations, it might be nice to have
	//       the prompt here reflect the special role `primary_region` plays for postgres apps

	region, err := prompt.Region(ctx, !m.appFull.Organization.PaidPlan, prompt.RegionParams{
		Message: "Choose the primary region for this app:",
	})
	if err != nil {
		return err
	}
	if region == nil {
		return errors.New("no region provided")
	}
	m.appConfig.PrimaryRegion = region.Code
	return nil
}

func (m *v2PlatformMigrator) determineConfigPath(ctx context.Context) error {
	path := state.WorkingDirectory(ctx)
	if flag.IsSpecified(ctx, "config") {
		path = flag.GetString(ctx, "config")
	}
	configPath, err := appconfig.ResolveConfigFileFromPath(path)
	if err != nil {
		return err
	}

	m.configPath = configPath
	return nil
}

func (m *v2PlatformMigrator) ConfirmChanges(ctx context.Context) (bool, error) {

	numAllocs := len(m.oldAllocs)

	fmt.Fprintf(m.io.Out, "This migration process will do the following, in order:\n")
	fmt.Fprintf(m.io.Out, " * Lock your application, preventing changes during the migration\n")

	printAllocs := func(msgSuffix string) {
		fmt.Fprintf(m.io.Out, " * Remove legacy VMs %s\n", msgSuffix)
		if numAllocs > 0 {
			s := "s"
			if numAllocs == 1 {
				s = ""
			}
			fmt.Fprintf(m.io.Out, "   * Remove %d alloc%s\n", numAllocs, s)
		}
	}

	if !m.canAvoidDowntime {
		printAllocs("")
	}

	fmt.Fprintf(m.io.Out, " * Create machines, copying the configuration of each existing VM\n")
	for name, count := range m.oldVmCounts {
		s := "s"
		if count == 1 {
			s = ""
		}
		fmt.Fprintf(m.io.Out, "   * Create %d \"%s\" machine%s\n", count, name, s)
	}

	if m.canAvoidDowntime {
		printAllocs("after health checks pass for the new machines")
	}

	fmt.Fprintf(m.io.Out, " * Set the application platform version to \"machines\"\n")
	fmt.Fprintf(m.io.Out, " * Unlock your application\n")
	if exists, _ := appconfig.ConfigFileExistsAtPath(m.configPath); exists {
		fmt.Fprintf(m.io.Out, " * Overwrite the config file at '%s'\n", m.configPath)
	} else {
		fmt.Fprintf(m.io.Out, " * Save the app's config file to '%s'\n", m.configPath)
	}

	confirm := false
	prompt := &survey.Confirm{
		Message: "Would you like to continue?",
	}
	err := survey.AskOne(prompt, &confirm)

	return confirm, err
}

func determineAppConfigForMachines(ctx context.Context) (*appconfig.Config, error) {
	var (
		err                error
		appNameFromContext = appconfig.NameFromContext(ctx)
		cfg                = appconfig.ConfigFromContext(ctx)
	)
	if cfg == nil {
		cfg, err = appconfig.FromRemoteApp(ctx, appNameFromContext)
		if err != nil {
			return nil, err
		}
	}
	if appNameFromContext != "" {
		cfg.AppName = appNameFromContext
	}
	return cfg, nil
}

func determineVmSpecs(vmSize api.VMSize) (*api.MachineGuest, error) {

	preset := strings.Replace(vmSize.Name, "dedicated-cpu", "performance", 1)

	guest := &api.MachineGuest{}
	err := guest.SetSize(preset)

	if err != nil {
		return nil, fmt.Errorf("nomad VM definition incompatible with machines API: %w", err)
	}
	guest.MemoryMB = vmSize.MemoryMB

	return guest, nil
}
