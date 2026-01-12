// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"fmt"
	"net"
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
)

// DCGMMode represents the DCGM connection mode
type DCGMMode string

const (
	// DCGMModeEmbedded starts an embedded DCGM engine
	DCGMModeEmbedded DCGMMode = "embedded"
	// DCGMModeStandalone connects to an external DCGM host engine
	DCGMModeStandalone DCGMMode = "standalone"
)

// dcgmInterface defines the methods we use from the DCGM library
// This interface allows us to mock the DCGM library for testing
type dcgmInterface interface {
	// Init initializes DCGM in embedded mode (starts a local DCGM engine)
	Init() (cleanup func(), err error)
	// InitStandalone connects to an external DCGM host engine
	InitStandalone(address string) (cleanup func(), err error)
	WatchPidFieldsEx(updateFreq, maxKeepAge time.Duration, maxKeepSamples int, gpus ...uint) (dcgm.GroupHandle, error)
	FieldGroupCreate(fieldsGroupName string, fields []dcgm.Short) (dcgm.FieldHandle, error)
	WatchFieldsWithGroup(fieldsGroup dcgm.FieldHandle, group dcgm.GroupHandle) error
	GetValuesSince(gpuGroup dcgm.GroupHandle, fieldGroup dcgm.FieldHandle, sinceTime time.Time) ([]dcgm.FieldValue_v2, time.Time, error)
	GetProcessInfo(group dcgm.GroupHandle, pid uint) ([]dcgm.ProcessInfo, error)
	DestroyGroup(group dcgm.GroupHandle) error
	FieldGroupDestroy(fieldsGroup dcgm.FieldHandle) error
}

// defaultDCGMImpl is the default implementation that calls the actual DCGM library
type defaultDCGMImpl struct{}

// Init initializes DCGM in embedded mode (starts a local DCGM engine)
func (d *defaultDCGMImpl) Init() (func(), error) {
	// Embedded mode starts a DCGM engine within the process.
	// This requires DCGM libraries and a local NVIDIA GPU.
	return dcgm.Init(dcgm.Embedded)
}

// InitStandalone connects to an external DCGM host engine
// The address should be in the format "hostname:port" (e.g., "dcgm-exporter:5555")
func (d *defaultDCGMImpl) InitStandalone(address string) (func(), error) {
	// Validate the address format (must include host and port)
	_, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid DCGM address format %q (expected host:port): %w", address, err)
	}

	// Standalone mode connects to an external DCGM host engine.
	// The go-dcgm library's Init function for Standalone mode expects:
	//   args[0] = address in "host:port" format
	//   args[1] = "0" for TCP socket, "1" for Unix socket
	// We use TCP (0) for network connections to remote DCGM host engines.
	return dcgm.Init(dcgm.Standalone, address, "0")
}

func (d *defaultDCGMImpl) WatchPidFieldsEx(updateFreq, maxKeepAge time.Duration, maxKeepSamples int, gpus ...uint) (dcgm.GroupHandle, error) {
	return dcgm.WatchPidFieldsEx(updateFreq, maxKeepAge, maxKeepSamples, gpus...)
}

func (d *defaultDCGMImpl) FieldGroupCreate(fieldsGroupName string, fields []dcgm.Short) (dcgm.FieldHandle, error) {
	return dcgm.FieldGroupCreate(fieldsGroupName, fields)
}

func (d *defaultDCGMImpl) WatchFieldsWithGroup(fieldsGroup dcgm.FieldHandle, group dcgm.GroupHandle) error {
	return dcgm.WatchFieldsWithGroup(fieldsGroup, group)
}

func (d *defaultDCGMImpl) GetValuesSince(gpuGroup dcgm.GroupHandle, fieldGroup dcgm.FieldHandle, sinceTime time.Time) ([]dcgm.FieldValue_v2, time.Time, error) {
	return dcgm.GetValuesSince(gpuGroup, fieldGroup, sinceTime)
}

func (d *defaultDCGMImpl) GetProcessInfo(group dcgm.GroupHandle, pid uint) ([]dcgm.ProcessInfo, error) {
	return dcgm.GetProcessInfo(group, pid)
}

func (d *defaultDCGMImpl) DestroyGroup(group dcgm.GroupHandle) error {
	return dcgm.DestroyGroup(group)
}

func (d *defaultDCGMImpl) FieldGroupDestroy(fieldsGroup dcgm.FieldHandle) error {
	return dcgm.FieldGroupDestroy(fieldsGroup)
}

// dcgmLib is the instance used by the code, initialized to the default implementation
var dcgmLib dcgmInterface = &defaultDCGMImpl{}
