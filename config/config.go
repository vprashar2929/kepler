// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"gopkg.in/yaml.v3"

	"k8s.io/utils/ptr"
)

// Feature represents an experimental feature identifier
type Feature string

const (
	// ExperimentalRedfishFeature represents the Redfish BMC power monitoring feature
	ExperimentalRedfishFeature Feature = "redfish"

	// ExperimentalGPUFeature represents the GPU power monitoring feature
	ExperimentalGPUFeature Feature = "gpu"

	// PrometheusFeature represents the Prometheus exporter feature
	PrometheusFeature Feature = "prometheus"

	// StdoutFeature represents the stdout exporter feature
	StdoutFeature Feature = "stdout"

	// PprofFeature represents the pprof debug endpoints feature
	PprofFeature Feature = "pprof"
)

// Config represents the complete application configuration
type (
	Log struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	}
	Host struct {
		SysFS  string `yaml:"sysfs"`
		ProcFS string `yaml:"procfs"`
	}

	// Rapl configuration
	Rapl struct {
		Zones []string `yaml:"zones"`
	}

	// Development mode settings; disabled by default
	Dev struct {
		FakeCpuMeter struct {
			Enabled *bool    `yaml:"enabled"`
			Zones   []string `yaml:"zones"`
		} `yaml:"fake-cpu-meter"`
		FakeGpuMeter struct {
			Enabled    *bool   `yaml:"enabled"`
			Devices    []uint  `yaml:"devices"`
			PowerBase  float64 `yaml:"powerBase"`  // Base power consumption in watts
			PowerRange float64 `yaml:"powerRange"` // Power variation range in watts
			EnergyStep float64 `yaml:"energyStep"` // Energy increment per update in joules
		} `yaml:"fake-gpu-meter"`
	}
	Web struct {
		Config          string   `yaml:"configFile"`
		ListenAddresses []string `yaml:"listenAddresses"`
	}

	Monitor struct {
		Interval  time.Duration `yaml:"interval"`  // Interval for monitoring resources
		Staleness time.Duration `yaml:"staleness"` // Time after which calculated values are considered stale

		// MaxTerminated controls terminated workload tracking behavior:
		// <0: Any negative value indicates to track unlimited terminated workloads (no capacity limit)
		// =0: Disable terminated workload tracking completely
		// >0: Track top N terminated workloads by energy consumption
		MaxTerminated int `yaml:"maxTerminated"`

		// MinTerminatedEnergyThreshold sets the minimum energy consumption threshold for terminated workloads
		// Only terminated workloads with energy consumption above this threshold will be tracked
		// Value is in joules (e.g., 10 = 10 joules)
		// TODO: Add support for parsing energy units like "10J", "500mJ", "2kJ"
		MinTerminatedEnergyThreshold int64 `yaml:"minTerminatedEnergyThreshold"`
	}

	// Exporter configuration
	StdoutExporter struct {
		Enabled *bool `yaml:"enabled"`
	}

	PrometheusExporter struct {
		Enabled         *bool    `yaml:"enabled"`
		DebugCollectors []string `yaml:"debugCollectors"`
		MetricsLevel    Level    `yaml:"metricsLevel"`
	}

	Exporter struct {
		Stdout     StdoutExporter     `yaml:"stdout"`
		Prometheus PrometheusExporter `yaml:"prometheus"`
	}

	// Debug configuration
	PprofDebug struct {
		Enabled *bool `yaml:"enabled"`
	}

	Debug struct {
		Pprof PprofDebug `yaml:"pprof"`
	}

	Kube struct {
		Enabled *bool  `yaml:"enabled"`
		Config  string `yaml:"config"`
		Node    string `yaml:"nodeName"`
	}

	// Platform contains settings for platform power monitoring
	Platform struct {
		Redfish Redfish `yaml:"redfish"`
		GPU     GPU     `yaml:"gpu"`
	}

	// Redfish contains settings for Redfish BMC power monitoring
	Redfish struct {
		Enabled     *bool         `yaml:"enabled"`
		NodeName    string        `yaml:"nodeName"`
		ConfigFile  string        `yaml:"configFile"`
		HTTPTimeout time.Duration `yaml:"httpTimeout"` // HTTP client timeout for BMC requests
	}

	// GPU contains settings for GPU power monitoring
	GPU struct {
		Enabled    *bool         `yaml:"enabled"`
		Type       string        `yaml:"type"`       // "dcgm" or "fake"
		Devices    []uint        `yaml:"devices"`    // GPU device IDs to monitor
		UpdateFreq time.Duration `yaml:"updateFreq"` // Update frequency for GPU metrics

		// DCGM-specific settings
		DCGMMode    string `yaml:"dcgmMode"`    // "embedded" or "standalone" (default: "embedded")
		DCGMAddress string `yaml:"dcgmAddress"` // Address for standalone mode (e.g., "dcgm-exporter:5555" or unix socket path)
	}

	// Experimental contains experimental features (no stability guarantees)
	Experimental struct {
		Platform Platform `yaml:"platform"`
	}

	Config struct {
		Log      Log      `yaml:"log"`
		Host     Host     `yaml:"host"`
		Monitor  Monitor  `yaml:"monitor"`
		Rapl     Rapl     `yaml:"rapl"`
		Exporter Exporter `yaml:"exporter"`
		Web      Web      `yaml:"web"`
		Debug    Debug    `yaml:"debug"`
		Dev      Dev      `yaml:"dev"` // WARN: do not expose dev settings as flags
		Kube     Kube     `yaml:"kube"`

		// NOTE: Experimental field is a pointer on purpose to
		// use omitempty to suppress printing (String) Experimental configuration
		// when it is empty
		Experimental *Experimental `yaml:"experimental,omitempty"`
	}
)

