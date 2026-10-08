PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin

.PHONY: build install test clean

build:
	go build -o review .

# Build straight into BINDIR under a temp name, then rename over the final
# path. rename(2) replaces the path atomically even while a running server is
# executing the old inode, which avoids the "Text file busy" that a plain
# copy/overwrite hits. Running instances keep the old binary until restarted;
# the next launch picks up the new one.
install:
	mkdir -p $(BINDIR)
	go build -o $(BINDIR)/.review.tmp .
	mv -f $(BINDIR)/.review.tmp $(BINDIR)/review

test:
	go test ./...

clean:
	rm -f review
