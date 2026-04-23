.PHONY: build test run clean lint docker-build docker-push

BIN      := bin/argus
CMD      := ./cmd/argus
IMAGE    := lucientong/argus
TAG      ?= latest

build:
	@mkdir -p bin
	go build -o $(BIN) $(CMD)

test:
	go test -race ./...

run: build
	ARGUS_CONFIG=configs/config.yaml ./$(BIN)

clean:
	rm -rf bin/

lint:
	go vet ./...

# Build the Docker image locally.
docker-build:
	docker buildx build \
		--file Dockerfile \
		--tag $(IMAGE):$(TAG) \
		--load \
		.

# Push the Docker image to Docker Hub.
docker-push: docker-build
	docker push $(IMAGE):$(TAG)