// MetricsLevelValue is a custom kingpin.Value that parses metrics levels directly into metrics.Level
type MetricsLevelValue struct {
	level *Level
}

// NewMetricsLevelValue creates a new MetricsLevelValue with the given target
func NewMetricsLevelValue(target *Level) *MetricsLevelValue {
	return &MetricsLevelValue{level: target}
}

// Set implements kingpin.Value interface - parses and accumulates metrics levels
func (m *MetricsLevelValue) Set(value string) error {
	// Parse the single value into a level
	level, err := ParseLevel([]string{value})
	if err != nil {
		return err
	}

	// If this is the first value, initialize to 0 first to clear any default
	allLevels := MetricsLevelAll
	if *m.level == allLevels {
		*m.level = 0
	}

	// Accumulate the level using bitwise OR
	*m.level |= level
	return nil
}

// String implements kingpin.Value interface
func (m *MetricsLevelValue) String() string {
	return m.level.String()
}

// IsCumulative implements kingpin.Value interface to support multiple values
func (m *MetricsLevelValue) IsCumulative() bool {
	return true
}

type SkipValidation int

const (
	SkipHostValidation         SkipValidation = 1
	SkipExperimentalValidation SkipValidation = 2
)

const (
	// Flags
	LogLevelFlag  = "log.level"
	LogFormatFlag = "log.format"

	HostSysFSFlag  = "host.sysfs"
	HostProcFSFlag = "host.procfs"

	MonitorIntervalFlag      = "monitor.interval"
	MonitorStaleness         = "monitor.staleness" // not a flag
	MonitorMaxTerminatedFlag = "monitor.max-terminated"

	// RAPL
	RaplZones = "rapl.zones" // not a flag

	pprofEnabledFlag = "debug.pprof"

	WebConfigFlag        = "web.config-file"
	WebListenAddressFlag = "web.listen-address"

	// Exporters
	ExporterStdoutEnabledFlag = "exporter.stdout"

	ExporterPrometheusEnabledFlag = "exporter.prometheus"
	// NOTE: not a flag
	ExporterPrometheusDebugCollectors = "exporter.prometheus.debug-collectors"
	ExporterPrometheusMetricsFlag     = "metrics"

	// kubernetes flags
	KubernetesFlag   = "kube.enable"
	KubeConfigFlag   = "kube.config"
	KubeNodeNameFlag = "kube.node-name"

	// Experimental Platform flags
	ExperimentalPlatformRedfishEnabledFlag  = "experimental.platform.redfish.enabled"
	ExperimentalPlatformRedfishNodeNameFlag = "experimental.platform.redfish.node-name"
	ExperimentalPlatformRedfishConfigFlag   = "experimental.platform.redfish.config-file"

	// Experimental GPU power monitoring flags
	ExperimentalPlatformGPUEnabledFlag    = "experimental.platform.gpu.enabled"
	ExperimentalPlatformGPUTypeFlag       = "experimental.platform.gpu.type"
	ExperimentalPlatformGPUDevicesFlag    = "experimental.platform.gpu.devices"
	ExperimentalPlatformGPUUpdateFreqFlag = "experimental.platform.gpu.update-freq"
	ExperimentalPlatformGPUDCGMModeFlag   = "experimental.platform.gpu.dcgm-mode"
	ExperimentalPlatformGPUDCGMAddrFlag   = "experimental.platform.gpu.dcgm-address"

// WARN:  dev settings shouldn't be exposed as flags as flags are intended for end users
)

