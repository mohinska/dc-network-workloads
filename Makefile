.PHONY: build test testbed-image testbed-up testbed-down

build:
	GOOS=linux go build -o bin/netbench ./cmd/netbench

test:
	go vet ./... && go test ./...

# Testbed: containerlab needs root (or the clab_admins group, after re-login).
testbed-image:
	docker build -t netbench-host testbed/images/host

testbed-up: testbed-image
	[ -f testbed/.ssh/id_ed25519 ] || ssh-keygen -q -t ed25519 -N "" -f testbed/.ssh/id_ed25519
	containerlab deploy -t testbed/topology.clab.yml --reconfigure

testbed-down:
	containerlab destroy -t testbed/topology.clab.yml --cleanup
