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

set -ex pipefail

source ./hack/common.sh

CLUSTER_PROVIDER=${CLUSTER_PROVIDER:-kubernetes}
MANIFESTS_OUT_DIR=${MANIFESTS_OUT_DIR:-"_output/generated-manifest"}

function check_deployment_status() {
    # round for 3 times and each for 60s
    # check if the rollout status is running
    deploy_status=1
    for i in 1 2 3
    do
        echo "check deployment status for round $i"
        kubectl rollout status daemonset kepler-exporter -n kepler --timeout 5m
        #check rollout status
        if [ $? -eq 0 ]
        then
            deploy_status=0
            break
        fi
    done 
    # if deployment in error
    if test $[deploy_status] -eq 1
    then
        echo "check the status of the kepler-exporter"
        kubectl -n kepler describe daemonset.apps/kepler-exporter
        echo "check the logs of the kepler-exporter"
        kubectl -n kepler logs daemonset.apps/kepler-exporter
    else
        sleep 60
        echo "check if kepler is still alive"
        kubectl logs $(kubectl -n kepler get pods -o name) -n kepler
        kubectl get all -n kepler
    fi
}

function intergration_test() {
    $CTR_CMD ps -a
    mkdir -p /tmp/.kube
    if [ "$CLUSTER_PROVIDER" == "microshift" ]
    then
        $CTR_CMD exec -i microshift cat /var/lib/microshift/resources/kubeadmin/kubeconfig > /tmp/.kube/config
    else
        kind get kubeconfig --name=kind > /tmp/.kube/config
    fi
    kubectl port-forward --address localhost $(kubectl -n kepler get pods -o name) 9102:9102 -n kepler -v7 &
    kubectl logs -n kepler daemonset/kepler-exporter
    kubectl get pods -n kepler -o yaml
    go test ./e2e/... --tags bcc -v --race --bench=. -cover --count=1 --vet=all
}

function main() {
    # verify the deployment of cluster
    case $1 in
    kepler)
        check_deployment_status
        ;;
    test)
        intergration_test
        ;;
    *)
        check_deployment_status
        intergration_test
        ;;
    esac
}

main "$@"
