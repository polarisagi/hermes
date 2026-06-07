.PHONY: all build build-mac build-linux build-windows build-all clean

BINARY_NAME=hermes
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags="-s -w -X 'github.com/polarisagi/hermes/internal/app.Version=${VERSION}' -X 'github.com/polarisagi/hermes/internal/api.Version=${VERSION}'"

all: clean build

build-ui:
	cd web && npm install && npm run build

build: build-ui
	go build ${LDFLAGS} -o bin/${BINARY_NAME} ./cmd/hermes
	go build ${LDFLAGS} -o bin/adc-gen ./cmd/adc-gen

build-mac:
	GOOS=darwin GOARCH=arm64 go build ${LDFLAGS} -o bin/${BINARY_NAME}-darwin-arm64 ./cmd/hermes
	GOOS=darwin GOARCH=amd64 go build ${LDFLAGS} -o bin/${BINARY_NAME}-darwin-amd64 ./cmd/hermes
	GOOS=darwin GOARCH=arm64 go build ${LDFLAGS} -o bin/adc-gen-darwin-arm64 ./cmd/adc-gen
	GOOS=darwin GOARCH=amd64 go build ${LDFLAGS} -o bin/adc-gen-darwin-amd64 ./cmd/adc-gen

build-linux:
	GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o bin/${BINARY_NAME}-linux-amd64 ./cmd/hermes
	GOOS=linux GOARCH=arm64 go build ${LDFLAGS} -o bin/${BINARY_NAME}-linux-arm64 ./cmd/hermes
	GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o bin/adc-gen-linux-amd64 ./cmd/adc-gen
	GOOS=linux GOARCH=arm64 go build ${LDFLAGS} -o bin/adc-gen-linux-arm64 ./cmd/adc-gen

build-windows:
	GOOS=windows GOARCH=amd64 go build ${LDFLAGS} -o bin/${BINARY_NAME}-windows-amd64.exe ./cmd/hermes
	GOOS=windows GOARCH=arm64 go build ${LDFLAGS} -o bin/${BINARY_NAME}-windows-arm64.exe ./cmd/hermes
	GOOS=windows GOARCH=amd64 go build ${LDFLAGS} -o bin/adc-gen-windows-amd64.exe ./cmd/adc-gen
	GOOS=windows GOARCH=arm64 go build ${LDFLAGS} -o bin/adc-gen-windows-arm64.exe ./cmd/adc-gen

build-all: build-mac build-linux build-windows

clean:
	rm -rf bin/

run:
	go run ./cmd/hermes

run-test:
	TEST_MODE=true go run ./cmd/hermes
