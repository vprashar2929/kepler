#!/usr/bin/env bash
#
# This file is part of the Kepler project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# Copyright 2022 The Kepler Contributors
#

set -e


_registry_port="5001"
_registry_name="kind-registry"

CTR_CMD=${CTR_CMD-docker}

CONFIG_PATH="kind"
KIND_VERSION=${KIND_VERSION:-0.15.0}
KIND_MANIFESTS_DIR="$CONFIG_PATH/manifests"
CLUSTER_NAME=${KIND_CLUSTER_NAME:-kind}
REGISTRY_NAME=${REGISTRY_NAME:-kind-registry}
REGISTRY_PORT=${REGISTRY_PORT:-5001}
KIND_DEFAULT_NETWORK="kind"
MICROSHIFT_CONTAINER_NAME="microshift"

IMAGE_REPO=${IMAGE_REPO:-localhost:5001}
ESTIMATOR_REPO=${ESTIMATOR_REPO:-quay.io/sustainable_computing_io}
MODEL_SERVER_REPO=${MODEL_SERVER_REPO:-quay.io/sustainable_computing_io}
IMAGE_TAG=${IMAGE_TAG:-devel}

# check CPU arch
PLATFORM=$(uname -m)
case ${PLATFORM} in
x86_64* | i?86_64* | amd64*)
    ARCH="amd64"
    ;;
ppc64le)
    ARCH="ppc64le"
    ;;
aarch64* | arm64*)
    ARCH="arm64"
    ;;
s390x*)
    ARCH="s390x"
    ;;
*)
    echo "invalid Arch, only support x86_64, ppc64le, aarch64"
    exit 1
    ;;
esac

# the cluster kind is a kubernetes cluster
if [[ ${CLUSTER_PROVIDER} = "kind" ]]; then
    CLUSTER_PROVIDER="kubernetes"
fi

function _get_pods() {
     kubectl get pods --all-namespaces --no-headers
}

function _wait_containers_ready {
     echo "Waiting for all containers to become ready ..."
     namespace=$1
     kubectl wait --for=condition=Ready pod --all -n "$namespace" --timeout 12m
}

function stop_microshift_container() {
    $CTR_CMD stop microshift
}

function wait_microshift_up {
    # wait till container is in running state
    while [ "$(${CTR_CMD} inspect -f '{{.State.Status}}' "${MICROSHIFT_CONTAINER_NAME}")" != "running" ]; do
        echo "Waiting for container ${MICROSHIFT_CONTAINER_NAME} to start..."
        sleep 5
    done
    echo "Container $MICROSHIFT_CONTAINER_NAME} is now running!"

    echo "Waiting for cluster to be ready ..."

    while [ -z "$($CTR_CMD exec --privileged "${MICROSHIFT_CONTAINER_NAME}" \
        kubectl --kubeconfig=/var/lib/microshift/resources/kubeadmin/kubeconfig \
        get nodes -o=jsonpath='{.items..status.conditions[-1:].status}' | grep True)" ]; do
        echo "Waiting for microshift cluster to be ready ..."
        sleep 10
    done

    sleep 60

    while [ -n "$(_get_pods | grep -v Running)" ]; do
        echo "Waiting for all pods to enter the Running state ..."
        _get_pods | >&2 grep -v Running || true
        sleep 10
    done
     _wait_containers_ready kube-system
 }
