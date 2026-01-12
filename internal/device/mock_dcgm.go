// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
	"github.com/stretchr/testify/mock"
)

// mockDCGMImpl is a mock implementation of dcgmInterface
type mockDCGMImpl struct {
	mock.Mock
}

func (m *mockDCGMImpl) Init() (func(), error) {
	calledArgs := m.Called()
	return calledArgs.Get(0).(func()), calledArgs.Error(1)
}

func (m *mockDCGMImpl) InitStandalone(address string) (func(), error) {
	calledArgs := m.Called(address)
	return calledArgs.Get(0).(func()), calledArgs.Error(1)
}

func (m *mockDCGMImpl) WatchPidFieldsEx(updateFreq, maxKeepAge time.Duration, maxKeepSamples int, gpus ...uint) (dcgm.GroupHandle, error) {
	calledArgs := m.Called(updateFreq, maxKeepAge, maxKeepSamples, gpus)
	return calledArgs.Get(0).(dcgm.GroupHandle), calledArgs.Error(1)
}

func (m *mockDCGMImpl) FieldGroupCreate(fieldsGroupName string, fields []dcgm.Short) (dcgm.FieldHandle, error) {
	calledArgs := m.Called(fieldsGroupName, fields)
	return calledArgs.Get(0).(dcgm.FieldHandle), calledArgs.Error(1)
}

func (m *mockDCGMImpl) WatchFieldsWithGroup(fieldsGroup dcgm.FieldHandle, group dcgm.GroupHandle) error {
	calledArgs := m.Called(fieldsGroup, group)
	return calledArgs.Error(0)
}

func (m *mockDCGMImpl) GetValuesSince(gpuGroup dcgm.GroupHandle, fieldGroup dcgm.FieldHandle, sinceTime time.Time) ([]dcgm.FieldValue_v2, time.Time, error) {
	calledArgs := m.Called(gpuGroup, fieldGroup, sinceTime)
	return calledArgs.Get(0).([]dcgm.FieldValue_v2), calledArgs.Get(1).(time.Time), calledArgs.Error(2)
}

func (m *mockDCGMImpl) GetProcessInfo(group dcgm.GroupHandle, pid uint) ([]dcgm.ProcessInfo, error) {
	calledArgs := m.Called(group, pid)
	return calledArgs.Get(0).([]dcgm.ProcessInfo), calledArgs.Error(1)
}

func (m *mockDCGMImpl) DestroyGroup(group dcgm.GroupHandle) error {
	calledArgs := m.Called(group)
	return calledArgs.Error(0)
}

func (m *mockDCGMImpl) FieldGroupDestroy(fieldsGroup dcgm.FieldHandle) error {
	calledArgs := m.Called(fieldsGroup)
	return calledArgs.Error(0)
}
