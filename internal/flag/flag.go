// Package flag implements flag-related functionality.
package flag

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"github.com/superfly/flyctl/internal/flag/completion"
	"github.com/superfly/flyctl/internal/flag/flagnames"
)

// Flag wraps the set of flags.
type Flag interface {
	addTo(*cobra.Command)
}

type Set []Flag

func (s Set) addTo(cmd *cobra.Command) {
	for _, flag := range s {
		flag.addTo(cmd)
	}
}

// Add adds flag to cmd, binding them on v should v not be nil.
func Add(cmd *cobra.Command, flags ...Flag) {
	for _, flag := range flags {
		flag.addTo(cmd)
	}
}

// Bool wraps the set of boolean flags.
type Bool struct {
	Name        string
	Shorthand   string
	Description string
	Default     bool
	Hidden      bool
}

func (b Bool) addTo(cmd *cobra.Command) {
	flags := cmd.Flags()

	if b.Shorthand != "" {
		_ = flags.BoolP(b.Name, b.Shorthand, b.Default, b.Description)
	} else {
		_ = flags.Bool(b.Name, b.Default, b.Description)
	}

	f := flags.Lookup(b.Name)
	f.Hidden = b.Hidden
}

// String wraps the set of string flags.
type String struct {
	Name         string
	Shorthand    string
	Description  string
	Default      string
	ConfName     string
	EnvName      string
	Hidden       bool
	CompletionFn func(ctx context.Context, cmd *cobra.Command, args []string, partial string) ([]string, error)
}

func (s String) addTo(cmd *cobra.Command) {
	flags := cmd.Flags()

	if s.Shorthand != "" {
		_ = flags.StringP(s.Name, s.Shorthand, s.Default, s.Description)
	} else {
		_ = flags.String(s.Name, s.Default, s.Description)
	}

	f := flags.Lookup(s.Name)
	f.Hidden = s.Hidden
	if s.CompletionFn != nil {
		err := cmd.RegisterFlagCompletionFunc(s.Name, completion.AdaptFn(s.CompletionFn))
		if err != nil {
			panic(err)
		}
	}
}

// Int wraps the set of int flags.
type Int struct {
	Name        string
	Shorthand   string
	Description string
	Default     int
	Hidden      bool
}

func (i Int) addTo(cmd *cobra.Command) {
	flags := cmd.Flags()

	if i.Shorthand != "" {
		_ = flags.IntP(i.Name, i.Shorthand, i.Default, i.Description)
	} else {
		_ = flags.Int(i.Name, i.Default, i.Description)
	}

	f := flags.Lookup(i.Name)
	f.Hidden = i.Hidden
}

// StringSlice wraps the set of string slice flags.
type StringSlice struct {
	Name        string
	Shorthand   string
	Description string
	Default     []string
	ConfName    string
	EnvName     string
	Hidden      bool
}

func (ss StringSlice) addTo(cmd *cobra.Command) {
	flags := cmd.Flags()

	if ss.Shorthand != "" {
		_ = flags.StringSliceP(ss.Name, ss.Shorthand, ss.Default, ss.Description)
	} else {
		_ = flags.StringSlice(ss.Name, ss.Default, ss.Description)
	}

	f := flags.Lookup(ss.Name)
	f.Hidden = ss.Hidden
}

// StringArray wraps the set of string array flags.
type StringArray struct {
	Name        string
	Shorthand   string
	Description string
	Default     []string
	ConfName    string
	EnvName     string
	Hidden      bool
}

func (ss StringArray) addTo(cmd *cobra.Command) {
	flags := cmd.Flags()

	if ss.Shorthand != "" {
		_ = flags.StringArrayP(ss.Name, ss.Shorthand, ss.Default, ss.Description)
	} else {
		_ = flags.StringArray(ss.Name, ss.Default, ss.Description)
	}

	f := flags.Lookup(ss.Name)
	f.Hidden = ss.Hidden
}

// Duration wraps the set of duration flags.
type Duration struct {
	Name        string
	Shorthand   string
	Description string
	Default     time.Duration
	ConfName    string
	EnvName     string
	Hidden      bool
}

func (d Duration) addTo(cmd *cobra.Command) {
	flags := cmd.Flags()

	if d.Shorthand != "" {
		_ = flags.DurationP(d.Name, d.Shorthand, d.Default, d.Description)
	} else {
		_ = flags.Duration(d.Name, d.Default, d.Description)
	}

	f := flags.Lookup(d.Name)
	f.Hidden = d.Hidden
}

// Org returns an org string flag.
func Org() String {
	return String{
		Name:         flagnames.Org,
		Description:  "The target Fly organization",
		Shorthand:    "o",
		CompletionFn: completion.InitFlyApi(completion.CompleteOrgs),
	}
}

// Region returns a region string flag.
func Region() String {
	return String{
		Name:         flagnames.Region,
		Description:  "The target region (see 'flyctl platform regions')",
		Shorthand:    "r",
		CompletionFn: completion.InitFlyApi(completion.CompleteRegions),
	}
}

