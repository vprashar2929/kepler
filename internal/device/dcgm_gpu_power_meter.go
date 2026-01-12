// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
)

// DCGMGPUZone represents a GPU device zone using DCGM
type DCGMGPUZone struct {
	deviceID      uint
	lastEnergy    Energy
	currentEnergy Energy
	maxEnergy     Energy
	mu            sync.RWMutex
}

// NewDCGMGPUZone creates a new DCGM GPU zone
func NewDCGMGPUZone(deviceID uint) *DCGMGPUZone {
	return &DCGMGPUZone{
		deviceID:  deviceID,
		maxEnergy: Energy(^uint64(0)), // Max uint64 as we don't have hardware limits from DCGM
	}
}

// Name returns the zone name
func (z *DCGMGPUZone) Name() string {
	return "gpu"
}

// Index returns the GPU device ID
func (z *DCGMGPUZone) Index() int {
	return int(z.deviceID)
}

// DeviceID returns the GPU device ID
func (z *DCGMGPUZone) DeviceID() uint {
	return z.deviceID
}

// Path returns the path identifier
func (z *DCGMGPUZone) Path() string {
	return fmt.Sprintf("dcgm:gpu:%d", z.deviceID)
}

// Energy returns the current energy reading
func (z *DCGMGPUZone) Energy() (Energy, error) {
	z.mu.RLock()
	defer z.mu.RUnlock()
	return z.currentEnergy, nil
}

// MaxEnergy returns the maximum energy value before wrap
func (z *DCGMGPUZone) MaxEnergy() Energy {
	return z.maxEnergy
}

// updateEnergy updates the energy reading
func (z *DCGMGPUZone) updateEnergy(energy Energy) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.lastEnergy = z.currentEnergy
	z.currentEnergy = energy
}

