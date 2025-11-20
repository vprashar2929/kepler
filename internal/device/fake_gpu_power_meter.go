// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// fakeGPUZone implements the GPUEnergyZone interface for testing
type fakeGPUZone struct {
	deviceID  uint
	energy    Energy
	maxEnergy Energy
	mu        sync.Mutex

	// For generating fake values
	energyStep   Energy
	randomFactor float64
}

var _ GPUEnergyZone = (*fakeGPUZone)(nil)

// Name returns the zone name
func (z *fakeGPUZone) Name() string {
	return "gpu"
}

// Index returns the index of the zone (same as device ID)
func (z *fakeGPUZone) Index() int {
	return int(z.deviceID)
}

// Path returns the path identifier
func (z *fakeGPUZone) Path() string {
	return fmt.Sprintf("fake:gpu:%d", z.deviceID)
}

// Energy returns the current energy reading
func (z *fakeGPUZone) Energy() (Energy, error) {
	z.mu.Lock()
	defer z.mu.Unlock()

	// Simulate energy accumulation with some randomness
	randomComponent := Energy(rand.Float64() * float64(z.energyStep) * z.randomFactor)
	z.energy = (z.energy + z.energyStep + randomComponent) % z.maxEnergy

	return z.energy, nil
}

// MaxEnergy returns the maximum energy value before wrap
func (z *fakeGPUZone) MaxEnergy() Energy {
	return z.maxEnergy
}

// DeviceID returns the GPU device ID
func (z *fakeGPUZone) DeviceID() uint {
	return z.deviceID
}

// fakeGPUPowerMeter implements GPUPowerMeter interface for testing
type fakeGPUPowerMeter struct {
	logger     *slog.Logger
	zones      []GPUEnergyZone
	devices    []uint
	powerBase  float64 // Base power consumption in watts
	powerRange float64 // Power variation range in watts
	energyStep float64 // Energy increment per update in joules

	// Process power tracking (simulated)
	processPower map[processKey]processMetrics
	mu           sync.RWMutex
	running      bool
}

type processMetrics struct {
	power         Power
	energy        Energy
	smUtilization float64 // SM utilization percentage (0-100)
}

var _ GPUPowerMeter = (*fakeGPUPowerMeter)(nil)

// FakeGPUOptFn is a functional option for configuring FakeGPUPowerMeter
type FakeGPUOptFn func(*fakeGPUPowerMeter)

// WithFakeGPULogger sets the logger
func WithFakeGPULogger(logger *slog.Logger) FakeGPUOptFn {
	return func(m *fakeGPUPowerMeter) {
		m.logger = logger
	}
}

// WithFakeGPUPowerBase sets the base power consumption
func WithFakeGPUPowerBase(watts float64) FakeGPUOptFn {
	return func(m *fakeGPUPowerMeter) {
		m.powerBase = watts
	}
}

// WithFakeGPUPowerRange sets the power variation range
func WithFakeGPUPowerRange(watts float64) FakeGPUOptFn {
	return func(m *fakeGPUPowerMeter) {
		m.powerRange = watts
	}
}

// WithFakeGPUEnergyStep sets the energy increment per update
func WithFakeGPUEnergyStep(joules float64) FakeGPUOptFn {
	return func(m *fakeGPUPowerMeter) {
		m.energyStep = joules
	}
}

// NewFakeGPUMeter creates a new fake GPU power meter for testing
func NewFakeGPUMeter(devices []uint, opts ...FakeGPUOptFn) (GPUPowerMeter, error) {
	if len(devices) == 0 {
		devices = []uint{0} // Default to GPU 0
	}

	meter := &fakeGPUPowerMeter{
		logger:       slog.Default(),
		devices:      devices,
		powerBase:    100.0,  // 100W base
		powerRange:   50.0,   // ±50W variation
		energyStep:   1000.0, // 1kJ per update
		processPower: make(map[processKey]processMetrics),
	}

	// Apply options
	for _, opt := range opts {
		opt(meter)
	}

	// Create zones for each device
	zones := make([]GPUEnergyZone, len(devices))
	for i, devID := range devices {
		zones[i] = &fakeGPUZone{
			deviceID:     devID,
			energy:       0,
			maxEnergy:    Energy(^uint64(0)), // Max uint64
			energyStep:   Energy(meter.energyStep) * MicroJoule,
			randomFactor: 0.2, // 20% randomness
		}
	}
	meter.zones = zones

	meter.logger = meter.logger.With("meter", "fake-gpu")
	meter.logger.Info("Created fake GPU meter", "devices", devices)

	return meter, nil
}

// Name returns the meter name
func (m *fakeGPUPowerMeter) Name() string {
	return "fake-gpu"
}

