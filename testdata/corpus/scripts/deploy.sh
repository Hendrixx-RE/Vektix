#!/usr/bin/env bash
set -euo pipefail

REGISTRY="registry.internal.example.com"
IMAGE="$REGISTRY/vektix/app:latest"

echo "Pushing image to $IMAGE..."
docker push "$IMAGE"

echo "Applying Kubernetes rollout restart..."
kubectl rollout restart deployment/api-server
kubectl rollout status deployment/api-server --timeout=120s
