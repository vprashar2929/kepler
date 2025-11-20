<!-- SPDX-FileCopyrightText: 2025 The Kepler Authors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# GPU Monitoring Troubleshooting Guide

## Problem: "NVML doesn't exist on this system"

### Root Cause

The error occurs due to a **library version mismatch** between the NVIDIA libraries baked into the Kepler container image and the NVIDIA driver running on the OpenShift host.

**What's happening:**
1. The Dockerfile installs `libnvidia-ml` package (e.g., version 580.105.08)
2. The OpenShift node has NVIDIA driver installed by GPU Operator (e.g., version 580.95.05)
3. When Kepler tries to call `dcgm.Init()`, it uses the baked-in library version
4. The library version doesn't match the kernel driver version → NVML initialization fails

### Why Runtime Class Alone Doesn't Fix It

Setting `runtimeClassName: nvidia` tells the NVIDIA Container Toolkit to inject GPU resources, but if the container **already has** NVIDIA libraries installed, those take precedence in the library search path, causing the version mismatch.

## Solution

### 1. Rebuild the Container Image (Recommended)

Modify the Dockerfile to **remove** the `libnvidia-ml` package installation:

```dockerfile
# Before (❌ causes version mismatch)
RUN dnf install -y \
      datacenter-gpu-manager-4-core \
      libnvidia-ml && \
    dnf clean all

# After (✅ lets runtime inject correct version)
RUN dnf install -y \
      datacenter-gpu-manager-4-core && \
    dnf clean all
```

Update the `LD_LIBRARY_PATH` to include the runtime-injected libraries:

```dockerfile
ENV LD_LIBRARY_PATH=/usr/lib64:/usr/local/dcgm/lib64:/usr/local/nvidia/lib64
```

### 2. Verify the Fix

After rebuilding and redeploying:

```bash
# Check that NVIDIA libraries are injected at runtime
oc exec -n kepler <pod-name> -- ls -la /usr/local/nvidia/lib64/libnvidia-ml.so*

# Check that devices are accessible
oc exec -n kepler <pod-name> -- ls -la /dev/nvidia*

# Check logs for successful initialization
oc logs -n kepler <pod-name> | grep "GPU power monitoring"
```

You should see:
```
level=INFO msg="GPU power monitoring is enabled (experimental)" type=dcgm devices=[0]
level=INFO msg="Using DCGM GPU meter"
```

## Alternative: Version Matching

If you cannot modify the Dockerfile, ensure the `libnvidia-ml` version in the image matches the driver version on your nodes:

```bash
# Check host driver version
oc get nodes -o json | jq -r '.items[].metadata.labels."nvidia.com/cuda.driver-version.full"' | head -1

# Install matching version in Dockerfile
RUN dnf install -y libnvidia-ml-<matching-version>
```

**Drawback**: This ties your image to specific driver versions and breaks when nodes are upgraded.

## How NVIDIA Container Toolkit Works

When you set `runtimeClassName: nvidia`:

1. **Device Plugin** allocates GPU resources based on `nvidia.com/gpu: 1` limit
2. **Container Toolkit Hook** is triggered during container creation
3. **Toolkit** mounts:
   - GPU device files (`/dev/nvidia*`) into the container
   - NVIDIA driver libraries into `/usr/local/nvidia/lib64`
   - NVIDIA binaries into `/usr/local/nvidia/bin`
4. **Runtime** starts the container with injected resources

If libraries are pre-installed in the image, step 3's injected libraries may not be used due to `LD_LIBRARY_PATH` precedence.

## Verification Commands

### Check GPU Operator Status
```bash
oc get pods -n nvidia-gpu-operator
oc get nodes -l nvidia.com/gpu.present=true
```

### Check Runtime Classes
```bash
oc get runtimeclass
# Should show: nvidia, nvidia-cdi, nvidia-legacy
```

### Check SCC Assignment
```bash
oc get pod -n kepler <pod-name> -o jsonpath='{.metadata.annotations.openshift\.io/scc}'
# Should show: kepler-dcgm-scc or privileged
```

### Check Environment Variables
```bash
oc get pod -n kepler <pod-name> -o jsonpath='{.spec.containers[0].env}' | jq
# Should include NVIDIA_VISIBLE_DEVICES and NVIDIA_DRIVER_CAPABILITIES
```

### Test DCGM Access
```bash
# From inside a working container
oc exec -n kepler <pod-name> -- dcgmi discovery -l
```

## References

- [NVIDIA Container Toolkit Documentation](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/)
- [OpenShift GPU Operator Guide](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/openshift/contents.html)
- [DCGM Documentation](https://docs.nvidia.com/datacenter/dcgm/latest/)