// Yes returns a yes bool flag.
func Yes() Bool {
	return Bool{
		Name:        flagnames.Yes,
		Shorthand:   "y",
		Description: "Accept all confirmations",
	}
}

// App returns an app string flag.
func App() String {
	return String{
		Name:         flagnames.App,
		Shorthand:    "a",
		Description:  "Application name",
		CompletionFn: completion.InitFlyApi(completion.CompleteApps),
	}
}

// AppConfig returns an app config string flag.
func AppConfig() String {
	return String{
		Name:        flagnames.AppConfigFilePath,
		Shorthand:   "c",
		Description: "Path to application configuration file",
	}
}

// Image returns a Docker image config string flag.
func Image() String {
	return String{
		Name:        flagnames.Image,
		Shorthand:   "i",
		Description: "The Docker image to deploy",
	}
}

// Now returns a boolean flag for deploying immediately
func Now() Bool {
	return Bool{
		Name:        flagnames.Now,
		Description: "Deploy now without confirmation",
		Default:     false,
	}
}

func NoDeploy() Bool {
	return Bool{
		Name:        "no-deploy",
		Description: "Do not prompt for deployment",
	}
}

// GenerateName returns a boolean flag for generating an application name
func GenerateName() Bool {
	return Bool{
		Name:        flagnames.GenerateNameFlag,
		Description: "Always generate a name for the app",
		Default:     false,
	}
}

const remoteOnlyName = "remote-only"

// RemoteOnly returns a boolean flag for deploying remotely
func RemoteOnly(defaultValue bool) Bool {
	return Bool{
		Name:        remoteOnlyName,
		Description: "Perform builds on a remote builder instance instead of using the local docker daemon",
		Default:     defaultValue,
	}
}

func GetRemoteOnly(ctx context.Context) bool {
	return GetBool(ctx, remoteOnlyName)
}

const localOnlyName = "local-only"

// RemoteOnly returns a boolean flag for deploying remotely
func LocalOnly() Bool {
	return Bool{
		Name:        localOnlyName,
		Description: "Only perform builds locally using the local docker daemon",
	}
}

func GetLocalOnly(ctx context.Context) bool {
	return GetBool(ctx, localOnlyName)
}

const detachName = "detach"

// Detach returns a boolean flag for detaching during deployment
func Detach() Bool {
	return Bool{
		Name:        detachName,
		Description: "Return immediately instead of monitoring deployment progress",
	}
}

func GetDetach(ctx context.Context) bool {
	return GetBool(ctx, detachName)
}

const buildOnlyName = "build-only"

// BuildOnly returns a boolean flag for building without a deployment
func BuildOnly() Bool {
	return Bool{
		Name:        buildOnlyName,
		Description: "Build but do not deploy",
	}
}

func GetBuildOnly(ctx context.Context) bool {
	return GetBool(ctx, buildOnlyName)
}

const pushName = "push"

// Push returns a boolean flag to force pushing a build image to the registry
func Push() Bool {
	return Bool{
		Name:        pushName,
		Description: "Push image to registry after build is complete",
		Default:     false,
	}
}

const dockerfileName = "dockerfile"

func Dockerfile() String {
	return String{
		Name:        dockerfileName,
		Description: "Path to a Dockerfile. Defaults to the Dockerfile in the working directory.",
	}
}

const ignorefileName = "ignorefile"

func Ignorefile() String {
	return String{
		Name:        ignorefileName,
		Description: "Path to a Docker ignore file. Defaults to the .dockerignore file in the working directory.",
	}
}

func ImageLabel() String {
	return String{
		Name:        "image-label",
		Description: `Image label to use when tagging and pushing to the fly registry. Defaults to "deployment-{timestamp}".`,
	}
}

func NoCache() Bool {
	return Bool{
		Name:        "no-cache",
		Description: "Do not use the build cache when building the image",
	}
}

func BuildSecret() StringArray {
	return StringArray{
		Name:        "build-secret",
		Description: "Set of build secrets of NAME=VALUE pairs. Can be specified multiple times. See https://docs.docker.com/develop/develop-images/build_enhancements/#new-docker-build-secret-information",
	}
}

func BuildArg() StringArray {
	return StringArray{
		Name:        "build-arg",
		Description: "Set of build time variables in the form of NAME=VALUE pairs. Can be specified multiple times.",
	}
}

func BuildTarget() String {
	return String{
		Name:        "build-target",
		Description: "Set the target build stage to build if the Dockerfile has more than one stage",
	}
}

func Nixpacks() Bool {
	return Bool{
		Name:        "nixpacks",
		Default:     false,
		Description: "Deploy using nixpacks to build the image",
	}
}

func Strategy() String {
	return String{
		Name:        "strategy",
		Description: "The strategy for replacing running instances. Options are canary, rolling, bluegreen, or immediate. Default is canary, or rolling when max-per-region is set.",
	}
}

func JSONOutput() Bool {
	return Bool{
		Name:        flagnames.JSONOutput,
		Shorthand:   "j",
		Description: "JSON output",
		Default:     false,
	}
}