// DefaultConfig returns a Config with default values
func DefaultConfig() *Config {
	cfg := &Config{
		Log: Log{
			Level:  "info",
			Format: "text",
		},
		Host: Host{
			SysFS:  "/sys",
			ProcFS: "/proc",
		},
		Rapl: Rapl{
			Zones: []string{},
		},
		Monitor: Monitor{
			Interval:  5 * time.Second,
			Staleness: 500 * time.Millisecond,

			MaxTerminated:                500,
			MinTerminatedEnergyThreshold: 10, // 10 Joules
		},
		Exporter: Exporter{
			Stdout: StdoutExporter{
				Enabled: ptr.To(false),
			},
			Prometheus: PrometheusExporter{
				Enabled:         ptr.To(true),
				DebugCollectors: []string{"go"},
				MetricsLevel:    MetricsLevelAll,
			},
		},
		Debug: Debug{
			Pprof: PprofDebug{
				Enabled: ptr.To(false),
			},
		},
		Web: Web{
			ListenAddresses: []string{":28282"},
		},
		Kube: Kube{
			Enabled: ptr.To(false),
		},

		// NOTE: Experimental config will be nil by default and only allocated when needed
		// to avoid printing the configs if experimental features are disabled
		// see use of `omitempty`
	}

	cfg.Dev.FakeCpuMeter.Enabled = ptr.To(false)
	cfg.Dev.FakeGpuMeter.Enabled = ptr.To(false)
	cfg.Dev.FakeGpuMeter.Devices = []uint{0}
	cfg.Dev.FakeGpuMeter.PowerBase = 100.0
	cfg.Dev.FakeGpuMeter.PowerRange = 50.0
	cfg.Dev.FakeGpuMeter.EnergyStep = 1000.0
	return cfg
}

