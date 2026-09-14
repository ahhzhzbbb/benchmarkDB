.PHONY: build test clean docker-build docker-push run lint

# Binary name
BINARY := benchmark
# Docker image
IMAGE := db-benchmark
TAG := latest

## build: Build the benchmark binary
build:
	go build -ldflags="-w -s" -o $(BINARY) ./cmd/benchmark

## test: Run all tests
test:
	go test -v -race -count=1 ./...

## test-unit: Run unit tests only
test-unit:
	go test -v -race -count=1 -short ./...

## lint: Run go vet
lint:
	go vet ./...

## clean: Remove build artifacts
clean:
	rm -f $(BINARY)

## run-redis: Run benchmark against Redis with example config
run-redis:
	go run ./cmd/benchmark run --config configs/example.yaml --database-type redis

## run-aerospike: Run benchmark against Aerospike with example config
run-aerospike:
	go run ./cmd/benchmark run --config configs/example.yaml --database-type aerospike

## check: Run connectivity check
check:
	go run ./cmd/benchmark check --config configs/example.yaml

## docker-build: Build Docker image
docker-build:
	docker build -t $(IMAGE):$(TAG) -f deploy/Dockerfile .

## docker-push: Push Docker image (set REGISTRY env var)
docker-push:
	docker tag $(IMAGE):$(TAG) $(REGISTRY)/$(IMAGE):$(TAG)
	docker push $(REGISTRY)/$(IMAGE):$(TAG)

## k8s-deploy: Deploy benchmark job to Kubernetes
k8s-deploy:
	kubectl apply -f deploy/benchmark-job.yaml

## k8s-logs: View benchmark job logs
k8s-logs:
	kubectl logs job/db-benchmark -f

## k8s-clean: Remove benchmark job
k8s-clean:
	kubectl delete -f deploy/benchmark-job.yaml --ignore-not-found

## mod-tidy: Tidy Go modules
mod-tidy:
	go mod tidy

## help: Show this help message
help:
	@grep -E '^## ' Makefile | sed 's/## //' | column -t -s ':'
