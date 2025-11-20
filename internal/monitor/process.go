// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package monitor

import (
	"fmt"
	"strconv"

	"github.com/sustainable-computing-io/kepler/internal/resource"
)

// firstProcessRead initializes process power data for the first time
func (pm *PowerMonitor) firstProcessRead(snapshot *Snapshot) error {
	running := pm.resources.Processes().Running
	processes := make(Processes, len(running))

	zones := snapshot.Node.Zones
	nodeCPUTimeDelta := pm.resources.Node().ProcessTotalCPUTimeDelta

	for _, proc := range running {
		process := newProcess(proc, zones)

		// Calculate initial energy based on CPU ratio * nodeActiveEnergy
		for zone, nodeZoneUsage := range zones {
			if nodeZoneUsage.ActivePower == 0 || nodeZoneUsage.activeEnergy == 0 || nodeCPUTimeDelta == 0 {
				continue
			}

			cpuTimeRatio := proc.CPUTimeDelta / nodeCPUTimeDelta
			activeEnergy := Energy(cpuTimeRatio * float64(nodeZoneUsage.activeEnergy))

			process.Zones[zone] = Usage{
				Power:       Power(0), // No power in first read - no delta time to calculate rate
				EnergyTotal: activeEnergy,
			}
		}

		processes[process.StringID()] = process
	}
	snapshot.Processes = processes

	// Initialize GPU power for processes if GPU meter is available
	if pm.gpu != nil {
		pm.initProcessGPUPower(snapshot)
	}

	pm.logger.Debug("Initialized process power tracking",
		"processes", len(processes),
	)
	return nil
}

func newProcess(proc *resource.Process, zones NodeZoneUsageMap) *Process {
	process := &Process{
		PID:          proc.PID,
		Comm:         proc.Comm,
		Exe:          proc.Exe,
		Type:         proc.Type,
		CPUTotalTime: proc.CPUTotalTime,
		Zones:        make(ZoneUsageMap, len(zones)),
		GPUZones:     make(GPUUsageMap),
	}

	// Initialize each zone with zero values
	for zone := range zones {
		process.Zones[zone] = Usage{
			EnergyTotal: Energy(0),
			Power:       Power(0),
		}
	}

	// Add the container ID if available
	if proc.Container != nil {
		process.ContainerID = proc.Container.ID
	}

	// Add the VM ID if available
	if proc.VirtualMachine != nil {
		process.VirtualMachineID = proc.VirtualMachine.ID
	}
	return process
}

// calculateProcessPower calculates process power for each running process
func (pm *PowerMonitor) calculateProcessPower(prev, newSnapshot *Snapshot) error {
	// Clear terminated workloads if snapshot has been exported
	if pm.exported.Load() {
		pm.logger.Debug("Clearing terminated processes after export")
		pm.terminatedProcessesTracker.Clear()
	}

	procs := pm.resources.Processes()

	pm.logger.Debug("Processing terminated processes", "terminated", len(procs.Terminated))
	for pid := range procs.Terminated {
		pidStr := fmt.Sprintf("%d", pid)
		prevProcess, exists := prev.Processes[pidStr]
		if !exists {
			continue
		}

		// Add to internal tracker (which will handle priority-based retention)
		// NOTE: Each terminated process is only added once since a process cannot be terminated twice
		pm.terminatedProcessesTracker.Add(prevProcess.Clone())
	}

	running := procs.Running

	zones := newSnapshot.Node.Zones
	nodeCPUTimeDelta := pm.resources.Node().ProcessTotalCPUTimeDelta
	pm.logger.Debug("Calculating Process power",
		"node.cpu.time", nodeCPUTimeDelta,
		"running", len(running),
	)

	// Initialize process map
	processMap := make(Processes, len(running))

	if len(running) == 0 {
		// this is odd!
		pm.logger.Warn("No running processes found, skipping running process power calculation")
	}

	for _, proc := range running {
		process := newProcess(proc, zones)
		pid := process.StringID() // to string

		// For each zone in the node, calculate process's share
		for zone, nodeZoneUsage := range zones {
			if nodeZoneUsage.ActivePower == 0 || nodeZoneUsage.activeEnergy == 0 || nodeCPUTimeDelta == 0 {
				continue
			}

			cpuTimeRatio := proc.CPUTimeDelta / nodeCPUTimeDelta
			// Calculate energy  for this interval
			activeEnergy := Energy(cpuTimeRatio * float64(nodeZoneUsage.activeEnergy))

			// Calculate absolute energy based on previous data
			absoluteEnergy := activeEnergy
			if prev, exists := prev.Processes[pid]; exists {
				if prevUsage, hasZone := prev.Zones[zone]; hasZone {
					absoluteEnergy += prevUsage.EnergyTotal
				}
			}

			// Calculate process's share of this zone's power and energy
			process.Zones[zone] = Usage{
				Power:       Power(cpuTimeRatio * nodeZoneUsage.ActivePower.MicroWatts()),
				EnergyTotal: absoluteEnergy,
			}
		}

		processMap[process.StringID()] = process
	}

	// Update the snapshot of running processes
	newSnapshot.Processes = processMap

	// Calculate GPU power for processes if GPU meter is available
	if pm.gpu != nil {
		pm.calculateProcessGPUPower(newSnapshot, prev)
	}

	// Populate terminated processes from tracker
	newSnapshot.TerminatedProcesses = pm.terminatedProcessesTracker.Items()
	pm.logger.Debug("snapshot updated for process",
		"running", len(newSnapshot.Processes),
		"terminated", len(newSnapshot.TerminatedProcesses),
	)

	return nil
}

