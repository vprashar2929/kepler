<!-- SPDX-FileCopyrightText: 2025 The Kepler Authors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# OpenShift GPU-Enabled Kepler Deployment

This directory contains Kustomize manifests for deploying Kepler on OpenShift with NVIDIA GPU monitoring support.

## Prerequisites

1. **NVIDIA GPU Operator**: Must be installed and running on your OpenShift cluster
2. **GPU Nodes**: Nodes with NVIDIA GPUs and the label `nvidia.com/gpu.present: "true"`
3. **Privileged SCC**: The manifests grant the `privileged` SCC to the Kepler service account

## What's Different from Kubernetes

These manifests extend the base Kubernetes manifests with OpenShift-specific configurations:

1. **Security Context Constraints (SCC)**: Grants `privileged` SCC to allow hardware access
2. **GPU Resource Requests**: Requests `nvidia.com/gpu: 1` to trigger NVIDIA device plugin injection
3. **NVIDIA Environment Variables**: Sets `NVIDIA_VISIBLE_DEVICES` and `NVIDIA_DRIVER_CAPABILITIES` for proper driver access
4. **Node Selector**: Ensures Kepler only runs on GPU-enabled nodes
5. **GPU Configuration**: Enables DCGM-based GPU monitoring in the ConfigMap

## Deployment

Deploy using `oc` or `kubectl`:

```bash
oc apply -k manifests/openshift
```

Or with `kustomize`:

```bash
kustomize build manifests/openshift | oc apply -f -
```

## Verification

Check that Kepler pods are running on GPU nodes:

```bash
oc get pods -n kepler -o wide
```

Check logs for successful GPU initialization:

```bash
oc logs -n kepler -l app.kubernetes.io/name=kepler -f
```

You should see:
```
level=INFO msg="GPU power monitoring is enabled (experimental)" type=dcgm devices=[0]
level=INFO msg="Using DCGM GPU meter"
```

## Troubleshooting

### "NVML doesn't exist on this system"

This error indicates the NVIDIA driver libraries are not accessible or there's a version mismatch.

**Root Cause**: The Kepler container image has NVIDIA libraries baked in (version 580.105.08), but the NVIDIA Container Toolkit needs to inject the host's driver version (e.g., 580.95.05) for proper compatibility.

**Solution**: The Dockerfile should be modified to NOT install `libnvidia-ml` package. Instead, rely on the NVIDIA Container Toolkit to inject the correct driver libraries at runtime via the `runtimeClassName: nvidia`.

**Temporary Workaround**: If you cannot rebuild the image, you can:
1. Ensure the driver version in the image matches the host
2. Or use a base image without pre-installed NVIDIA libraries

**Verify**:
1. GPU Operator is running: `oc get pods -n nvidia-gpu-operator`
2. Node has GPU label: `oc get nodes -l nvidia.com/gpu.present=true`
3. GPU resource is available: `oc describe node <node-name> | grep nvidia.com/gpu`
4. Check driver version match:
   ```bash
   # Host driver version
   oc get nodes -o json | jq '.items[].metadata.labels."nvidia.com/cuda.driver-version.full"'

   # Container library version
   oc run test --image=<kepler-image> --restart=Never --command -- sh -c 'ls -la /usr/lib64/libnvidia-ml.so*'
   ```

### Kepler pod not scheduled

If the pod remains in `Pending`:

```bash
oc describe pod -n kepler <pod-name>
```

Common causes:
- No nodes with `nvidia.com/gpu.present: "true"` label
- GPU resources exhausted on available nodes
- SCC not properly granted (check with `oc get scc privileged -o yaml`)

### Adjusting GPU Device IDs

If your GPUs are not at index 0, update the ConfigMap in `gpu-patch.yaml`:

```yaml
experimental:
  platform:
    gpu:
      devices: [0, 1]  # Monitor GPUs 0 and 1
```

## Files

- `kustomization.yaml`: Main Kustomize file that references base k8s manifests
- `scc.yaml`: Grants privileged SCC to Kepler service account
- `gpu-patch.yaml`: Patches ConfigMap and DaemonSet for GPU support
- `README.md`: This file

## Security Considerations

Kepler requires `privileged` SCC because it needs:
- Access to `/sys` and `/proc` for hardware counter reads
- Access to RAPL energy counters
- Access to NVIDIA device files and libraries

In production, consider:
- Restricting Kepler to specific nodes using node selectors
- Using Pod Security Admission with a custom profile
- Regular security audits of the Kepler service account permissions
