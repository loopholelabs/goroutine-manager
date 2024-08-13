# Test
test:
	go test -timeout 3600s -parallel $(shell nproc) -race ./...

# Benchmark
benchmark:
	go test -timeout 3600s -bench=./... ./...

# Dependencies
depend:
	true
