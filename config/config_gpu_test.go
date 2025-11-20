// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestGPUFeatureEnabled(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected bool
	}{{
		name: "gpu feature enabled",
		config: &Config{
			Experimental: &Experimental{
				Platform: Platform{
					GPU: GPU{
						Enabled: ptr.To(true),
					},
				},
			},
		},
		expected: true,
	}, {
		name: "gpu feature disabled",
		config: &Config{
			Experimental: &Experimental{
				Platform: Platform{
					GPU: GPU{
						Enabled: ptr.To(false),
					},
				},
			},
		},
		expected: false,
	}, {
		name:     "gpu feature nil experimental",
		config:   &Config{},
		expected: false,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.config.IsFeatureEnabled(ExperimentalGPUFeature)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestApplyGPUConfig(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *Config
		flagsSet   map[string]bool
		enabled    *bool
		gpuType    *string
		gpuDevices *string
		updateFreq *time.Duration
		wantErr    bool
		expected   *GPU
	}{{
		name: "apply enabled flag",
		cfg:  &Config{},
		flagsSet: map[string]bool{
			ExperimentalPlatformGPUEnabledFlag: true,
		},
		enabled:    ptr.To(true),
		gpuType:    ptr.To("dcgm"),
		gpuDevices: ptr.To(""),
		updateFreq: ptr.To(1 * time.Second),
		expected: &GPU{
			Enabled:    ptr.To(true),
			Type:       "dcgm",
			Devices:    []uint{0},
			UpdateFreq: 1 * time.Second,
		},
	}, {
		name: "apply type and devices",
		cfg:  &Config{},
		flagsSet: map[string]bool{
			ExperimentalPlatformGPUTypeFlag:    true,
			ExperimentalPlatformGPUDevicesFlag: true,
		},
		enabled:    ptr.To(false),
		gpuType:    ptr.To("fake"),
		gpuDevices: ptr.To("0,1,2"),
		updateFreq: ptr.To(2 * time.Second),
		expected: &GPU{
			Enabled:    ptr.To(false),
			Type:       "fake",
			Devices:    []uint{0, 1, 2},
			UpdateFreq: 1 * time.Second, // Default
		},
	}, {
		name: "invalid device ID",
		cfg:  &Config{},
		flagsSet: map[string]bool{
			ExperimentalPlatformGPUDevicesFlag: true,
		},
		enabled:    ptr.To(false),
		gpuType:    ptr.To("dcgm"),
		gpuDevices: ptr.To("0,invalid,2"),
		updateFreq: ptr.To(1 * time.Second),
		wantErr:    true,
	}, {
		name: "apply update frequency",
		cfg:  &Config{},
		flagsSet: map[string]bool{
			ExperimentalPlatformGPUUpdateFreqFlag: true,
		},
		enabled:    ptr.To(false),
		gpuType:    ptr.To("dcgm"),
		gpuDevices: ptr.To(""),
		updateFreq: ptr.To(5 * time.Second),
		expected: &GPU{
			Enabled:    ptr.To(false),
			Type:       "dcgm",
			Devices:    []uint{0},
			UpdateFreq: 5 * time.Second,
		},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := applyGPUConfig(tc.cfg, tc.flagsSet, tc.enabled, tc.gpuType, tc.gpuDevices, tc.updateFreq)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, tc.cfg.Experimental)
			assert.Equal(t, tc.expected, &tc.cfg.Experimental.Platform.GPU)
		})
	}
}

func TestGPUConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
		errMsg  string
	}{{
		name: "valid dcgm config",
		modify: func(cfg *Config) {
			cfg.Experimental = &Experimental{
				Platform: Platform{
					GPU: GPU{
						Enabled:    ptr.To(true),
						Type:       "dcgm",
						Devices:    []uint{0, 1},
						UpdateFreq: 1 * time.Second,
					},
				},
			}
		},
		wantErr: false,
	}, {
		name: "valid fake config",
		modify: func(cfg *Config) {
			cfg.Experimental = &Experimental{
				Platform: Platform{
					GPU: GPU{
						Enabled:    ptr.To(true),
						Type:       "fake",
						Devices:    []uint{0},
						UpdateFreq: 500 * time.Millisecond,
					},
				},
			}
		},
		wantErr: false,
	}, {
		name: "invalid gpu type",
		modify: func(cfg *Config) {
			cfg.Experimental = &Experimental{
				Platform: Platform{
					GPU: GPU{
						Enabled:    ptr.To(true),
						Type:       "invalid",
						Devices:    []uint{0},
						UpdateFreq: 1 * time.Second,
					},
				},
			}
		},
		wantErr: true,
		errMsg:  "invalid GPU type",
	}, {
		name: "invalid update frequency",
		modify: func(cfg *Config) {
			cfg.Experimental = &Experimental{
				Platform: Platform{
					GPU: GPU{
						Enabled:    ptr.To(true),
						Type:       "dcgm",
						Devices:    []uint{0},
						UpdateFreq: -1 * time.Second,
					},
				},
			}
		},
		wantErr: true,
		errMsg:  "GPU update frequency must be positive",
	}, {
		name: "no devices specified",
		modify: func(cfg *Config) {
			cfg.Experimental = &Experimental{
				Platform: Platform{
					GPU: GPU{
						Enabled:    ptr.To(true),
						Type:       "dcgm",
						Devices:    []uint{},
						UpdateFreq: 1 * time.Second,
					},
				},
			}
		},
		wantErr: true,
		errMsg:  "at least one GPU device ID must be specified",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Start with default config that passes validation
			cfg := DefaultConfig()
			cfg.Host.SysFS = "/sys"
			cfg.Host.ProcFS = "/proc"
			cfg.Web.ListenAddresses = []string{":9100"}

			// Apply test modifications
			tc.modify(cfg)

			err := cfg.Validate()

			if tc.wantErr {
				assert.Error(t, err)
				if tc.errMsg != "" {
					assert.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHasGPUFlags(t *testing.T) {
	tests := []struct {
		name     string
		flagsSet map[string]bool
		expected bool
	}{{
		name: "has enabled flag",
		flagsSet: map[string]bool{
			ExperimentalPlatformGPUEnabledFlag: true,
		},
		expected: true,
	}, {
		name: "has type flag",
		flagsSet: map[string]bool{
			ExperimentalPlatformGPUTypeFlag: true,
		},
		expected: true,
	}, {
		name: "has devices flag",
		flagsSet: map[string]bool{
			ExperimentalPlatformGPUDevicesFlag: true,
		},
		expected: true,
	}, {
		name: "has update freq flag",
		flagsSet: map[string]bool{
			ExperimentalPlatformGPUUpdateFreqFlag: true,
		},
		expected: true,
	}, {
		name: "has multiple gpu flags",
		flagsSet: map[string]bool{
			ExperimentalPlatformGPUEnabledFlag: true,
			ExperimentalPlatformGPUTypeFlag:    true,
		},
		expected: true,
	}, {
		name: "has non-gpu flags only",
		flagsSet: map[string]bool{
			ExperimentalPlatformRedfishEnabledFlag: true,
			"some-other-flag":                      true,
		},
		expected: false,
	}, {
		name:     "no flags",
		flagsSet: map[string]bool{},
		expected: false,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := hasGPUFlags(tc.flagsSet)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDevFakeGPUMeterConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Test defaults
	assert.Equal(t, ptr.To(false), cfg.Dev.FakeGpuMeter.Enabled)
	assert.Equal(t, []uint{0}, cfg.Dev.FakeGpuMeter.Devices)
	assert.Equal(t, 100.0, cfg.Dev.FakeGpuMeter.PowerBase)
	assert.Equal(t, 50.0, cfg.Dev.FakeGpuMeter.PowerRange)
	assert.Equal(t, 1000.0, cfg.Dev.FakeGpuMeter.EnergyStep)
}

func TestGPUConfigYAML(t *testing.T) {
	yamlStr := `
experimental:
  platform:
    gpu:
      enabled: true
      type: fake
      devices: [0, 1, 2]
      updateFreq: 2s
`
	reader := strings.NewReader(yamlStr)
	cfg, err := Load(reader)
	assert.NoError(t, err)

	assert.NotNil(t, cfg.Experimental)
	gpu := cfg.Experimental.Platform.GPU
	assert.Equal(t, ptr.To(true), gpu.Enabled)
	assert.Equal(t, "fake", gpu.Type)
	assert.Equal(t, []uint{0, 1, 2}, gpu.Devices)
	assert.Equal(t, 2*time.Second, gpu.UpdateFreq)
}

func TestExperimentalFeatureEnabled(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected bool
	}{{
		name: "gpu enabled",
		config: &Config{
			Experimental: &Experimental{
				Platform: Platform{
					GPU: GPU{
						Enabled: ptr.To(true),
					},
					Redfish: Redfish{
						Enabled: ptr.To(false),
					},
				},
			},
		},
		expected: true,
	}, {
		name: "both gpu and redfish enabled",
		config: &Config{
			Experimental: &Experimental{
				Platform: Platform{
					GPU: GPU{
						Enabled: ptr.To(true),
					},
					Redfish: Redfish{
						Enabled: ptr.To(true),
					},
				},
			},
		},
		expected: true,
	}, {
		name: "all experimental features disabled",
		config: &Config{
			Experimental: &Experimental{
				Platform: Platform{
					GPU: GPU{
						Enabled: ptr.To(false),
					},
					Redfish: Redfish{
						Enabled: ptr.To(false),
					},
				},
			},
		},
		expected: false,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.config.experimentalFeatureEnabled()
			assert.Equal(t, tc.expected, result)
		})
	}
}
