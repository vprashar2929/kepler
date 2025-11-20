// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package monitor

import (
	"errors"
	"time"
)

func (pm *PowerMonitor) calculateNodePower(prevNode, newNode *Node) error {
	// Get previous measurements for calculating watts
	prevReadTime := prevNode.Timestamp
	prevZones := prevNode.Zones

	now := pm.clock.Now()
	newNode.Timestamp = now

	// get zones first, before locking for read
	zones, err := pm.cpu.Zones()
	if err != nil {
		return err
	}

	nodeCPUTimeDelta := pm.resources.Node().ProcessTotalCPUTimeDelta
	nodeCPUUsageRatio := pm.resources.Node().CPUUsageRatio
	newNode.UsageRatio = nodeCPUUsageRatio

	pm.logger.Debug("Calculating Node power",
		"node.process-cpu.time", nodeCPUTimeDelta,
		"node.cpu.usage-ratio", nodeCPUUsageRatio,
	)

	// NOTE: energy is in MicroJoules and Power is in MicroWatts
	timeDiff := now.Sub(prevReadTime).Seconds()
	// Get the current energy

	var retErr error
	for _, zone := range zones {
		absEnergy, err := zone.Energy()
		if err != nil {
			retErr = errors.Join(err)
			pm.logger.Warn("Could not read energy for zone", "zone", zone.Name(), "index", zone.Index(), "error", err)
			continue
		}

		// Calculate watts and joules diff if we have previous data for the zone
		var activeEnergy, activeEnergyTotal, idleEnergyTotal Energy
		var power, activePower, idlePower Power

		if prevZone, ok := prevZones[zone]; ok {
			// Absolute is a running total, so to find the current energy usage, calculate the delta
			// delta = current - previous
			// active = delta * cpuUsage
			// idle = delta - active

			deltaEnergy := calculateEnergyDelta(absEnergy, prevZone.EnergyTotal, zone.MaxEnergy())

			activeEnergy = Energy(float64(deltaEnergy) * nodeCPUUsageRatio)
			idleEnergy := deltaEnergy - activeEnergy

			activeEnergyTotal = prevZone.ActiveEnergyTotal + activeEnergy
			idleEnergyTotal = prevZone.IdleEnergyTotal + idleEnergy

			powerF64 := float64(deltaEnergy) / float64(timeDiff)
			power = Power(powerF64)
			activePower = Power(powerF64 * nodeCPUUsageRatio)
			idlePower = power - activePower
		}

		newNode.Zones[zone] = NodeUsage{
			EnergyTotal: absEnergy,

			activeEnergy:      activeEnergy,
			ActiveEnergyTotal: activeEnergyTotal,
			IdleEnergyTotal:   idleEnergyTotal,

			Power:       power,
			ActivePower: activePower,
			IdlePower:   idlePower,
		}
	}

	// Collect GPU power data if available
	if pm.gpu != nil {
		pm.collectNodeGPUData(newNode, prevNode, time.Duration(timeDiff), nodeCPUUsageRatio)
	}

	return retErr
}

// Calculate joules difference handling wraparound
func calculateEnergyDelta(current, previous, maxJoules Energy) Energy {
	if current >= previous {
		return current - previous
	}

	// counter wraparound
	if maxJoules > 0 {
		return (maxJoules - previous) + current
	}

	return 0 // Unable to calculate delta
}

