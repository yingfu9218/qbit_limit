APP_NAME := qbit_limit
VERSION  := 2.0.0
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS  := -s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)
DIST_DIR := dist

.PHONY: all build run test clean build-all linux-amd64 linux-arm64 linux-armv7 darwin-amd64 darwin-arm64 windows-amd64

all: build

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(APP_NAME) .

run:
	go run . -port 8091

test:
	go test -v ./...

clean:
	rm -rf $(DIST_DIR) $(APP_NAME)

build-all: linux-amd64 linux-arm64 linux-armv7 darwin-amd64 darwin-arm64 windows-amd64

linux-amd64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-linux-amd64 .

linux-arm64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-linux-arm64 .

linux-armv7:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-linux-armv7 .

darwin-amd64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-darwin-amd64 .

darwin-arm64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-darwin-arm64 .

windows-amd64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-windows-amd64.exe .
