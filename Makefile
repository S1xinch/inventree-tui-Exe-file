PI_HOST ?= root@192.168.4.193

build:
	go build -o inv-tui .

# Raspberry Pi 1 B+ is ARMv6 (ARM11) -- GOARM=6, static binary, stripped.
pi:
	GOOS=linux GOARCH=arm GOARM=6 go build -ldflags="-s -w" -o inv-tui-armv6 .

deploy:
	PI_HOST=$(PI_HOST) ./deploy.sh

test:
	go test ./...

.PHONY: build pi deploy test
