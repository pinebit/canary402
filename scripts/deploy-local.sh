#!/usr/bin/env sh
set -eu

IMAGE="${IMAGE:-canary402:dev}"
VERSION="${VERSION:-dev}"
CONTEXT="$(obol kubectl config current-context)"
CLUSTER="${CONTEXT#k3d-}"

if [ "$CLUSTER" = "$CONTEXT" ] || [ -z "$CLUSTER" ]; then
  echo "Could not derive the k3d cluster name from context: $CONTEXT" >&2
  exit 1
fi

echo "Building $IMAGE"
docker build --build-arg "VERSION=$VERSION" -t "$IMAGE" .

echo "Importing $IMAGE into k3d cluster $CLUSTER"
k3d image import "$IMAGE" --cluster "$CLUSTER"

echo "Applying Kubernetes resources"
obol kubectl apply -f deploy/k8s.yaml
obol kubectl -n llm rollout restart deployment/canary402
obol kubectl -n llm rollout status deployment/canary402 --timeout=120s

echo "Canary402 is running in namespace llm."
echo "Next: ./scripts/smoke-local.sh"
