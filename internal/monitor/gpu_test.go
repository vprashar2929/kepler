// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package monitor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/sustainable-computing-io/kepler/internal/device"
)

// TestProcessGPUPowerCollection tests GPU power collection for processes using ratio-based attribution
func TestProcessGPUPowerCollection(t *testing.T) {
	// Create mock GPU meter
	mockGPU := new(device.MockGPUPowerMeter)

	// Create mock GPU zones
	mockZone1 := new(mockGPUZone)
	mockZone1.On("DeviceID").Return(uint(0))

	gpuZones := []device.GPUEnergyZone{mockZone1}
	mockGPU.On("Zones").Return(gpuZones, nil)

	// Setup process utilization expectations (single process with 50% SM utilization)
	mockGPU.On("ProcessUtilization", 1234, uint(0)).Return(&device.GPUProcessUtilization{
		PID:            1234,
		GPUID:          0,
		SMUtilization:  50.0,
		EnergyConsumed: device.Energy(1000),
	}, nil)

	// Create mock CPU meter
	mockCPU := new(MockCPUPowerMeter)
	zones := []EnergyZone{}
	mockCPU.On("Zones").Return(zones, nil)
	mockCPU.On("PrimaryEnergyZone").Return(nil, fmt.Errorf("no zones"))

	// Create power monitor with GPU
	pm := NewPowerMonitor(mockCPU, WithGPUPowerMeter(mockGPU))

	// Create a snapshot with processes
	snapshot := NewSnapshot()
	snapshot.Node.GPUZones = make(NodeGPUUsageMap)
	// Node GPU power is 100W (100,000,000 microWatts) with 1000 microJoules active energy
	snapshot.Node.GPUZones[0] = NodeUsage{
		EnergyTotal:  1000,
		ActivePower:  device.Power(100_000_000), // 100W in microWatts
		activeEnergy: device.Energy(1000),       // 1000 microJoules
	}

	process := &Process{
		PID:      1234,
		Comm:     "test-process",
		GPUZones: make(GPUUsageMap),
	}
	snapshot.Processes["1234"] = process

	// Calculate GPU power
	pm.calculateProcessGPUPower(snapshot, nil)

	// Verify GPU power was collected
	// Since there's only one process with 50% utilization, and totalSMUtil = 50,
	// the ratio is 50/50 = 1.0, so process gets 100% of node GPU power
	assert.Contains(t, process.GPUZones, uint(0))
	assert.Equal(t, device.Power(100_000_000), process.GPUZones[0].Power)
	assert.Equal(t, device.Energy(1000), process.GPUZones[0].EnergyTotal)

	mockGPU.AssertExpectations(t)
	mockZone1.AssertExpectations(t)
}

