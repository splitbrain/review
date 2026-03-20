.PHONY: build test clean

build:
	go build -o review .

test:
	go test ./...

clean:
	rm -f review
