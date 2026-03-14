BINARY=pve-snapshot-api
VERSION=$(shell head -1 debian/changelog | grep -oP '\(.*?\)' | tr -d '()')

.PHONY: build test vet clean deb

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) ./cmd/pve-snapshot-api/

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)
	rm -rf debian/pve-snapshot-api/
	rm -f ../pve-snapshot-api_*.deb ../pve-snapshot-api_*.changes ../pve-snapshot-api_*.buildinfo

deb:
	dpkg-buildpackage -us -uc -b
