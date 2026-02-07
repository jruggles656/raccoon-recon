.PHONY: build run test clean

build:
	go build -o reconsuite .

run: build
	./reconsuite

test:
	go test ./... -v

clean:
	rm -f reconsuite reconsuite.db
	rm -rf reports/*.md reports/*.pdf
