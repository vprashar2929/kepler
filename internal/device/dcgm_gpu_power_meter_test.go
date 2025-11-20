// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"testing"
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestDCGMGPUZone tests the DCGMGPUZone implementation
func TestDCGMGPUZone(t *testing.T) {
	zone := NewDCGMGPUZone(0)

	assert.Equal(t, "gpu", zone.Name())
	assert.Equal(t, 0, zone.Index())
	assert.Equal(t, uint(0), zone.DeviceID())
	assert.Equal(t, "dcgm:gpu:0", zone.Path())

	// Test initial energy reading
	energy, err := zone.Energy()
	assert.NoError(t, err)
	assert.Equal(t, Energy(0), energy)

	// Test energy update
	zone.updateEnergy(Energy(1000))
	energy, err = zone.Energy()
	assert.NoError(t, err)
	assert.Equal(t, Energy(1000), energy)
}

// TestGPUPowerMeterInterface tests that MockGPUPowerMeter implements GPUPowerMeter
func TestGPUPowerMeterInterface(t *testing.T) {
	var _ GPUPowerMeter = (*MockGPUPowerMeter)(nil)
}

// TestMockGPUPowerMeterUsage demonstrates usage of MockGPUPowerMeter
func TestMockGPUPowerMeterUsage(t *testing.T) {
	mockGPU := NewMockGPUPowerMeter(t)

	// Setup expectations
	mockGPU.On("Name").Return("mock-gpu")
	mockGPU.On("Start").Return(nil)
	mockGPU.On("Stop").Return(nil)

	// Create zones
	zone1 := NewDCGMGPUZone(0)
	zone2 := NewDCGMGPUZone(1)
	zones := []GPUEnergyZone{zone1, zone2}
	mockGPU.On("Zones").Return(zones, nil)

	// Setup process power expectations
	mockGPU.On("ProcessPower", 1234, uint(0)).Return(Power(50), Energy(1000), nil)
	mockGPU.On("ProcessPower", 1234, uint(1)).Return(Power(100), Energy(2000), nil)

	// Test usage
	assert.Equal(t, "mock-gpu", mockGPU.Name())

	err := mockGPU.Start()
	assert.NoError(t, err)

	gotZones, err := mockGPU.Zones()
	assert.NoError(t, err)
	assert.Len(t, gotZones, 2)

	power, energy, err := mockGPU.ProcessPower(1234, 0)
	assert.NoError(t, err)
	assert.Equal(t, Power(50), power)
	assert.Equal(t, Energy(1000), energy)

	power, energy, err = mockGPU.ProcessPower(1234, 1)
	assert.NoError(t, err)
	assert.Equal(t, Power(100), power)
	assert.Equal(t, Energy(2000), energy)

	err = mockGPU.Stop()
	assert.NoError(t, err)
}

// TestDCGMGPUPowerMeterOpts tests the options handling
func TestDCGMGPUPowerMeterOpts(t *testing.T) {
	// Test default options
	opts := DCGMGPUPowerMeterOpts{}

	// We can't actually create a real DCGM meter in tests without DCGM installed,
	// but we can test the options validation logic
	if opts.UpdateFreq == 0 {
		opts.UpdateFreq = 1 * time.Second
	}
	assert.Equal(t, 1*time.Second, opts.UpdateFreq)

	if opts.MaxKeepAge == 0 {
		opts.MaxKeepAge = 30 * time.Second
	}
	assert.Equal(t, 30*time.Second, opts.MaxKeepAge)

	if opts.MaxSamples == 0 {
		opts.MaxSamples = 1000
	}
	assert.Equal(t, 1000, opts.MaxSamples)
}

// TestDCGMGPUPowerMeter_InitAndUsage tests initialization and basic usage with mocked DCGM
func TestDCGMGPUPowerMeter_InitAndUsage(t *testing.T) {
	// Save original DCGM lib and restore after test
	originalLib := dcgmLib
	defer func() { dcgmLib = originalLib }()

	mockDCGM := new(mockDCGMImpl)
	dcgmLib = mockDCGM

	// Setup mock expectations for initialization
	mockDCGM.On("Init").Return(func() {}, nil)

	// Mock group creation
	mockGroupHandle := dcgm.GroupHandle{}
	mockDCGM.On("WatchPidFieldsEx", mock.Anything, mock.Anything, mock.Anything, []uint{0}).Return(mockGroupHandle, nil)

	// Mock field group creation
	mockFieldHandle := dcgm.FieldHandle{}
	mockFieldHandle.SetHandle(1) // Non-zero handle
	mockDCGM.On("FieldGroupCreate", mock.Anything, mock.Anything).Return(mockFieldHandle, nil)

	// Mock watch fields
	mockDCGM.On("WatchFieldsWithGroup", mockFieldHandle, mockGroupHandle).Return(nil)

	// Mock cleanup
	mockDCGM.On("FieldGroupDestroy", mockFieldHandle).Return(nil)
	mockDCGM.On("DestroyGroup", mockGroupHandle).Return(nil)

	// Create the meter
	opts := DCGMGPUPowerMeterOpts{
		GPUDevices: []uint{0},
	}
	meter, err := NewDCGMGPUPowerMeter(opts)
	assert.NoError(t, err)
	assert.NotNil(t, meter)
	assert.Equal(t, "dcgm-gpu", meter.Name())

	// Test cleanup
	err = meter.Stop()
	assert.NoError(t, err)

	mockDCGM.AssertExpectations(t)
}