// TestProcessGPUPowerNormalization tests that sum of process GPU powers equals node GPU power
func TestProcessGPUPowerNormalization(t *testing.T) {
	// Create mock GPU meter
	mockGPU := new(device.MockGPUPowerMeter)

	// Create mock GPU zone
	mockZone := new(mockGPUZone)
	mockZone.On("DeviceID").Return(uint(0))

	gpuZones := []device.GPUEnergyZone{mockZone}
	mockGPU.On("Zones").Return(gpuZones, nil)

	// Setup process utilization expectations for 3 processes sharing GPU
	// Process 1: 30% SM utilization
	// Process 2: 50% SM utilization
	// Process 3: 20% SM utilization
	// Total: 100% (but this is coincidental, it could be any value)
	mockGPU.On("ProcessUtilization", 1001, uint(0)).Return(&device.GPUProcessUtilization{
		PID:           1001,
		GPUID:         0,
		SMUtilization: 30.0,
	}, nil)
	mockGPU.On("ProcessUtilization", 1002, uint(0)).Return(&device.GPUProcessUtilization{
		PID:           1002,
		GPUID:         0,
		SMUtilization: 50.0,
	}, nil)
	mockGPU.On("ProcessUtilization", 1003, uint(0)).Return(&device.GPUProcessUtilization{
		PID:           1003,
		GPUID:         0,
		SMUtilization: 20.0,
	}, nil)

	// Create mock CPU meter
	mockCPU := new(MockCPUPowerMeter)
	mockCPU.On("Zones").Return([]EnergyZone{}, nil)
	mockCPU.On("PrimaryEnergyZone").Return(nil, fmt.Errorf("no zones"))

	// Create power monitor with GPU
	pm := NewPowerMonitor(mockCPU, WithGPUPowerMeter(mockGPU))

	// Create a snapshot with processes
	snapshot := NewSnapshot()
	snapshot.Node.GPUZones = make(NodeGPUUsageMap)
	// Node GPU power is 150W (150,000,000 microWatts)
	nodeGPUPower := device.Power(150_000_000)
	nodeGPUEnergy := device.Energy(3000)
	snapshot.Node.GPUZones[0] = NodeUsage{
		EnergyTotal:  nodeGPUEnergy,
		ActivePower:  nodeGPUPower,
		activeEnergy: nodeGPUEnergy,
	}

	// Create 3 processes
	proc1 := &Process{PID: 1001, Comm: "proc1", GPUZones: make(GPUUsageMap)}
	proc2 := &Process{PID: 1002, Comm: "proc2", GPUZones: make(GPUUsageMap)}
	proc3 := &Process{PID: 1003, Comm: "proc3", GPUZones: make(GPUUsageMap)}
	snapshot.Processes["1001"] = proc1
	snapshot.Processes["1002"] = proc2
	snapshot.Processes["1003"] = proc3

	// Calculate GPU power
	pm.calculateProcessGPUPower(snapshot, nil)

	// Verify that sum of process GPU powers equals node GPU power
	totalProcessPower := proc1.GPUZones[0].Power + proc2.GPUZones[0].Power + proc3.GPUZones[0].Power
	totalProcessEnergy := proc1.GPUZones[0].EnergyTotal + proc2.GPUZones[0].EnergyTotal + proc3.GPUZones[0].EnergyTotal

	assert.Equal(t, nodeGPUPower, totalProcessPower, "Sum of process GPU powers should equal node GPU power")
	assert.Equal(t, nodeGPUEnergy, totalProcessEnergy, "Sum of process GPU energies should equal node GPU energy")

	// Verify individual power ratios
	// Total SM util = 30 + 50 + 20 = 100
	// Process 1: 30/100 = 0.3 -> 45W
	// Process 2: 50/100 = 0.5 -> 75W
	// Process 3: 20/100 = 0.2 -> 30W
	expectedProc1Power := device.Power(float64(nodeGPUPower) * 0.3)
	expectedProc2Power := device.Power(float64(nodeGPUPower) * 0.5)
	expectedProc3Power := device.Power(float64(nodeGPUPower) * 0.2)

	assert.Equal(t, expectedProc1Power, proc1.GPUZones[0].Power, "Process 1 should get 30% of GPU power")
	assert.Equal(t, expectedProc2Power, proc2.GPUZones[0].Power, "Process 2 should get 50% of GPU power")
	assert.Equal(t, expectedProc3Power, proc3.GPUZones[0].Power, "Process 3 should get 20% of GPU power")

	mockGPU.AssertExpectations(t)
}

