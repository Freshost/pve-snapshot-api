BINARY=pve-snapshot-api

.PHONY: build test vet clean deb release-check

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

release-check:
	python3 scripts/release-metadata.py
	python3 -m unittest discover -s scripts -p 'test_*.py'
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.11