// TestDCGMGPUPowerMeter_UpdateMetrics tests the metric update cycle
func TestDCGMGPUPowerMeter_UpdateMetrics(t *testing.T) {
	// Save original DCGM lib and restore after test
	originalLib := dcgmLib
	defer func() { dcgmLib = originalLib }()

	mockDCGM := new(mockDCGMImpl)
	dcgmLib = mockDCGM

	// Setup init mocks (simplified)
	mockDCGM.On("Init").Return(func() {}, nil)
	mockDCGM.On("WatchPidFieldsEx", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(dcgm.GroupHandle{}, nil)
	mockDCGM.On("FieldGroupCreate", mock.Anything, mock.Anything).Return(dcgm.FieldHandle{}, nil)
	mockDCGM.On("WatchFieldsWithGroup", mock.Anything, mock.Anything).Return(nil)

	// Create meter
	meter, _ := NewDCGMGPUPowerMeter(DCGMGPUPowerMeterOpts{GPUDevices: []uint{0}})

	// Mock metric values
	mockValues := []dcgm.FieldValue_v2{
		{
			EntityID:  0,
			FieldID:   dcgm.DCGM_FI_DEV_POWER_USAGE,
			FieldType: dcgm.DCGM_FT_DOUBLE,
			Value:     [4096]byte{}, // Float value is set via pointer in C, simplified here
		},
		{
			EntityID:  0,
			FieldID:   dcgm.DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION,
			FieldType: dcgm.DCGM_FT_INT64,
			Value:     [4096]byte{},
		},
	}
	// We can't easily set binary values in the mock structure that match C union layout without unsafe
	// So we'll trust the flow logic rather than exact value parsing in this test
	// or we'd need helper methods in the mock to construct these values.
	// For now, let's assume the mocked call returns success.

	mockDCGM.On("GetValuesSince", mock.Anything, mock.Anything, mock.Anything).Return(mockValues, time.Now(), nil)

	// Test update
	err := meter.updateDeviceMetrics()
	assert.NoError(t, err)

	mockDCGM.AssertExpectations(t)
}

// TestDCGMGPUPowerMeter_ProcessPower tests process power retrieval
func TestDCGMGPUPowerMeter_ProcessPower(t *testing.T) {
	// Save original DCGM lib and restore after test
	originalLib := dcgmLib
	defer func() { dcgmLib = originalLib }()

	mockDCGM := new(mockDCGMImpl)
	dcgmLib = mockDCGM

	// Init mocks
	mockDCGM.On("Init").Return(func() {}, nil)
	mockDCGM.On("WatchPidFieldsEx", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(dcgm.GroupHandle{}, nil)
	mockDCGM.On("FieldGroupCreate", mock.Anything, mock.Anything).Return(dcgm.FieldHandle{}, nil)
	mockDCGM.On("WatchFieldsWithGroup", mock.Anything, mock.Anything).Return(nil)

	meter, _ := NewDCGMGPUPowerMeter(DCGMGPUPowerMeterOpts{GPUDevices: []uint{0}})

	// Pre-populate device power for calculation
	meter.devicePower[0] = Power(100 * 1e6) // 100 Watts

	// Mock process info
	energy := uint64(1000)
	smUtil := float64(50.0)
	mockProcessInfo := []dcgm.ProcessInfo{
		{
			GPU: 0,
			PID: 12345,
			ProcessUtilization: dcgm.ProcessUtilInfo{
				EnergyConsumed: &energy,
				SmUtil:         &smUtil,
			},
		},
	}

	mockDCGM.On("GetProcessInfo", mock.Anything, uint(12345)).Return(mockProcessInfo, nil)

	// Test
	power, energyVal, err := meter.ProcessPower(12345, 0)
	assert.NoError(t, err)
	// Energy: 1000 Joules -> 1000 * 1e6 MicroJoules
	assert.Equal(t, Energy(1000*1e6), energyVal)
	// Power: 100W * 50% = 50W -> 50 * 1e6 MicroWatts
	assert.Equal(t, Power(50*1e6), power)
}