// TestProcessGPUPowerTimesliced tests GPU power attribution with timesliced/shared GPU
// where SM utilization doesn't add up to 100%
func TestProcessGPUPowerTimesliced(t *testing.T) {
	// Create mock GPU meter
	mockGPU := new(device.MockGPUPowerMeter)

	// Create mock GPU zone
	mockZone := new(mockGPUZone)
	mockZone.On("DeviceID").Return(uint(0))

	gpuZones := []device.GPUEnergyZone{mockZone}
	mockGPU.On("Zones").Return(gpuZones, nil)

	// Setup process utilization expectations for timesliced scenario
	// Process 1: 10% SM utilization
	// Process 2: 15% SM utilization
	// Total: 25% (GPU is not fully utilized, but we still distribute 100% of power)
	mockGPU.On("ProcessUtilization", 2001, uint(0)).Return(&device.GPUProcessUtilization{
		PID:           2001,
		GPUID:         0,
		SMUtilization: 10.0,
	}, nil)
	mockGPU.On("ProcessUtilization", 2002, uint(0)).Return(&device.GPUProcessUtilization{
		PID:           2002,
		GPUID:         0,
		SMUtilization: 15.0,
	}, nil)

	// Create mock CPU meter
	mockCPU := new(MockCPUPowerMeter)
	mockCPU.On("Zones").Return([]EnergyZone{}, nil)
	mockCPU.On("PrimaryEnergyZone").Return(nil, fmt.Errorf("no zones"))

	// Create power monitor with GPU
	pm := NewPowerMonitor(mockCPU, WithGPUPowerMeter(mockGPU))

	// Create a snapshot with processes
	snapshot := NewSnapshot()
	snapshot.Node.GPUZones = make(NodeGPUUsageMap)
	// Node GPU power is 200W (200,000,000 microWatts)
	nodeGPUPower := device.Power(200_000_000)
	nodeGPUEnergy := device.Energy(4000)
	snapshot.Node.GPUZones[0] = NodeUsage{
		EnergyTotal:  nodeGPUEnergy,
		ActivePower:  nodeGPUPower,
		activeEnergy: nodeGPUEnergy,
	}

	// Create 2 processes
	proc1 := &Process{PID: 2001, Comm: "proc1", GPUZones: make(GPUUsageMap)}
	proc2 := &Process{PID: 2002, Comm: "proc2", GPUZones: make(GPUUsageMap)}
	snapshot.Processes["2001"] = proc1
	snapshot.Processes["2002"] = proc2

	// Calculate GPU power
	pm.calculateProcessGPUPower(snapshot, nil)

	// Verify that sum of process GPU powers equals node GPU power
	// Even though utilization is only 25%, we distribute 100% of the power
	totalProcessPower := proc1.GPUZones[0].Power + proc2.GPUZones[0].Power

	assert.Equal(t, nodeGPUPower, totalProcessPower, "Sum of process GPU powers should equal node GPU power (even with low utilization)")

	// Verify individual power ratios
	// Total SM util = 10 + 15 = 25
	// Process 1: 10/25 = 0.4 -> 80W
	// Process 2: 15/25 = 0.6 -> 120W
	expectedProc1Power := device.Power(float64(nodeGPUPower) * (10.0 / 25.0))
	expectedProc2Power := device.Power(float64(nodeGPUPower) * (15.0 / 25.0))

	assert.Equal(t, expectedProc1Power, proc1.GPUZones[0].Power, "Process 1 should get 40% of GPU power (10/25)")
	assert.Equal(t, expectedProc2Power, proc2.GPUZones[0].Power, "Process 2 should get 60% of GPU power (15/25)")

	mockGPU.AssertExpectations(t)
}

