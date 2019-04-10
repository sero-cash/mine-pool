# This Makefile is meant to be used by people that do not usually work
# with Go source code. If you know what GOPATH is then you probably
# don't need to bother with make.

.PHONY: all test clean



GOBIN = $(shell pwd)/build/bin
root=$(shell pwd)
GO ?= latest

darwin:
	build/env.sh darwin-amd64 go install

linux-v3:
	build/env.sh linux-v3 go install

linux-v4:
	build/env.sh linux-v4  go install

test:darwin
	build/env.sh go test -v ./...

clean:
	rm -fr build/_workspace/pkg/ $(GOBIN)/*



devtools:
	env GOBIN= go get -u github.com/karalabe/xgo

pool-linux: pool-linux-amd64-v3 pool-linux-amd64-v4
	@echo "Linux cross compilation done:"
	@ls -ld $(GOBIN)/mine-pool-linux-*

pool-linux-amd64-v3:
	build/env.sh linux-v3 go run build/pci.go  xgo -- --go=$(GO) --out=mine-pool-v3 --targets=linux/amd64 -v ./
	@echo "Linux centos amd64 cross compilation done:"
	@ls -ld $(GOBIN)/mine-pool-v3-linux-* | grep amd64

gero-linux-amd64-v4:
	build/env.sh linux-v4 go run build/pci.go xgo -- --go=$(GO) --out=mine-pool-v4 --targets=linux/amd64 -v ./
	@echo "Linux  ubuntu amd64 cross compilation done:"
	@ls -ld $(GOBIN)/mine-pool-v4-linux-* | grep amd64

pool-darwin-amd64:
	build/env.sh darwin-amd64 go run build/pci.go xgo -- --go=$(GO) --targets=darwin/amd64 -v ./
	@echo "Darwin amd64 cross compilation done:"
	@ls -ld $(GOBIN)/mine-pool-darwin-* | grep amd64


pool-windows-amd64:
	build/env.sh windows-amd64 go run build/pci.go xgo -- --go=$(GO)  --targets=windows/amd64 -v ./
	@echo "Windows amd64 cross compilation done:"
	@ls -ld $(GOBIN)/mine-pool-windows-* | grep amd64
