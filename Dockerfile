# Build the binary
FROM golang:1.24 AS builder

ARG VERSION
ARG GIT_COMMIT
ARG GIT_BRANCH

WORKDIR /workspace

COPY . .

RUN make build \
  CGO_ENABLED=1 \
  PRODUCTION=1 \
  VERSION=${VERSION} \
  GIT_COMMIT=${GIT_COMMIT} \
  GIT_BRANCH=${GIT_BRANCH}

FROM registry.access.redhat.com/ubi9:latest

# Install DCGM libraries for GPU monitoring
# NOTE: We do NOT install libnvidia-ml here. The NVIDIA Container Toolkit
# will inject the correct version at runtime when runtimeClassName: nvidia is used.
# This ensures driver version compatibility between container and host.
RUN curl -s https://developer.download.nvidia.com/compute/cuda/repos/rhel9/x86_64/cuda-rhel9.repo \
    -o /etc/yum.repos.d/cuda-rhel9.repo && \
    dnf install -y \
      datacenter-gpu-manager-4-core && \
    dnf clean all

# The NVIDIA Container Toolkit will inject libraries into /usr/local/nvidia at runtime
# Ensure DCGM libraries and injected NVIDIA libraries are in the library path
ENV LD_LIBRARY_PATH=/usr/lib64:/usr/local/dcgm/lib64:/usr/local/nvidia/lib64

COPY --from=builder /workspace/bin/kepler-release /usr/bin/kepler

ENTRYPOINT ["/usr/bin/kepler"]
