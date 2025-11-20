// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFakeGPUMeter(t *testing.T) {
	devices := []uint{0, 1}
	meter, err := NewFakeGPUMeter(
		devices,
		WithFakeGPUPowerBase(150.0),
		WithFakeGPUPowerRange(75.0),
		WithFakeGPUEnergyStep(2000.0),
	)
	assert.NoError(t, err)
	assert.NotNil(t, meter)

	// Test Name
	assert.Equal(t, "fake-gpu", meter.Name())

	// Test Zones
	zones, err := meter.Zones()
	assert.NoError(t, err)
	assert.Len(t, zones, 2)

	// Verify zone properties
	for i, zone := range zones {
		assert.Equal(t, "gpu", zone.Name())
		assert.Equal(t, int(devices[i]), zone.Index())
		assert.Equal(t, devices[i], zone.DeviceID())
		assert.Contains(t, zone.Path(), "fake:gpu:")

		// Test energy reading
		energy1, err := zone.Energy()
		assert.NoError(t, err)
		assert.Greater(t, uint64(energy1), uint64(0))

		// Energy should increase on next read
		energy2, err := zone.Energy()
		assert.NoError(t, err)
		assert.Greater(t, uint64(energy2), uint64(energy1))
	}
}

func TestFakeGPUMeterProcessPower(t *testing.T) {
	devices := []uint{0}
	meter, err := NewFakeGPUMeter(devices)
	assert.NoError(t, err)

	fakeMeter := meter.(*fakeGPUPowerMeter)

	// Should error before starting
	_, _, err = meter.ProcessPower(1234, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not started")

	// Start the meter
	err = meter.Start()
	assert.NoError(t, err)

	// Initially no process power
	power, energy, err := meter.ProcessPower(1234, 0)
	assert.NoError(t, err)
	assert.Equal(t, Power(0), power)
	assert.Equal(t, Energy(0), energy)

	// Set process power manually
	fakeMeter.SetProcessPower(1234, 0, Power(50*MicroWatt), Energy(1000*MicroJoule))

	// Should now return the set values
	power, energy, err = meter.ProcessPower(1234, 0)
	assert.NoError(t, err)
	assert.Equal(t, Power(50*MicroWatt), power)
	assert.Equal(t, Energy(1000*MicroJoule), energy)

	// Invalid GPU ID should error
	_, _, err = meter.ProcessPower(1234, 99)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not monitored")

	// Stop the meter
	err = meter.Stop()
	assert.NoError(t, err)
}

func TestFakeGPUMeterSimulation(t *testing.T) {
	devices := []uint{0, 1}
	meter, err := NewFakeGPUMeter(devices)
	assert.NoError(t, err)

	// Start the meter
	err = meter.Start()
	assert.NoError(t, err)

	// Wait for simulation to generate some data
	time.Sleep(2 * time.Second)

	// Check if any process has GPU usage (probabilistic, might not always pass)
	// Just verify the simulation doesn't crash
	testPIDs := []int{1234, 5678, 9012}
	hasAnyUsage := false

	for _, pid := range testPIDs {
		for _, gpuID := range devices {
			power, energy, err := meter.ProcessPower(pid, gpuID)
			assert.NoError(t, err)
			if power > 0 || energy > 0 {
				hasAnyUsage = true
				t.Logf("Process %d on GPU %d: power=%v, energy=%v", pid, gpuID, power, energy)
			}
		}
	}

	// Due to randomness, we can't assert hasAnyUsage is true
	// but at least verify no errors occurred
	t.Logf("Simulation ran successfully, hasAnyUsage=%v", hasAnyUsage)

	// Stop the meter
	err = meter.Stop()
	assert.NoError(t, err)
}

func TestFakeGPUMeterDefaults(t *testing.T) {
	// Test with no devices specified
	meter, err := NewFakeGPUMeter(nil)
	assert.NoError(t, err)

	zones, err := meter.Zones()
	assert.NoError(t, err)
	assert.Len(t, zones, 1)
	assert.Equal(t, uint(0), zones[0].DeviceID())
}