// TestContainerGPUPowerAggregation tests GPU power aggregation from processes to containers
func TestContainerGPUPowerAggregation(t *testing.T) {
	// Create mock GPU meter
	mockGPU := new(device.MockGPUPowerMeter)
	mockCPU := new(MockCPUPowerMeter)
	zones := []EnergyZone{}
	mockCPU.On("Zones").Return(zones, nil)
	mockCPU.On("PrimaryEnergyZone").Return(nil, fmt.Errorf("no zones"))
	pm := NewPowerMonitor(mockCPU, WithGPUPowerMeter(mockGPU))

	// Create a snapshot with processes and containers
	snapshot := NewSnapshot()

	// Add container
	container := &Container{
		ID:       "container-1",
		Name:     "test-container",
		GPUZones: make(GPUUsageMap),
	}
	snapshot.Containers["container-1"] = container

	// Add processes belonging to the container
	process1 := &Process{
		PID:         1234,
		ContainerID: "container-1",
		GPUZones: GPUUsageMap{
			0: Usage{Power: 50, EnergyTotal: 1000},
			1: Usage{Power: 20, EnergyTotal: 500},
		},
	}
	process2 := &Process{
		PID:         5678,
		ContainerID: "container-1",
		GPUZones: GPUUsageMap{
			0: Usage{Power: 30, EnergyTotal: 600},
			1: Usage{Power: 40, EnergyTotal: 800},
		},
	}
	snapshot.Processes["1234"] = process1
	snapshot.Processes["5678"] = process2

	// Calculate container GPU power
	pm.calculateContainerGPUPower(snapshot)

	// Verify aggregation
	assert.Equal(t, Power(80), container.GPUZones[0].Power)          // 50 + 30
	assert.Equal(t, Energy(1600), container.GPUZones[0].EnergyTotal) // 1000 + 600
	assert.Equal(t, Power(60), container.GPUZones[1].Power)          // 20 + 40
	assert.Equal(t, Energy(1300), container.GPUZones[1].EnergyTotal) // 500 + 800
}

// TestPodGPUPowerAggregation tests GPU power aggregation from containers to pods
func TestPodGPUPowerAggregation(t *testing.T) {
	// Create mock GPU meter
	mockGPU := new(device.MockGPUPowerMeter)
	mockCPU := new(MockCPUPowerMeter)
	zones := []EnergyZone{}
	mockCPU.On("Zones").Return(zones, nil)
	mockCPU.On("PrimaryEnergyZone").Return(nil, fmt.Errorf("no zones"))
	pm := NewPowerMonitor(mockCPU, WithGPUPowerMeter(mockGPU))

	// Create a snapshot with containers and pods
	snapshot := NewSnapshot()

	// Add pod
	pod := &Pod{
		ID:       "pod-1",
		Name:     "test-pod",
		GPUZones: make(GPUUsageMap),
	}
	snapshot.Pods["pod-1"] = pod

	// Add containers belonging to the pod
	container1 := &Container{
		ID:    "container-1",
		PodID: "pod-1",
		GPUZones: GPUUsageMap{
			0: Usage{Power: 80, EnergyTotal: 1600},
		},
	}
	container2 := &Container{
		ID:    "container-2",
		PodID: "pod-1",
		GPUZones: GPUUsageMap{
			0: Usage{Power: 40, EnergyTotal: 800},
			1: Usage{Power: 60, EnergyTotal: 1200},
		},
	}
	snapshot.Containers["container-1"] = container1
	snapshot.Containers["container-2"] = container2

	// Calculate pod GPU power
	pm.calculatePodGPUPower(snapshot)

	// Verify aggregation
	assert.Equal(t, Power(120), pod.GPUZones[0].Power)         // 80 + 40
	assert.Equal(t, Energy(2400), pod.GPUZones[0].EnergyTotal) // 1600 + 800
	assert.Equal(t, Power(60), pod.GPUZones[1].Power)          // 0 + 60
	assert.Equal(t, Energy(1200), pod.GPUZones[1].EnergyTotal) // 0 + 1200
}

// mockGPUZone is a mock implementation of GPUEnergyZone for testing
type mockGPUZone struct {
	mock.Mock
}

func (m *mockGPUZone) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *mockGPUZone) Index() int {
	args := m.Called()
	return args.Int(0)
}

func (m *mockGPUZone) Path() string {
	args := m.Called()
	return args.String(0)
}

func (m *mockGPUZone) Energy() (device.Energy, error) {
	args := m.Called()
	return args.Get(0).(device.Energy), args.Error(1)
}

func (m *mockGPUZone) MaxEnergy() device.Energy {
	args := m.Called()
	return args.Get(0).(device.Energy)
}

func (m *mockGPUZone) DeviceID() uint {
	args := m.Called()
	return args.Get(0).(uint)
}
