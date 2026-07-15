.PHONY: test vet build docker deploy sell sell-agent smoke status logs

IMAGE ?= canary402:dev
VERSION ?= dev
GOCACHE ?= /tmp/canary402-gocache
CANARY_HOSTNAME ?=

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

sell:
	CANARY_HOSTNAME=$(CANARY_HOSTNAME) ./scripts/publish-local.sh

sell-agent:
	./scripts/publish-agent.sh

smoke:
	./scripts/smoke-local.sh

status:
	obol kubectl -n llm get deploy,pod,svc,pvc -l app.kubernetes.io/name=canary402
	-obol sell status canary402 -n llm

logs:
	obol kubectl -n llm logs deploy/canary402 -f
