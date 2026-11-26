.PHONY: build test testbed-image testbed-up testbed-down testbed-push

build:
	CGO_ENABLED=0 GOOS=linux go build -o bin/netbench ./cmd/netbench

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

# Copy the binary into the running hosts (netbench run starts `netbench agent` there).
testbed-push: build
	for n in 1 2 3 4 5 6; do docker cp bin/netbench clab-netbench-h$$n:/usr/local/bin/netbench; done