// Load loads configuration from an io.Reader
func Load(r io.Reader) (*Config, error) {
	cfg := DefaultConfig()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	cfg.sanitize()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// FromFile loads configuration from a file
func FromFile(filePath string) (*Config, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	var errRet error
	defer func() {
		err = file.Close()
		if err != nil && errRet == nil {
			errRet = err
		}
	}()

	cfg, errRet := Load(file)

	return cfg, errRet
}

type ConfigUpdaterFn func(*Config) error

// RegisterFlags registers command-line flags with kingpin app
// and returns ConfigUpdaterFn that updates the config from parsed flags
// as command line arguments override config file settings
func RegisterFlags(app *kingpin.Application) ConfigUpdaterFn {
	// track flags that were explicitly set
	flagsSet := map[string]bool{}

	app.PreAction(func(ctx *kingpin.ParseContext) error {
		// Clear the map in case this function is called multiple times
		flagsSet = map[string]bool{}

		for _, element := range ctx.Elements {
			if flag, ok := element.Clause.(*kingpin.FlagClause); ok && element.Value != nil {
				flagsSet[flag.Model().Name] = true
			}
		}
		return nil
	})

	// Logging
	logLevel := app.Flag(LogLevelFlag, "Logging level: debug, info, warn, error").Default("info").Enum("debug", "info", "warn", "error")
	logFormat := app.Flag(LogFormatFlag, "Logging format: text or json").Default("text").Enum("text", "json")
	// host
	hostSysFS := app.Flag(HostSysFSFlag, "Host sysfs path").Default("/sys").ExistingDir()
	hostProcFS := app.Flag(HostProcFSFlag, "Host procfs path").Default("/proc").ExistingDir()

	// monitor
	monitorInterval := app.Flag(MonitorIntervalFlag,
		"Interval for monitoring resources (processes, container, vm, etc...); 0 to disable").Default("5s").Duration()
	monitorMaxTerminated := app.Flag(MonitorMaxTerminatedFlag,
		"Maximum number of terminated workloads to track; 0 to disable, -1 for unlimited").Default("500").Int()

	enablePprof := app.Flag(pprofEnabledFlag, "Enable pprof debug endpoints").Default("false").Bool()
	webConfig := app.Flag(WebConfigFlag, "Web config file path").Default("").String()
	webListenAddresses := app.Flag(WebListenAddressFlag, "Web server listen addresses").Default(":28282").Strings()

	// exporters
	stdoutExporterEnabled := app.Flag(ExporterStdoutEnabledFlag, "Enable stdout exporter").Default("false").Bool()

	prometheusExporterEnabled := app.Flag(ExporterPrometheusEnabledFlag, "Enable Prometheus exporter").Default("true").Bool()

	metricsLevel := MetricsLevelAll
	app.Flag(ExporterPrometheusMetricsFlag, "Metrics levels to export (node,process,container,vm,pod)").SetValue(NewMetricsLevelValue(&metricsLevel))

	kubernetes := app.Flag(KubernetesFlag, "Monitor kubernetes").Default("false").Bool()
	kubeconfig := app.Flag(KubeConfigFlag, "Path to a kubeconfig. Only required if out-of-cluster.").ExistingFile()
	nodeName := app.Flag(KubeNodeNameFlag, "Name of kubernetes node on which kepler is running.").String()

	// experimental platform
	redfishEnabled := app.Flag(ExperimentalPlatformRedfishEnabledFlag, "Enable experimental Redfish BMC power monitoring").Default("false").Bool()
	redfishNodeName := app.Flag(ExperimentalPlatformRedfishNodeNameFlag, "Node name for experimental Redfish platform power monitoring").String()
	redfishConfig := app.Flag(ExperimentalPlatformRedfishConfigFlag, "Path to experimental Redfish BMC configuration file").String()

	// experimental GPU
	gpuEnabled := app.Flag(ExperimentalPlatformGPUEnabledFlag, "Enable experimental GPU power monitoring").Default("false").Bool()
	gpuType := app.Flag(ExperimentalPlatformGPUTypeFlag, "GPU power meter type (dcgm or fake)").Default("dcgm").String()
	gpuDevices := app.Flag(ExperimentalPlatformGPUDevicesFlag, "GPU device IDs to monitor (comma-separated)").String()
	gpuUpdateFreq := app.Flag(ExperimentalPlatformGPUUpdateFreqFlag, "GPU metrics update frequency").Default("1s").Duration()
	gpuDCGMMode := app.Flag(ExperimentalPlatformGPUDCGMModeFlag, "DCGM connection mode (embedded or standalone)").Default("embedded").String()
	gpuDCGMAddr := app.Flag(ExperimentalPlatformGPUDCGMAddrFlag, "DCGM address for standalone mode (e.g., dcgm-exporter:5555)").String()

	return func(cfg *Config) error {
		// Logging settings
		if flagsSet[LogLevelFlag] {
			cfg.Log.Level = *logLevel
		}

		if flagsSet[LogFormatFlag] {
			cfg.Log.Format = *logFormat
		}

		if flagsSet[HostSysFSFlag] {
			cfg.Host.SysFS = *hostSysFS
		}

		if flagsSet[HostProcFSFlag] {
			cfg.Host.ProcFS = *hostProcFS
		}

		// monitor settings
		if flagsSet[MonitorIntervalFlag] {
			cfg.Monitor.Interval = *monitorInterval
		}
		if flagsSet[MonitorMaxTerminatedFlag] {
			cfg.Monitor.MaxTerminated = *monitorMaxTerminated
		}

		if flagsSet[pprofEnabledFlag] {
			cfg.Debug.Pprof.Enabled = enablePprof
		}

		if flagsSet[WebConfigFlag] {
			cfg.Web.Config = *webConfig
		}

		if flagsSet[WebListenAddressFlag] {
			cfg.Web.ListenAddresses = *webListenAddresses
		}

		if flagsSet[ExporterStdoutEnabledFlag] {
			cfg.Exporter.Stdout.Enabled = stdoutExporterEnabled
		}

		if flagsSet[ExporterPrometheusEnabledFlag] {
			cfg.Exporter.Prometheus.Enabled = prometheusExporterEnabled
		}

		if flagsSet[ExporterPrometheusMetricsFlag] {
			cfg.Exporter.Prometheus.MetricsLevel = metricsLevel
		}

		if flagsSet[KubernetesFlag] {
			cfg.Kube.Enabled = kubernetes
		}

		if flagsSet[KubeConfigFlag] {
			cfg.Kube.Config = *kubeconfig
		}

		if flagsSet[KubeNodeNameFlag] {
			cfg.Kube.Node = *nodeName
		}

		// Apply experimental platform settings
		if err := applyRedfishConfig(cfg, flagsSet, redfishEnabled, redfishNodeName, redfishConfig); err != nil {
			return err
		}
		if err := applyGPUConfig(cfg, flagsSet, gpuEnabled, gpuType, gpuDevices, gpuUpdateFreq, gpuDCGMMode, gpuDCGMAddr); err != nil {
			return err
		}

		cfg.sanitize()
		return cfg.Validate()
	}
}

// applyRedfishConfig applies Redfish configuration flags and resolves NodeName if enabled
func applyRedfishConfig(cfg *Config, flagsSet map[string]bool, enabled *bool, nodeName *string, cfgFile *string) error {
	// Early exit if no redfish flags are set and config file does not have experimental
	// section (i.e cfg.Experimental == nil)
	if !hasRedfishFlags(flagsSet) && cfg.Experimental == nil {
		return nil
	}

	// At this point, either redfish flags are set or config file has experimental section
	// so ensure experimental section exists
	if cfg.Experimental == nil {
		cfg.Experimental = &Experimental{
			Platform: Platform{
				Redfish: defaultRedfishConfig(),
			},
		}
	}

	redfish := &cfg.Experimental.Platform.Redfish

	// Apply flag values
	applyRedfishFlags(redfish, flagsSet, enabled, nodeName, cfgFile)

	// Exit (without resolving NodeName) if Redfish is not enabled
	if !ptr.Deref(redfish.Enabled, false) {
		return nil
	}

	// Resolve NodeName since Redfish is enabled
	return resolveRedfishNodeName(redfish, cfg.Kube.Node)
}

// hasRedfishFlags returns true if any experimental flags are set
func hasRedfishFlags(flagsSet map[string]bool) bool {
	return flagsSet[ExperimentalPlatformRedfishEnabledFlag] ||
		flagsSet[ExperimentalPlatformRedfishNodeNameFlag] ||
		flagsSet[ExperimentalPlatformRedfishConfigFlag]
}

func defaultRedfishConfig() Redfish {
	return Redfish{
		Enabled:     ptr.To(false),
		HTTPTimeout: 5 * time.Second,
	}
}

// applyRedfishFlags applies flag values to redfish config
func applyRedfishFlags(redfish *Redfish, flagsSet map[string]bool, enabled *bool, nodeName *string, cfgFile *string) {
	if flagsSet[ExperimentalPlatformRedfishEnabledFlag] {
		redfish.Enabled = enabled
	}

	if flagsSet[ExperimentalPlatformRedfishNodeNameFlag] {
		redfish.NodeName = *nodeName
	}

	if flagsSet[ExperimentalPlatformRedfishConfigFlag] {
		redfish.ConfigFile = *cfgFile
	}
}

// resolveRedfishNodeName resolves the Redfish node name
func resolveRedfishNodeName(redfish *Redfish, kubeNodeName string) error {
	resolvedNodeName, err := resolveNodeName(redfish.NodeName, kubeNodeName)
	if err != nil {
		return fmt.Errorf("failed to resolve Redfish node name: %w", err)
	}
	redfish.NodeName = resolvedNodeName
	return nil
}

// resolveNodeName resolves the node name using the following precedence:
// 1. CLI flag / config.yaml (--experimental.platform.redfish.node-name)
// 2. Kubernetes node name
// 3. Hostname fallback
func resolveNodeName(redfishNodeName, kubeNodeName string) (string, error) {
	// Priority 1: CLI flag
	if strings.TrimSpace(redfishNodeName) != "" {
		return strings.TrimSpace(redfishNodeName), nil
	}

	// Priority 2: Kubernetes node name
	if strings.TrimSpace(kubeNodeName) != "" {
		return strings.TrimSpace(kubeNodeName), nil
	}

	// Priority 3: Hostname fallback
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("failed to determine node name: %w", err)
	}

	return hostname, nil
}

