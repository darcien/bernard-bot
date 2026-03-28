set dotenv-load := true

# List all available targets if just is executed with no arguments
default:
    @just --list

# Format, vet, and build code
check:
    go fmt ./... && go vet ./... && go build ./...

# Run tests
test:
    go test ./...

# Run server locally
run:
    go run .

# Register all commands to Discord Application
register:
    go run ./cmd/manage register

# List registered commands in Discord Application
list:
    go run ./cmd/manage list

# Delete a registered command from Discord Application
delete commandId:
    go run ./cmd/manage delete {{ commandId }}

# Build binary
build:
    go build -o bernard .

# Build binary for Linux
build-linux:
    GOOS=linux GOARCH=amd64 go build -o bernard-linux .
