.PHONY: test vet build docker deploy sell-agent smoke status logs

IMAGE ?= canary402:dev
VERSION ?= dev
GOCACHE ?= /tmp/canary402-gocache

test:
	GOCACHE=$(GOCACHE) go test ./...

vet:
	GOCACHE=$(GOCACHE) go vet ./...

build:
	GOCACHE=$(GOCACHE) go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o canary402 ./cmd/canary402

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

deploy:
	IMAGE=$(IMAGE) VERSION=$(VERSION) ./scripts/deploy-local.sh

sell-agent:
	./scripts/publish-agent.sh

smoke:
	./scripts/smoke-local.sh

status:
	obol kubectl -n llm get deploy,pod,svc,pvc -l app.kubernetes.io/name=canary402
	obol sell status canary402 -n agent-canary402

logs:
	obol kubectl -n llm logs deploy/canary402 -f