// applyGPUConfig applies GPU configuration flags
func applyGPUConfig(cfg *Config, flagsSet map[string]bool, enabled *bool, gpuType *string, gpuDevices *string, updateFreq *time.Duration, dcgmMode *string, dcgmAddr *string) error {
	// Early exit if no GPU flags are set and config file does not have experimental section
	if !hasGPUFlags(flagsSet) && cfg.Experimental == nil {
		return nil
	}

	// Ensure experimental section exists
	if cfg.Experimental == nil {
		cfg.Experimental = &Experimental{
			Platform: Platform{
				GPU: defaultGPUConfig(),
			},
		}
	} else if cfg.Experimental.Platform.GPU.Enabled == nil {
		// Initialize GPU config if not present
		cfg.Experimental.Platform.GPU = defaultGPUConfig()
	}

	gpu := &cfg.Experimental.Platform.GPU

	// Apply flag values
	if flagsSet[ExperimentalPlatformGPUEnabledFlag] {
		gpu.Enabled = enabled
	}

	if flagsSet[ExperimentalPlatformGPUTypeFlag] {
		gpu.Type = *gpuType
	}

	if flagsSet[ExperimentalPlatformGPUDevicesFlag] && *gpuDevices != "" {
		// Parse comma-separated device IDs
		deviceStrs := strings.Split(*gpuDevices, ",")
		devices := make([]uint, 0, len(deviceStrs))
		for _, devStr := range deviceStrs {
			devID, err := strconv.ParseUint(strings.TrimSpace(devStr), 10, 32)
			if err != nil {
				return fmt.Errorf("invalid GPU device ID %q: %w", devStr, err)
			}
			devices = append(devices, uint(devID))
		}
		gpu.Devices = devices
	}

	if flagsSet[ExperimentalPlatformGPUUpdateFreqFlag] {
		gpu.UpdateFreq = *updateFreq
	}

	if flagsSet[ExperimentalPlatformGPUDCGMModeFlag] {
		gpu.DCGMMode = *dcgmMode
	}

	if flagsSet[ExperimentalPlatformGPUDCGMAddrFlag] {
		gpu.DCGMAddress = *dcgmAddr
	}

	return nil
}