// DCGMGPUPowerMeter implements GPUPowerMeter using NVIDIA DCGM
type DCGMGPUPowerMeter struct {
	logger       *slog.Logger
	zones        []GPUEnergyZone
	groupHandle  dcgm.GroupHandle
	fieldGroup   dcgm.FieldHandle
	updateFreq   time.Duration
	mu           sync.RWMutex
	processInfo  map[processKey]*dcgm.ProcessInfo
	devicePower  map[uint]Power  // Device ID -> Power
	deviceEnergy map[uint]Energy // Device ID -> Energy
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

type processKey struct {
	pid int
	gpu uint
}

// DCGMGPUPowerMeterOpts contains options for DCGM GPU power meter
type DCGMGPUPowerMeterOpts struct {
	Logger     *slog.Logger
	UpdateFreq time.Duration
	MaxKeepAge time.Duration
	MaxSamples int
	GPUDevices []uint

	// DCGMMode specifies how to connect to DCGM: "embedded" or "standalone"
	// Embedded mode starts a local DCGM engine (requires local GPU)
	// Standalone mode connects to an external DCGM host engine
	DCGMMode DCGMMode
	// DCGMAddress is the address of the DCGM host engine for standalone mode
	// Format: "hostname:port" (e.g., "dcgm-exporter:5555")
	DCGMAddress string
}

// NewDCGMGPUPowerMeter creates a new DCGM-based GPU power meter
func NewDCGMGPUPowerMeter(opts DCGMGPUPowerMeterOpts) (*DCGMGPUPowerMeter, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.UpdateFreq == 0 {
		opts.UpdateFreq = 1 * time.Second
	}
	if opts.MaxKeepAge == 0 {
		opts.MaxKeepAge = 30 * time.Second
	}
	if opts.MaxSamples == 0 {
		opts.MaxSamples = 1000
	}
	if len(opts.GPUDevices) == 0 {
		// Default to GPU 0
		opts.GPUDevices = []uint{0}
	}
	if opts.DCGMMode == "" {
		opts.DCGMMode = DCGMModeEmbedded
	}

	// Initialize DCGM based on mode
	var cleanup func()
	var err error

	switch opts.DCGMMode {
	case DCGMModeStandalone:
		if opts.DCGMAddress == "" {
			return nil, fmt.Errorf("DCGM address is required for standalone mode")
		}
		opts.Logger.Info("Connecting to DCGM in standalone mode", "address", opts.DCGMAddress)
		cleanup, err = dcgmLib.InitStandalone(opts.DCGMAddress)
	case DCGMModeEmbedded:
		fallthrough
	default:
		opts.Logger.Info("Initializing DCGM in embedded mode")
		cleanup, err = dcgmLib.Init()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to initialize DCGM (mode=%s): %w", opts.DCGMMode, err)
	}

	// If initialization fails later, ensure cleanup
	defer func() {
		if err != nil {
			cleanup()
		}
	}()

	// Create zones for each GPU
	zones := make([]GPUEnergyZone, 0, len(opts.GPUDevices))
	for _, gpuID := range opts.GPUDevices {
		zones = append(zones, NewDCGMGPUZone(gpuID))
	}

	meter := &DCGMGPUPowerMeter{
		logger:       opts.Logger.With("meter", "dcgm-gpu"),
		zones:        zones,
		updateFreq:   opts.UpdateFreq,
		processInfo:  make(map[processKey]*dcgm.ProcessInfo),
		devicePower:  make(map[uint]Power),
		deviceEnergy: make(map[uint]Energy),
		stopCh:       make(chan struct{}),
	}

	// Create field group for process monitoring
	groupHandle, err := dcgmLib.WatchPidFieldsEx(
		opts.UpdateFreq,
		opts.MaxKeepAge,
		opts.MaxSamples,
		opts.GPUDevices...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create DCGM watch group: %w", err)
	}
	meter.groupHandle = groupHandle

	// Create field group for device monitoring
	fieldIds := []dcgm.Short{
		dcgm.DCGM_FI_DEV_POWER_USAGE,
		dcgm.DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION,
	}
	fieldGroupName := fmt.Sprintf("kepler_device_fields_%d", time.Now().UnixNano())
	fieldGroup, err := dcgmLib.FieldGroupCreate(fieldGroupName, fieldIds)
	if err != nil {
		// cleanup group if field group creation fails
		_ = dcgmLib.DestroyGroup(groupHandle)
		return nil, fmt.Errorf("failed to create DCGM field group: %w", err)
	}
	meter.fieldGroup = fieldGroup

	// Start watching the device fields
	if err := dcgmLib.WatchFieldsWithGroup(fieldGroup, groupHandle); err != nil {
		_ = dcgmLib.DestroyGroup(groupHandle)
		_ = dcgmLib.FieldGroupDestroy(fieldGroup)
		return nil, fmt.Errorf("failed to watch DCGM fields: %w", err)
	}

	return meter, nil
}

// Name returns the meter name
func (m *DCGMGPUPowerMeter) Name() string {
	return "dcgm-gpu"
}

// Zones returns the GPU energy zones
func (m *DCGMGPUPowerMeter) Zones() ([]GPUEnergyZone, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.zones, nil
}

// DevicePower returns the instantaneous power for a specific GPU device
func (m *DCGMGPUPowerMeter) DevicePower(gpuID uint) (Power, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	power, exists := m.devicePower[gpuID]
	return power, exists
}

// ProcessPower returns power and energy for a specific process on a GPU
// Note: This method calculates power based on SM utilization ratio of total device power.
// For normalized power attribution (where sum of process powers equals node GPU power),
// use ProcessUtilization() and calculate ratios at the monitor level.
func (m *DCGMGPUPowerMeter) ProcessPower(pid int, gpuID uint) (Power, Energy, error) {
	// Fetch process info directly from DCGM
	// GetProcessInfo returns info for all GPUs in the group for the given PID
	infos, err := dcgmLib.GetProcessInfo(m.groupHandle, uint(pid))
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get process info for pid %d: %w", pid, err)
	}

	for _, info := range infos {
		if info.GPU == gpuID {
			// Found the process on the requested GPU
			if info.ProcessUtilization.EnergyConsumed == nil {
				return 0, 0, fmt.Errorf("no energy data for process %d on GPU %d", pid, gpuID)
			}

			// EnergyConsumed is in Joules (according to go-dcgm docs), convert to MicroJoules
			energy := Energy(*info.ProcessUtilization.EnergyConsumed * 1e6)

			// Calculate power based on device power and SM utilization
			m.mu.RLock()
			devicePower, exists := m.devicePower[gpuID]
			m.mu.RUnlock()

			if !exists {
				// If we don't have device power yet, we can't calculate process power
				// But we can return energy
				return 0, energy, nil
			}

			var power Power
			if info.ProcessUtilization.SmUtil != nil {
				utilizationRatio := *info.ProcessUtilization.SmUtil / 100.0
				power = Power(float64(devicePower) * utilizationRatio)
			}

			return power, energy, nil
		}
	}

	return 0, 0, fmt.Errorf("process %d not found on GPU %d", pid, gpuID)
}

