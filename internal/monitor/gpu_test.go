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

// TestProcessGPUPowerCollection tests GPU power collection for processes
func TestProcessGPUPowerCollection(t *testing.T) {
	// Create mock GPU meter
	mockGPU := new(device.MockGPUPowerMeter)

	// Create mock GPU zones
	mockZone1 := new(mockGPUZone)
	mockZone1.On("DeviceID").Return(uint(0))

	mockZone2 := new(mockGPUZone)
	mockZone2.On("DeviceID").Return(uint(1))

	gpuZones := []device.GPUEnergyZone{mockZone1, mockZone2}
	mockGPU.On("Zones").Return(gpuZones, nil)

	// Setup process power expectations
	mockGPU.On("ProcessPower", 1234, uint(0)).Return(device.Power(50), device.Energy(1000), nil)
	mockGPU.On("ProcessPower", 1234, uint(1)).Return(device.Power(0), device.Energy(0), nil).Maybe()

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
	snapshot.Node.GPUZones[0] = NodeUsage{EnergyTotal: 1000}
	snapshot.Node.GPUZones[1] = NodeUsage{EnergyTotal: 2000}

	process := &Process{
		PID:      1234,
		Comm:     "test-process",
		GPUZones: make(GPUUsageMap),
	}
	snapshot.Processes["1234"] = process

	// Calculate GPU power
	pm.calculateProcessGPUPower(snapshot, nil)

	// Verify GPU power was collected
	assert.Contains(t, process.GPUZones, uint(0))
	assert.Equal(t, device.Power(50), process.GPUZones[0].Power)
	assert.Equal(t, device.Energy(1000), process.GPUZones[0].EnergyTotal)

	mockGPU.AssertExpectations(t)
	mockZone1.AssertExpectations(t)
	mockZone2.AssertExpectations(t)
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
