// Package flag implements flag-related functionality.
package flag

import (
	"context"
	"reflect"
	"time"

	"github.com/spf13/cobra"
)

const (
	// AccessTokenName denotes the name of the access token flag.
	AccessTokenName = "access-token"

	// VerboseName denotes the name of the verbose flag.
	VerboseName = "verbose"

	// JSONOutputName denotes the name of the json output flag.
	JSONOutputName = "json"

	// LocalOnlyName denotes the name of the local-only flag.
	LocalOnlyName = "local-only"

	// OrgName denotes the name of the org flag.
	OrgName = "org"

	// RegionName denotes the name of the region flag.
	RegionName = "region"

	// YesName denotes the name of the yes flag.
	YesName = "yes"

	// AppName denotes the name of the app flag.
	AppName = "app"

	// AppConfigFilePathName denotes the name of the app config file path flag.
	AppConfigFilePathName = "config"

	// ImageName denotes the name of the image flag.
	ImageName = "image"

	// NowName denotes the name of the now flag.
	NowName = "now"

	// NoDeploy denotes the name of the no deploy flag.
	NoDeployName = "no-deploy"

	// GenerateName denotes the name of the generate name flag.
	GenerateNameFlagName = "generate-name"

	// DetachName denotes the name of the detach flag.
	DetachName = "detach"
)

func makeAlias[T any](template T, name string) T {

	var ret T
	value := reflect.ValueOf(&ret).Elem()

	descField := reflect.ValueOf(template).FieldByName("Description")
	if descField.IsValid() {
		value.FieldByName("Description").SetString(descField.String())
	}

	nameField := value.FieldByName("Name")
	if nameField.IsValid() {
		nameField.SetString(name)
	}

	hiddenField := value.FieldByName("Hidden")
	if hiddenField.IsValid() {
		hiddenField.SetBool(true)
	}
	return ret
}

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
	Aliases     []string
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

	for _, name := range b.Aliases {
		makeAlias(b, name).addTo(cmd)
	}
	err := cmd.Flags().SetAnnotation(f.Name, "flyctl_alias", b.Aliases)
	if err != nil {
		panic(err)
	}
}

// String wraps the set of string flags.
type String struct {
	Name        string
	Shorthand   string
	Description string
	Default     string
	ConfName    string
	EnvName     string
	Hidden      bool
	Aliases     []string
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

	for _, name := range s.Aliases {
		makeAlias(s, name).addTo(cmd)
	}
	err := cmd.Flags().SetAnnotation(f.Name, "flyctl_alias", s.Aliases)
	if err != nil {
		panic(err)
	}
}

// Int wraps the set of int flags.
type Int struct {
	Name        string
	Shorthand   string
	Description string
	Default     int
	Hidden      bool
	Aliases     []string
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

	for _, name := range i.Aliases {
		makeAlias(i, name).addTo(cmd)
	}
	err := cmd.Flags().SetAnnotation(f.Name, "flyctl_alias", i.Aliases)
	if err != nil {
		panic(err)
	}
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
	Aliases     []string
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

	for _, name := range ss.Aliases {
		makeAlias(ss, name).addTo(cmd)
	}
	err := cmd.Flags().SetAnnotation(f.Name, "flyctl_alias", ss.Aliases)
	if err != nil {
		panic(err)
	}
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
	Aliases     []string
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

	for _, name := range ss.Aliases {
		makeAlias(ss, name).addTo(cmd)
	}
	err := cmd.Flags().SetAnnotation(f.Name, "flyctl_alias", ss.Aliases)
	if err != nil {
		panic(err)
	}
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
	Aliases     []string
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

	for _, name := range d.Aliases {
		makeAlias(d, name).addTo(cmd)
	}
	err := cmd.Flags().SetAnnotation(f.Name, "flyctl_alias", d.Aliases)
	if err != nil {
		panic(err)
	}
}

// Org returns an org string flag.
func Org() String {
	return String{
		Name:        OrgName,
		Description: "The target Fly organization",
		Shorthand:   "o",
	}
}

// Region returns a region string flag.
func Region() String {
	return String{
		Name:        RegionName,
		Description: "The target region (see 'flyctl platform regions')",
		Shorthand:   "r",
	}
}

// Yes returns a yes bool flag.
func Yes() Bool {
	return Bool{
		Name:        YesName,
		Shorthand:   "y",
		Description: "Accept all confirmations",
	}
}

// App returns an app string flag.
func App() String {
	return String{
		Name:        AppName,
		Shorthand:   "a",
		Description: "Application name",
	}
}

// AppConfig returns an app config string flag.
func AppConfig() String {
	return String{
		Name:        AppConfigFilePathName,
		Shorthand:   "c",
		Description: "Path to application configuration file",
	}
}

// Image returns a Docker image config string flag.
func Image() String {
	return String{
		Name:        ImageName,
		Shorthand:   "i",
		Description: "The Docker image to deploy",
	}
}

// Now returns a boolean flag for deploying immediately
func Now() Bool {
	return Bool{
		Name:        NowName,
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
		Name:        GenerateNameFlagName,
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
		Name:        JSONOutputName,
		Shorthand:   "j",
		Description: "JSON output",
		Default:     false,
	}
}