// ProcessUtilization returns the SM utilization for a specific process on a GPU
// This is used for ratio-based power attribution where:
// ProcessPower = (ProcessSMUtil / TotalSMUtil) * NodeGPUPower
func (m *DCGMGPUPowerMeter) ProcessUtilization(pid int, gpuID uint) (*GPUProcessUtilization, error) {
	// Fetch process info directly from DCGM
	infos, err := dcgmLib.GetProcessInfo(m.groupHandle, uint(pid))
	if err != nil {
		return nil, fmt.Errorf("failed to get process info for pid %d: %w", pid, err)
	}

	for _, info := range infos {
		if info.GPU == gpuID {
			util := &GPUProcessUtilization{
				PID:   pid,
				GPUID: gpuID,
			}

			// Get SM utilization
			if info.ProcessUtilization.SmUtil != nil {
				util.SMUtilization = *info.ProcessUtilization.SmUtil
			}

			// Get energy consumed if available
			if info.ProcessUtilization.EnergyConsumed != nil {
				// EnergyConsumed is in Joules, convert to MicroJoules
				util.EnergyConsumed = Energy(*info.ProcessUtilization.EnergyConsumed * 1e6)
			}

			return util, nil
		}
	}

	return nil, fmt.Errorf("process %d not found on GPU %d", pid, gpuID)
}

// Start begins GPU power monitoring
func (m *DCGMGPUPowerMeter) Start() error {
	m.logger.Info("Starting DCGM GPU power monitoring")

	// Start the update loop
	m.wg.Add(1)
	go m.updateLoop()

	return nil
}

// Stop stops GPU power monitoring
func (m *DCGMGPUPowerMeter) Stop() error {
	m.logger.Info("Stopping DCGM GPU power monitoring")
	close(m.stopCh)
	m.wg.Wait()

	// Cleanup DCGM resources
	if m.fieldGroup.GetHandle() != 0 {
		_ = dcgmLib.FieldGroupDestroy(m.fieldGroup)
	}
	// GroupHandle is a struct, not a pointer, so we don't need nil check
	// But we should destroy it
	_ = dcgmLib.DestroyGroup(m.groupHandle)
	m.logger.Debug("Cleaned up DCGM resources")

	return nil
}

// updateLoop periodically updates GPU metrics
func (m *DCGMGPUPowerMeter) updateLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.updateFreq)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.update(); err != nil {
				m.logger.Error("Failed to update GPU metrics", "error", err)
			}
		case <-m.stopCh:
			return
		}
	}
}

// update fetches the latest GPU metrics
func (m *DCGMGPUPowerMeter) update() error {
	return m.updateDeviceMetrics()
}

// updateDeviceMetrics updates device-level power metrics
func (m *DCGMGPUPowerMeter) updateDeviceMetrics() error {
	// Get latest values for monitored fields
	values, _, err := dcgmLib.GetValuesSince(m.groupHandle, m.fieldGroup, time.Time{})
	if err != nil {
		return fmt.Errorf("failed to get device metrics: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, val := range values {
		if val.Status != 0 { // DCGM_ST_OK
			continue
		}

		gpuID := val.EntityID

		switch val.FieldID {
		case dcgm.DCGM_FI_DEV_POWER_USAGE:
			// Watts to MicroWatts
			power := Power(val.Float64() * 1e6)
			m.devicePower[gpuID] = power

		case dcgm.DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION:
			// MilliJoules to MicroJoules
			energy := Energy(val.Int64() * 1000)
			m.deviceEnergy[gpuID] = energy

			// Update zone energy
			for _, zone := range m.zones {
				if gpuZone, ok := zone.(*DCGMGPUZone); ok && gpuZone.DeviceID() == gpuID {
					gpuZone.updateEnergy(energy)
					break
				}
			}
		}
	}

	return nil
}
