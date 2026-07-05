# baasparse — build & deploy
#
#   make build     build ./baasparse
#   make test      run the test suite
#   make deploy    build and install to ~/.baasparse (binary + schema.sql)
#   make run       run from source (go run)
#   make clean     remove local build output

BINARY  := baasparse
PKG     := ./cmd/baasparse
PREFIX  := $(HOME)/.baasparse
LDFLAGS := -s -w

.PHONY: all build test vet run deploy clean

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

run:
	go run $(PKG)

# Build and install the binary + schema into ~/.baasparse. The binary finds
# schema.sql next to itself, so setupdb/createadmin/run work from anywhere.
deploy: build
	@mkdir -p $(PREFIX)
	install -m 0755 $(BINARY) $(PREFIX)/$(BINARY)
	install -m 0644 .db/schema.sql $(PREFIX)/schema.sql
	@echo
	@echo "Installed to $(PREFIX):"
	@echo "  $(PREFIX)/$(BINARY)"
	@echo "  $(PREFIX)/schema.sql"
	@echo
	@echo "Next:"
	@echo "  export DATABASE_URL='postgres://postgres:admin@localhost:5432/baasparseDB?sslmode=disable'"
	@echo "  $(PREFIX)/$(BINARY) setupdb        # apply the schema"
	@echo "  $(PREFIX)/$(BINARY) createadmin    # create an administrator"
	@echo "  $(PREFIX)/$(BINARY) --listen :8080 # run the engine"
	@echo
	@echo "Tip: add ~/.baasparse to your PATH to run 'baasparse' directly."

clean:
	rm -f $(BINARY)