// hasGPUFlags returns true if any GPU experimental flags are set
func hasGPUFlags(flagsSet map[string]bool) bool {
	return flagsSet[ExperimentalPlatformGPUEnabledFlag] ||
		flagsSet[ExperimentalPlatformGPUTypeFlag] ||
		flagsSet[ExperimentalPlatformGPUDevicesFlag] ||
		flagsSet[ExperimentalPlatformGPUUpdateFreqFlag] ||
		flagsSet[ExperimentalPlatformGPUDCGMModeFlag] ||
		flagsSet[ExperimentalPlatformGPUDCGMAddrFlag]
}

// defaultGPUConfig returns default GPU configuration
func defaultGPUConfig() GPU {
	return GPU{
		Enabled:     ptr.To(false),
		Type:        "dcgm",
		Devices:     []uint{0}, // Default to GPU 0
		UpdateFreq:  1 * time.Second,
		DCGMMode:    "embedded",
		DCGMAddress: "", // Empty means use default (localhost:5555 for standalone)
	}
}

// IsFeatureEnabled returns true if the specified feature is enabled
func (c *Config) IsFeatureEnabled(feature Feature) bool {
	switch feature {
	case ExperimentalRedfishFeature:
		if c.Experimental == nil {
			return false
		}
		return ptr.Deref(c.Experimental.Platform.Redfish.Enabled, false)
	case ExperimentalGPUFeature:
		if c.Experimental == nil {
			return false
		}
		return ptr.Deref(c.Experimental.Platform.GPU.Enabled, false)
	case PrometheusFeature:
		return ptr.Deref(c.Exporter.Prometheus.Enabled, false)
	case StdoutFeature:
		return ptr.Deref(c.Exporter.Stdout.Enabled, false)
	case PprofFeature:
		return ptr.Deref(c.Debug.Pprof.Enabled, false)
	default:
		return false
	}
}

// experimentalFeatureEnabled returns true if any experimental feature is enabled
func (c *Config) experimentalFeatureEnabled() bool {
	if c.Experimental == nil {
		return false
	}

	// Check if Redfish is enabled
	if ptr.Deref(c.Experimental.Platform.Redfish.Enabled, false) {
		return true
	}

	// Check if GPU is enabled
	if ptr.Deref(c.Experimental.Platform.GPU.Enabled, false) {
		return true
	}

	// Add checks for future experimental features here

	return false
}

func (c *Config) sanitize() {
	c.Log.Level = strings.TrimSpace(c.Log.Level)
	c.Log.Format = strings.TrimSpace(c.Log.Format)
	c.Host.SysFS = strings.TrimSpace(c.Host.SysFS)
	c.Host.ProcFS = strings.TrimSpace(c.Host.ProcFS)
	c.Web.Config = strings.TrimSpace(c.Web.Config)
	for i := range c.Web.ListenAddresses {
		c.Web.ListenAddresses[i] = strings.TrimSpace(c.Web.ListenAddresses[i])
	}

	for i := range c.Rapl.Zones {
		c.Rapl.Zones[i] = strings.TrimSpace(c.Rapl.Zones[i])
	}

	for i := range c.Exporter.Prometheus.DebugCollectors {
		c.Exporter.Prometheus.DebugCollectors[i] = strings.TrimSpace(c.Exporter.Prometheus.DebugCollectors[i])
	}
	c.Kube.Config = strings.TrimSpace(c.Kube.Config)

	if c.Experimental == nil {
		return
	}

	c.Experimental.Platform.Redfish.NodeName = strings.TrimSpace(c.Experimental.Platform.Redfish.NodeName)
	c.Experimental.Platform.Redfish.ConfigFile = strings.TrimSpace(c.Experimental.Platform.Redfish.ConfigFile)

	// If all experimental features are disabled, set experimental to nil to hide it
	if !c.experimentalFeatureEnabled() {
		c.Experimental = nil
	}
}

