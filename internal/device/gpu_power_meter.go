// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

// GPUEnergyZone represents a GPU device energy zone
type GPUEnergyZone interface {
	EnergyZone

	// DeviceID returns the GPU device ID
	DeviceID() uint
}

// GPUProcessUtilization contains utilization data for a process on a GPU
type GPUProcessUtilization struct {
	PID            int     // Process ID
	GPUID          uint    // GPU device ID
	SMUtilization  float64 // Streaming Multiprocessor utilization (0-100)
	EnergyConsumed Energy  // Energy consumed by the process (if available from hardware)
}

// GPUPowerMeter implements power metering for GPU devices
type GPUPowerMeter interface {
	powerMeter

	// Zones returns a slice of the GPU energy measurement zones
	Zones() ([]GPUEnergyZone, error)

	// ProcessPower returns the power consumption for a specific process
	// Returns power in watts and energy in joules
	ProcessPower(pid int, gpuID uint) (Power, Energy, error)

	// ProcessUtilization returns the SM utilization for a specific process on a GPU
	// This is used for ratio-based power attribution where:
	// ProcessPower = (ProcessSMUtil / TotalSMUtil) * NodeGPUPower
	// Returns utilization data and an error if the process is not found on the GPU
	ProcessUtilization(pid int, gpuID uint) (*GPUProcessUtilization, error)

	// DevicePower returns the instantaneous power for a specific GPU device
	// Returns power and a boolean indicating if the device exists
	DevicePower(gpuID uint) (Power, bool)

	// Start initializes the GPU power monitoring
	Start() error

	// Stop stops the GPU power monitoring
	Stop() error
}