// Zones returns the GPU energy zones
func (m *fakeGPUPowerMeter) Zones() ([]GPUEnergyZone, error) {
	return m.zones, nil
}

// ProcessPower returns simulated power and energy for a process on a GPU
func (m *fakeGPUPowerMeter) ProcessPower(pid int, gpuID uint) (Power, Energy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.running {
		return 0, 0, fmt.Errorf("GPU meter not started")
	}

	// Check if the GPU ID is valid
	validGPU := false
	for _, devID := range m.devices {
		if devID == gpuID {
			validGPU = true
			break
		}
	}
	if !validGPU {
		return 0, 0, fmt.Errorf("GPU %d not monitored", gpuID)
	}

	key := processKey{pid: pid, gpu: gpuID}
	metrics, exists := m.processPower[key]
	if !exists {
		// No GPU usage for this process
		return 0, 0, nil
	}

	return metrics.power, metrics.energy, nil
}

// ProcessUtilization returns the SM utilization for a specific process on a GPU
func (m *fakeGPUPowerMeter) ProcessUtilization(pid int, gpuID uint) (*GPUProcessUtilization, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.running {
		return nil, fmt.Errorf("GPU meter not started")
	}

	// Check if the GPU ID is valid
	validGPU := false
	for _, devID := range m.devices {
		if devID == gpuID {
			validGPU = true
			break
		}
	}
	if !validGPU {
		return nil, fmt.Errorf("GPU %d not monitored", gpuID)
	}

	key := processKey{pid: pid, gpu: gpuID}
	metrics, exists := m.processPower[key]
	if !exists {
		return nil, fmt.Errorf("process %d not found on GPU %d", pid, gpuID)
	}

	return &GPUProcessUtilization{
		PID:            pid,
		GPUID:          gpuID,
		SMUtilization:  metrics.smUtilization,
		EnergyConsumed: metrics.energy,
	}, nil
}

// Start begins GPU power monitoring
func (m *fakeGPUPowerMeter) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	m.running = true
	m.logger.Info("Started fake GPU power monitoring")

	// Start a goroutine to simulate process GPU usage
	go m.simulateProcessGPUUsage()

	return nil
}

// Stop stops GPU power monitoring
func (m *fakeGPUPowerMeter) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.running = false
	m.logger.Info("Stopped fake GPU power monitoring")

	return nil
}

// DevicePower returns the instantaneous power for a specific GPU device
func (m *fakeGPUPowerMeter) DevicePower(gpuID uint) (Power, bool) {
	// Generate simulated power based on base power and random variation
	variation := (rand.Float64() - 0.5) * m.powerRange
	power := Power((m.powerBase + variation) * 1e6) // Convert watts to microwatts

	// Check if device exists
	for _, id := range m.devices {
		if id == gpuID {
			return power, true
		}
	}
	return 0, false
}

// simulateProcessGPUUsage simulates random process GPU usage
func (m *fakeGPUPowerMeter) simulateProcessGPUUsage() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Simulate some PIDs that might use GPU
	testPIDs := []int{1234, 5678, 9012}

	for {
		select {
		case <-ticker.C:
			m.mu.Lock()
			if !m.running {
				m.mu.Unlock()
				return
			}

			// Randomly assign GPU usage to processes
			for _, pid := range testPIDs {
				for _, gpuID := range m.devices {
					key := processKey{pid: pid, gpu: gpuID}

					// 50% chance a process uses a GPU
					if rand.Float64() < 0.5 {
						// Generate random power consumption
						power := m.powerBase + (rand.Float64()-0.5)*m.powerRange
						// Generate random SM utilization (10-90%)
						smUtil := 10.0 + rand.Float64()*80.0

						// Update or create metrics
						metrics := m.processPower[key]
						metrics.power = Power(power) * MicroWatt
						metrics.energy += Energy(power) * MicroJoule // Simple accumulation
						metrics.smUtilization = smUtil
						m.processPower[key] = metrics
					} else {
						// Remove if no usage
						delete(m.processPower, key)
					}
				}
			}

			m.mu.Unlock()
		}
	}
}

// SetProcessPower allows tests to manually set process power consumption
func (m *fakeGPUPowerMeter) SetProcessPower(pid int, gpuID uint, power Power, energy Energy) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := processKey{pid: pid, gpu: gpuID}
	m.processPower[key] = processMetrics{
		power:  power,
		energy: energy,
	}
}

// SetProcessUtilization allows tests to manually set process GPU utilization
func (m *fakeGPUPowerMeter) SetProcessUtilization(pid int, gpuID uint, smUtilization float64, energy Energy) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := processKey{pid: pid, gpu: gpuID}
	metrics := m.processPower[key]
	metrics.smUtilization = smUtilization
	metrics.energy = energy
	m.processPower[key] = metrics
}