// initProcessGPUPower initializes GPU power tracking for processes using ratio-based attribution.
// For the initial read, we collect utilization data and distribute the initial energy reading
// based on SM utilization ratios, consistent with calculateProcessGPUPower.
func (pm *PowerMonitor) initProcessGPUPower(snapshot *Snapshot) {
	if snapshot.Node.GPUZones == nil || len(snapshot.Node.GPUZones) == 0 {
		return
	}

	// Get GPU zones from the meter
	gpuZones, err := pm.gpu.Zones()
	if err != nil {
		pm.logger.Error("Failed to get GPU zones for process init", "error", err)
		return
	}

	// For each GPU, collect utilization and distribute initial energy
	for _, gpuZone := range gpuZones {
		gpuID := gpuZone.DeviceID()

		nodeGPUUsage, exists := snapshot.Node.GPUZones[gpuID]
		if !exists {
			continue
		}

		// Collect utilization data for all processes on this GPU
		type processUtilData struct {
			process       *Process
			smUtilization float64
		}
		var processUtils []processUtilData
		var totalSMUtil float64

		for pid, process := range snapshot.Processes {
			intPID, err := strconv.Atoi(pid)
			if err != nil {
				continue
			}

			// Initialize GPU zone for this process
			process.GPUZones[gpuID] = Usage{
				EnergyTotal: 0,
				Power:       0,
			}

			// Get process utilization from GPU meter
			util, err := pm.gpu.ProcessUtilization(intPID, gpuID)
			if err != nil {
				// No GPU usage for this process on this GPU
				continue
			}

			processUtils = append(processUtils, processUtilData{
				process:       process,
				smUtilization: util.SMUtilization,
			})
			totalSMUtil += util.SMUtilization
		}

		// Distribute initial energy based on utilization ratios
		// Note: Power is 0 for first read since we need Δt to calculate rate
		for _, putil := range processUtils {
			var initialEnergy Energy
			if totalSMUtil > 0 {
				utilizationRatio := putil.smUtilization / totalSMUtil
				initialEnergy = Energy(utilizationRatio * float64(nodeGPUUsage.ActiveEnergyTotal))
			}

			putil.process.GPUZones[gpuID] = Usage{
				EnergyTotal: initialEnergy,
				Power:       0, // No power in first read - no delta time to calculate rate
			}
		}
	}
}

// calculateProcessGPUPower calculates GPU power for each process using ratio-based attribution.
// This ensures that Sum(Process GPU Power) == Node GPU Power for each GPU device.
// The attribution is based on SM (Streaming Multiprocessor) utilization ratios:
// ProcessPower = (ProcessSMUtil / TotalSMUtil) * NodeGPUActivePower
func (pm *PowerMonitor) calculateProcessGPUPower(newSnapshot, prev *Snapshot) {
	if newSnapshot.Node.GPUZones == nil || len(newSnapshot.Node.GPUZones) == 0 {
		return
	}

	// Get GPU zones from the meter
	gpuZones, err := pm.gpu.Zones()
	if err != nil {
		pm.logger.Error("Failed to get GPU zones for process attribution", "error", err)
		return
	}

	// For each GPU, collect all process utilizations first, then distribute power
	for _, gpuZone := range gpuZones {
		gpuID := gpuZone.DeviceID()

		// Get node GPU power for this device
		nodeGPUUsage, exists := newSnapshot.Node.GPUZones[gpuID]
		if !exists {
			continue
		}

		// Collect utilization data for all processes on this GPU
		type processUtilData struct {
			pid           string
			process       *Process
			smUtilization float64
			energy        Energy
		}
		var processUtils []processUtilData
		var totalSMUtil float64

		for pid, process := range newSnapshot.Processes {
			intPID, err := strconv.Atoi(pid)
			if err != nil {
				pm.logger.Warn("Invalid PID for GPU power calculation", "pid", pid, "error", err)
				continue
			}

			// Get process utilization from GPU meter
			util, err := pm.gpu.ProcessUtilization(intPID, gpuID)
			if err != nil {
				// No GPU usage for this process on this GPU
				continue
			}

			processUtils = append(processUtils, processUtilData{
				pid:           pid,
				process:       process,
				smUtilization: util.SMUtilization,
				energy:        util.EnergyConsumed,
			})
			totalSMUtil += util.SMUtilization
		}

		// Distribute node GPU power based on utilization ratios
		for _, putil := range processUtils {
			var power Power
			var activeEnergy Energy

			if totalSMUtil > 0 {
				// Calculate power ratio: ProcessPower = (ProcessSMUtil / TotalSMUtil) * NodeGPUActivePower
				utilizationRatio := putil.smUtilization / totalSMUtil
				power = Power(utilizationRatio * nodeGPUUsage.ActivePower.MicroWatts())
				// Calculate energy share based on the same ratio
				activeEnergy = Energy(utilizationRatio * float64(nodeGPUUsage.activeEnergy))
			}

			// Calculate absolute energy based on previous data
			absoluteEnergy := activeEnergy
			if prev != nil && prev.Processes != nil {
				if prevProc, exists := prev.Processes[putil.pid]; exists {
					if prevUsage, hasGPU := prevProc.GPUZones[gpuID]; hasGPU {
						absoluteEnergy += prevUsage.EnergyTotal
					}
				}
			}

			putil.process.GPUZones[gpuID] = Usage{
				Power:       power,
				EnergyTotal: absoluteEnergy,
			}
		}

		pm.logger.Debug("GPU power attribution",
			"gpu", gpuID,
			"node_power", nodeGPUUsage.ActivePower,
			"total_sm_util", totalSMUtil,
			"processes", len(processUtils),
		)
	}
}
