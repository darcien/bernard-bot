set dotenv-load

default-permission := "--allow-read=./ --allow-env --allow-net --allow-import=cdn.skypack.dev,deno.land,jsr.io,esm.sh"

# List all available targets if just is executed with no arguments
default:
  @just --list

# Install project dependencies
[macos]
install:
  # Install deno runtime - https://deno.land/manual@v1.28.2/getting_started/installation#download-and-install
  curl -fsSL https://deno.land/x/install/install.sh | sh
  # Install deployctl for deployment - https://github.com/denoland/deployctl#install
  deno install --allow-read --allow-write --allow-env --allow-net --allow-run --no-check -r -f https://deno.land/x/deploy/deployctl.ts

dev:
  deno run {{default-permission}} --watch ./mod.ts

# Register all commands to Discord application
register:
  deno run {{default-permission}} ./register.ts

# Get all registered commands in Discord application
get-registered:
  deno run {{default-permission}} ./registered.ts

# Delete a registered command from Discord
delete-registered commandId:
  deno run {{default-permission}} ./delete.ts --commandId="{{commandId}}"

# Deploy the slash commands request handler to Deno Deploy
deploy:
  deployctl deploy --token=$DENO_DEPLOY_TOKEN

deploy-prod:
  deployctl deploy --token=$DENO_DEPLOY_TOKEN --prod

test:
  deno test {{default-permission}}

update-snapshot:
  deno test {{default-permission}} --allow-write -- --update

# Format, vet, and build Go code (run before committing)
go-check:
  cd v2 && go fmt ./... && go vet ./... && go build ./...

# Run Go tests
go-test:
  cd v2 && go test ./...

# Run Go server locally (uses .env)
go-run:
  cd v2 && go run .

# Run Go server with .env.test credentials (for testing against a separate Discord app)
go-run-test:
  #!/usr/bin/env bash
  set -a && source "{{justfile_directory()}}/.env.test" && set +a
  cd v2 && go run .

# Register all commands to the production Discord application (uses .env)
go-register:
  cd v2 && go run ./cmd/manage register

# Register all commands to the test Discord application (uses .env.test at repo root)
go-register-test:
  #!/usr/bin/env bash
  set -a && source "{{justfile_directory()}}/.env.test" && set +a
  cd v2 && go run ./cmd/manage register

# List registered commands in the production Discord application (uses .env)
go-list:
  cd v2 && go run ./cmd/manage list

# List registered commands in the test Discord application (uses .env.test at repo root)
go-list-test:
  #!/usr/bin/env bash
  set -a && source "{{justfile_directory()}}/.env.test" && set +a
  cd v2 && go run ./cmd/manage list

# Delete a registered command from the production Discord application (uses .env)
go-delete commandId:
  cd v2 && go run ./cmd/manage delete {{commandId}}

# Delete a registered command from the test Discord application (uses .env.test at repo root)
go-delete-test commandId:
  #!/usr/bin/env bash
  set -a && source "{{justfile_directory()}}/.env.test" && set +a
  cd v2 && go run ./cmd/manage delete {{commandId}}

# Build binary for local testing (macOS arm64)
go-build:
  cd v2 && go build -o ../bernard .

# Cross-compile binary for Linux deployment
go-build-linux:
  cd v2 && GOOS=linux GOARCH=amd64 go build -o ../bernard-linux .
