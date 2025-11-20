// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
)

// dcgmInterface defines the methods we use from the DCGM library
// This interface allows us to mock the DCGM library for testing
type dcgmInterface interface {
	Init() (cleanup func(), err error)
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

func (d *defaultDCGMImpl) Init() (func(), error) {
	// Use the dcgm.Mode type directly if exported, otherwise use unexported alias
	// Since we don't have access to unexported types, we rely on the method signature matching
	// what dcgm.Init actually accepts.
	//
	// NOTE: dcgm.Embedded is an untyped constant or a type we can't see easily,
	// but the function signature in api.go uses a private type 'mode'.
	// However, since we are wrapping it, we need to match what we can pass.
	//
	// Wait, looking at api.go: func Init(m mode, args ...string)
	// 'mode' is unexported. This makes it tricky to wrap directly if we can't name the type.
	// BUT, the constants like dcgm.Embedded are likely exported constants of that type.
	//
	// Actually, in Go, if the type is unexported, we can't implement an interface method with it
	// unless the interface is in the same package.
	//
	// We might need to use reflection or simply assume we can pass the constants which are likely ints.
	// Let's check what dcgm.Embedded is defined as.
	//
	// If we can't name the type 'mode', we can't define the interface method with it.
	// This implies we can't wrap Init() directly in an interface outside the dcgm package
	// IF 'mode' is truly unexported and used in the signature.
	//
	// Let's workaround this by NOT wrapping Init directly in the same way,
	// OR by using an interface that accepts 'any' or 'int' and we cast it?
	// No, we can't cast to an unexported type.
	//
	// However, we observed dcgm.Init(dcgm.Embedded) works.
	// If we change our interface to take (modeType any), we still can't pass it to dcgm.Init
	// without casting to the unexported type `dcgm.mode`.
	//
	// Since we are in 'device' package, we cannot refer to 'dcgm.mode'.
	//
	// Strategy:
	// We will wrap the *call* to dcgm.Init inside the implementation,
	// but our interface will accept the values that are effectively the constants.
	//
	// Let's assume we only support Embedded mode for now as that's what we use.
	// Or we can just make Init take nothing and hardcode Embedded in the default impl,
	// since that's what the code does: cleanup, err := dcgm.Init(dcgm.Embedded)
	return dcgm.Init(dcgm.Embedded)
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