// firstNodeRead reads the energy for the first time
func (pm *PowerMonitor) firstNodeRead(node *Node) error {
	node.Timestamp = pm.clock.Now()

	zones, err := pm.cpu.Zones()
	if err != nil {
		return err
	}

	nodeCPUUsageRatio := pm.resources.Node().CPUUsageRatio
	var retErr error
	for _, zone := range zones {
		energy, err := zone.Energy()
		if err != nil {
			retErr = errors.Join(err)
			pm.logger.Warn("Could not read energy for zone", "zone", zone.Name(), "index", zone.Index(), "error", err)
			continue
		}
		activeEnergy := Energy(float64(energy) * nodeCPUUsageRatio)
		idleEnergy := energy - activeEnergy

		node.Zones[zone] = NodeUsage{
			EnergyTotal:       energy,
			ActiveEnergyTotal: activeEnergy,
			IdleEnergyTotal:   idleEnergy,
			activeEnergy:      activeEnergy,
			// Power can't be calculated in the first read since we need Δt
		}
	}

	// Initialize GPU zones if GPU meter is available
	if pm.gpu != nil {
		pm.initNodeGPUData(node)
	}

	return retErr
}

// collectNodeGPUData collects GPU power data for the node
func (pm *PowerMonitor) collectNodeGPUData(node, prevNode *Node, timeDiff time.Duration, nodeCPUUsageRatio float64) {
	gpuZones, err := pm.gpu.Zones()
	if err != nil {
		pm.logger.Error("Failed to get GPU zones", "error", err)
		return
	}

	prevGPUZones := make(NodeGPUUsageMap)
	if prevNode != nil && prevNode.GPUZones != nil {
		prevGPUZones = prevNode.GPUZones
	}

	for _, zone := range gpuZones {
		gpuID := zone.DeviceID()

		// Get current energy reading
		absEnergy, err := zone.Energy()
		if err != nil {
			pm.logger.Warn("Could not read energy for GPU", "gpu", gpuID, "error", err)
			continue
		}

		// Calculate power and energy deltas
		var activeEnergy, activeEnergyTotal, idleEnergyTotal Energy
		var power, activePower, idlePower Power

		if prevUsage, ok := prevGPUZones[gpuID]; ok {
			deltaEnergy := calculateEnergyDelta(absEnergy, prevUsage.EnergyTotal, zone.MaxEnergy())

			// For GPUs, use full energy as active (no idle/active split for now)
			// GPU workloads are typically active when running
			activeEnergy = deltaEnergy

			activeEnergyTotal = prevUsage.ActiveEnergyTotal + activeEnergy
			idleEnergyTotal = prevUsage.IdleEnergyTotal // No idle energy for GPUs

			// Use instantaneous power from DCGM if available
			if instantPower, exists := pm.gpu.DevicePower(gpuID); exists {
				power = instantPower
				activePower = power // All GPU power is considered active
				idlePower = 0
			} else {
				// Fallback to calculating from energy delta if instantaneous power not available
				// Convert time from nanoseconds to seconds for power calculation
				// Power = Energy / Time = microJoules / seconds = microWatts
				timeInSeconds := timeDiff.Seconds()
				if timeInSeconds > 0 {
					powerF64 := float64(deltaEnergy) / timeInSeconds
					power = Power(powerF64)
					activePower = power // All GPU power is considered active
					idlePower = 0
				}
			}
		}

		node.GPUZones[gpuID] = NodeUsage{
			EnergyTotal:       absEnergy,
			activeEnergy:      activeEnergy,
			ActiveEnergyTotal: activeEnergyTotal,
			IdleEnergyTotal:   idleEnergyTotal,
			Power:             power,
			ActivePower:       activePower,
			IdlePower:         idlePower,
		}
	}
}

// initNodeGPUData initializes GPU zones for the first read
func (pm *PowerMonitor) initNodeGPUData(node *Node) {
	gpuZones, err := pm.gpu.Zones()
	if err != nil {
		pm.logger.Error("Failed to get GPU zones", "error", err)
		return
	}

	for _, zone := range gpuZones {
		gpuID := zone.DeviceID()

		absEnergy, err := zone.Energy()
		if err != nil {
			pm.logger.Warn("Could not read initial energy for GPU", "gpu", gpuID, "error", err)
			absEnergy = 0
		}

		node.GPUZones[gpuID] = NodeUsage{
			EnergyTotal: absEnergy,
		}
	}
}
