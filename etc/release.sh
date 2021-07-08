#!/bin/bash

set -euo pipefail

docker build . -t pachyderm/config-pod:${CIRCLE_TAG}

echo "$DOCKERHUB_PASS" | docker login -u pachydermbuildbot --password-stdin
docker push pachyderm/config-pod:${CIRCLE_TAG}