// Validate checks for configuration errors
func (c *Config) Validate(skips ...SkipValidation) error {
	validationSkipped := make(map[SkipValidation]bool, len(skips))
	for _, v := range skips {
		validationSkipped[v] = true
	}
	var errs []string
	{ // log level

		validLogLevels := map[string]bool{
			"debug": true,
			"info":  true,
			"warn":  true,
			"error": true,
		}

		// Validate logging settings
		if _, valid := validLogLevels[c.Log.Level]; !valid {
			errs = append(errs, fmt.Sprintf("invalid log level: %s", c.Log.Level))
		}
	}
	{ // log format
		validFormats := map[string]bool{
			"text": true,
			"json": true,
		}
		if _, valid := validFormats[c.Log.Format]; !valid {
			errs = append(errs, fmt.Sprintf("invalid log format: %s", c.Log.Format))
		}
	}

	{ // Validate host settings
		if _, skip := validationSkipped[SkipHostValidation]; !skip {
			if err := canReadDir(c.Host.SysFS); err != nil {
				errs = append(errs, fmt.Sprintf("invalid sysfs path: %s: %s ", c.Host.SysFS, err.Error()))
			}
			if err := canReadDir(c.Host.ProcFS); err != nil {
				errs = append(errs, fmt.Sprintf("invalid procfs path: %s: %s ", c.Host.ProcFS, err.Error()))
			}
		}
	}
	{ // Web config file
		if c.Web.Config != "" {
			if err := canReadFile(c.Web.Config); err != nil {
				errs = append(errs, fmt.Sprintf("invalid web config file. path: %q: %s", c.Web.Config, err.Error()))
			}
		}
	}
	{ // Web listen addresses
		if len(c.Web.ListenAddresses) == 0 {
			errs = append(errs, "at least one web listen address must be specified")
		}
		for _, addr := range c.Web.ListenAddresses {
			if addr == "" {
				errs = append(errs, "web listen address cannot be empty")
				continue
			}
			if err := validateListenAddress(addr); err != nil {
				errs = append(errs, fmt.Sprintf("invalid web listen address %q: %s", addr, err.Error()))
			}
		}
	}
	{ // Monitor
		if c.Monitor.Interval < 0 {
			errs = append(errs, fmt.Sprintf("invalid monitor interval: %s can't be negative", c.Monitor.Interval))
		}
		if c.Monitor.Staleness < 0 {
			errs = append(errs, fmt.Sprintf("invalid monitor staleness: %s can't be negative", c.Monitor.Staleness))
		}

		if c.Monitor.MinTerminatedEnergyThreshold < 0 {
			errs = append(errs, fmt.Sprintf("invalid monitor min terminated energy threshold: %d can't be negative", c.Monitor.MinTerminatedEnergyThreshold))
		}
	}
	{ // Kubernetes
		if ptr.Deref(c.Kube.Enabled, false) {
			if c.Kube.Config != "" {
				if err := canReadFile(c.Kube.Config); err != nil {
					errs = append(errs, fmt.Sprintf("unreadable kubeconfig: %s", c.Kube.Config))
				}
			}
			if c.Kube.Node == "" {
				errs = append(errs, fmt.Sprintf("%s not supplied but %s set to true", KubeNodeNameFlag, KubernetesFlag))
			}
		}
	}
	// Experimental Platform validation
	if experimentalErrs := c.validateExperimentalConfig(validationSkipped); len(experimentalErrs) > 0 {
		errs = append(errs, experimentalErrs...)
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration: %s", strings.Join(errs, ", "))
	}

	return nil
}

