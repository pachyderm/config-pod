#!/bin/bash

set -exuo pipefail

export PATH=$(pwd):$(pwd)/cached-deps:$GOPATH/bin:$PATH

PACH_ADDRESS="grpc://$(minikube ip):30650" go test -v .
