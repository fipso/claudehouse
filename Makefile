GHOSTTY_COMMIT := bebca84668947bfc92b9a30ed58712e1c34eee1d
GHOSTTY_DIR    := vendor/ghostty
GHOSTTY_LIB    := $(GHOSTTY_DIR)/zig-out/lib/libghostty-vt.so
GHOSTTY_INC    := $(GHOSTTY_DIR)/zig-out/include
FONT_DIR       := fonts
FONT_FILE      := $(FONT_DIR)/JetBrainsMono-Regular.ttf
FONT_URL       := https://github.com/JetBrains/JetBrainsMono/raw/master/fonts/ttf/JetBrainsMono-Regular.ttf

GO_SRCS := $(shell find . -name '*.go' -not -path './vendor/*')

.PHONY: all run clean

all: claudehouse

# Clone ghostty at the specific commit (shallow)
$(GHOSTTY_DIR)/.git:
	@mkdir -p vendor
	git clone --depth 1 https://github.com/ghostty-org/ghostty.git $(GHOSTTY_DIR)
	cd $(GHOSTTY_DIR) && git fetch --depth 1 origin $(GHOSTTY_COMMIT) && git checkout $(GHOSTTY_COMMIT)

# Build libghostty-vt shared library
$(GHOSTTY_LIB): $(GHOSTTY_DIR)/.git
	cd $(GHOSTTY_DIR) && zig build -Doptimize=ReleaseFast -Dapp-runtime=none -Demit-lib-vt

# Download JetBrains Mono font
$(FONT_FILE):
	@mkdir -p $(FONT_DIR)
	curl -sL -o $(FONT_FILE) $(FONT_URL)

# Build the Go binary (run inside nix-shell for system headers, or set CGO_CFLAGS)
claudehouse: $(GHOSTTY_LIB) $(FONT_FILE) $(GO_SRCS)
	CGO_ENABLED=1 GOFLAGS=-mod=mod go build -buildvcs=false -o claudehouse .

# Build and run
run: claudehouse
	LD_LIBRARY_PATH=$(GHOSTTY_DIR)/zig-out/lib ./claudehouse

clean:
	rm -f claudehouse