// validateExperimentalConfig validates experimental configuration settings
func (c *Config) validateExperimentalConfig(validationSkipped map[SkipValidation]bool) []string {
	if !c.experimentalFeatureEnabled() || validationSkipped[SkipExperimentalValidation] {
		return nil
	}

	var errs []string

	{ // Validate experimental settings
		if c.IsFeatureEnabled(ExperimentalRedfishFeature) {
			if c.Experimental.Platform.Redfish.ConfigFile == "" {
				errs = append(errs, fmt.Sprintf("%s not supplied but %s set to true", ExperimentalPlatformRedfishConfigFlag, ExperimentalPlatformRedfishEnabledFlag))
			} else {
				if err := canReadFile(c.Experimental.Platform.Redfish.ConfigFile); err != nil {
					errs = append(errs, fmt.Sprintf("unreadable Redfish config file: %s: %s", c.Experimental.Platform.Redfish.ConfigFile, err.Error()))
				}
			}
		}

		if c.IsFeatureEnabled(ExperimentalGPUFeature) {
			// Validate GPU type
			if c.Experimental.Platform.GPU.Type != "dcgm" && c.Experimental.Platform.GPU.Type != "fake" {
				errs = append(errs, fmt.Sprintf("invalid GPU type %q: must be 'dcgm' or 'fake'", c.Experimental.Platform.GPU.Type))
			}

			// Validate update frequency
			if c.Experimental.Platform.GPU.UpdateFreq <= 0 {
				errs = append(errs, "GPU update frequency must be positive")
			}

			// Validate device IDs (should have at least one)
			if len(c.Experimental.Platform.GPU.Devices) == 0 {
				errs = append(errs, "at least one GPU device ID must be specified")
			}

			// Validate DCGM mode if type is dcgm
			if c.Experimental.Platform.GPU.Type == "dcgm" {
				mode := c.Experimental.Platform.GPU.DCGMMode
				if mode != "" && mode != "embedded" && mode != "standalone" {
					errs = append(errs, fmt.Sprintf("invalid DCGM mode %q: must be 'embedded' or 'standalone'", mode))
				}

				// Standalone mode requires an address
				if mode == "standalone" && c.Experimental.Platform.GPU.DCGMAddress == "" {
					errs = append(errs, "DCGM address is required when using standalone mode")
				}
			}
		}
	}

	return errs
}

func canReadDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}

	defer func() {
		// ignored on purpose
		_ = f.Close()
	}()

	_, err = f.ReadDir(1)
	if err != nil {
		return err
	}

	return nil
}

func canReadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}

	defer func() {
		// ignored on purpose
		_ = f.Close()
	}()
	buf := make([]byte, 8)
	_, err = f.Read(buf)
	if err != nil {
		return err
	}

	return nil
}

func validateListenAddress(addr string) error {
	if addr == "" {
		return fmt.Errorf("address cannot be empty")
	}

	// Use Go's standard library to parse host:port
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address format: %w", err)
	}

	// Validate port (host can be empty for listening on all interfaces)
	if err := validatePort(port); err != nil {
		return err
	}

	return nil
}

func validatePort(port string) error {
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("port must be numeric, got %s", port)
	}

	if portNum < 1 || portNum > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", portNum)
	}
	return nil
}

func (c *Config) String() string {
	bytes, err := yaml.Marshal(c)
	if err == nil {
		return string(bytes)
	}
	// NOTE:  this code path should not happen but if it does (i.e if yaml marshal) fails
	// for some reason, manually build the string
	return c.manualString()
}

func (c *Config) manualString() string {
	cfgs := []struct {
		Name  string
		Value string
	}{
		{LogLevelFlag, c.Log.Level},
		{LogFormatFlag, c.Log.Format},
		{HostSysFSFlag, c.Host.SysFS},
		{HostProcFSFlag, c.Host.ProcFS},
		{MonitorIntervalFlag, c.Monitor.Interval.String()},
		{MonitorStaleness, c.Monitor.Staleness.String()},
		{MonitorMaxTerminatedFlag, fmt.Sprintf("%d", c.Monitor.MaxTerminated)},
		{RaplZones, strings.Join(c.Rapl.Zones, ", ")},
		{ExporterStdoutEnabledFlag, fmt.Sprintf("%v", c.Exporter.Stdout.Enabled)},
		{ExporterPrometheusEnabledFlag, fmt.Sprintf("%v", c.Exporter.Prometheus.Enabled)},
		{ExporterPrometheusDebugCollectors, strings.Join(c.Exporter.Prometheus.DebugCollectors, ", ")},
		{ExporterPrometheusMetricsFlag, c.Exporter.Prometheus.MetricsLevel.String()},
		{pprofEnabledFlag, fmt.Sprintf("%v", c.Debug.Pprof.Enabled)},
		{KubeConfigFlag, fmt.Sprintf("%v", c.Kube.Config)},
	}
	sb := strings.Builder{}

	for _, cfg := range cfgs {
		sb.WriteString(cfg.Name)
		sb.WriteString(": ")
		sb.WriteString(cfg.Value)
		sb.WriteString("\n")
	}

	return sb.String()
}
