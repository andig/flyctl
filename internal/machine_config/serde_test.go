package machine_config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/superfly/flyctl/api"
)

func TestLoadTOMLAppConfigWithAppName(t *testing.T) {
	const path = "./testdata/app-name.toml"

	p, err := LoadConfig(path)
	assert.NoError(t, err)
	assert.Equal(t, p.AppName, "test-app")
}

func TestLoadTOMLAppConfigWithBuilderName(t *testing.T) {
	const path = "./testdata/build.toml"

	p, err := LoadConfig(path)
	assert.NoError(t, err)
	assert.Equal(t, p.Build.Builder, "builder/name")
}

func TestLoadTOMLAppConfigWithImage(t *testing.T) {
	const path = "./testdata/image.toml"

	p, err := LoadConfig(path)
	assert.NoError(t, err)
	assert.Equal(t, p.Build.Image, "image/name")
}

func TestLoadTOMLAppConfigWithDockerfile(t *testing.T) {
	const path = "./testdata/docker.toml"

	p, err := LoadConfig(path)
	assert.NoError(t, err)
	assert.Equal(t, p.Build.Dockerfile, "./Dockerfile")
}

func TestLoadTOMLAppConfigWithBuilderNameAndArgs(t *testing.T) {
	const path = "./testdata/build-with-args.toml"

	p, err := LoadConfig(path)
	assert.NoError(t, err)
	assert.Equal(t, p.Build.Args, map[string]string{"A": "B", "C": "D"})
}

func TestLoadTOMLAppConfigOldFormat(t *testing.T) {
	const path = "./testdata/old-format.toml"
	cfg, err := LoadConfig(path)
	assert.NoError(t, err)
	assert.Equal(t, &Config{
		FlyTomlPath: "./testdata/old-format.toml",
		AppName:     "foo",
		Env: map[string]string{
			"FOO": "STRING",
			"BAR": "123",
		},
		Services: []Service{
			{
				InternalPort: 8080,
				Ports: []api.MachinePort{
					{
						Port:     api.Pointer(80),
						Handlers: []string{"http"},
					},
				},
				Concurrency: &api.MachineServiceConcurrency{
					Type:      "requests",
					HardLimit: 23,
					SoftLimit: 12,
				},
				TCPChecks: []*ServiceTCPCheck{
					{
						Interval: api.MustParseDuration("10s"),
						Timeout:  api.MustParseDuration("2s"),
					},
					{
						Interval: api.MustParseDuration("20s"),
						Timeout:  api.MustParseDuration("3s"),
					},
				},
				HTTPChecks: []*ServiceHTTPCheck{
					{
						Interval: api.MustParseDuration("30s"),
						Timeout:  api.MustParseDuration("4s"),
					},
					{
						Interval: api.MustParseDuration("20s"),
						Timeout:  api.MustParseDuration("3s"),
					},
				},
			},
		},
	}, cfg)
}

func TestLoadTOMLAppConfigReferenceFormat(t *testing.T) {
	const path = "./testdata/full-reference.toml"
	cfg, err := LoadConfig(path)
	assert.NoError(t, err)
	assert.Equal(t, &Config{
		FlyTomlPath:   "./testdata/full-reference.toml",
		AppName:       "foo",
		KillSignal:    "SIGTERM",
		KillTimeout:   3,
		PrimaryRegion: "sea",
		Experimental: &Experimental{
			Cmd:          []string{"cmd"},
			Entrypoint:   []string{"entrypoint"},
			Exec:         []string{"exec"},
			AutoRollback: true,
			EnableConsul: true,
			EnableEtcd:   true,
		},

		Build: &Build{
			Builder:           "dockerfile",
			Image:             "foo/fighter",
			Builtin:           "whatisthis",
			Dockerfile:        "Dockerfile",
			Ignorefile:        ".gitignore",
			DockerBuildTarget: "target",
			Buildpacks:        []string{"packme", "well"},
			Settings: map[string]any{
				"foo":   "bar",
				"other": float64(2),
			},

			Args: map[string]string{
				"param1": "value1",
				"param2": "value2",
			},
		},

		Deploy: &Deploy{
			ReleaseCommand: "release command",
			Strategy:       "rolling-eyes",
		},

		Env: map[string]string{
			"FOO": "BAR",
		},

		Metrics: &api.MachineMetrics{
			Port: 9999,
			Path: "/metrics",
		},

		HttpService: &HTTPService{
			InternalPort: 8080,
			ForceHttps:   true,
			Concurrency: &api.MachineServiceConcurrency{
				Type:      "donuts",
				HardLimit: 10,
				SoftLimit: 4,
			},
		},

		Statics: []Static{
			{
				GuestPath: "/path/to/statics",
				UrlPrefix: "/static-assets",
			},
		},

		Mounts: &Volume{
			Source:      "data",
			Destination: "/data",
		},

		Processes: map[string]string{
			"web":  "run web",
			"task": "task all day",
		},

		Checks: map[string]*ToplevelCheck{
			"status": {
				Port:              api.Pointer(2020),
				Type:              api.Pointer("http"),
				Interval:          api.MustParseDuration("10s"),
				Timeout:           api.MustParseDuration("2s"),
				HTTPMethod:        api.Pointer("GET"),
				HTTPPath:          api.Pointer("/status"),
				HTTPProtocol:      api.Pointer("https"),
				HTTPTLSSkipVerify: api.Pointer(true),
				HTTPHeaders: map[string]string{
					"Content-Type":  "application/json",
					"Authorization": "super-duper-secret",
				},
			},
		},

		Services: []Service{
			{
				InternalPort: 8081,
				Protocol:     "tcp",
				Processes:    []string{"app"},

				Concurrency: &api.MachineServiceConcurrency{
					Type:      "requests",
					HardLimit: 22,
					SoftLimit: 13,
				},

				Ports: []api.MachinePort{
					{
						Port:       api.Pointer(80),
						StartPort:  api.Pointer(100),
						EndPort:    api.Pointer(200),
						Handlers:   []string{"https"},
						ForceHttps: true,
					},
				},

				TCPChecks: []*ServiceTCPCheck{
					{
						Interval:     api.MustParseDuration("21s"),
						Timeout:      api.MustParseDuration("4s"),
						GracePeriod:  api.MustParseDuration("1s"),
						RestartLimit: 3,
					},
				},

				HTTPChecks: []*ServiceHTTPCheck{
					{
						Interval:          api.MustParseDuration("81s"),
						Timeout:           api.MustParseDuration("7s"),
						GracePeriod:       api.MustParseDuration("2s"),
						RestartLimit:      4,
						HTTPMethod:        api.Pointer("GET"),
						HTTPPath:          api.Pointer("/"),
						HTTPProtocol:      api.Pointer("https"),
						HTTPTLSSkipVerify: api.Pointer(true),
						HTTPHeaders: map[string]string{
							"My-Custom-Header": "whatever",
						},
					},
					{
						Interval:   api.MustParseDuration("33s"),
						Timeout:    api.MustParseDuration("10s"),
						HTTPMethod: api.Pointer("POST"),
						HTTPPath:   api.Pointer("/check2"),
					},
				},
			},
		},
	}, cfg)
}
