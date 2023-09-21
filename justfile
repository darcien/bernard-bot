set dotenv-load

default-permission := "--allow-read=./ --allow-env --allow-net='0.0.0.0:8000,discord.com'"

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
  # Make sure $DISCORD_PUBLIC_KEY is set in the deployed env
  deployctl deploy --project=$DENO_DEPLOY_PROJECT_NAME ./mod.ts --token=$DENO_DEPLOY_TOKEN

deploy-prod:
  deployctl deploy --project=$DENO_DEPLOY_PROJECT_NAME ./mod.ts --token=$DENO_DEPLOY_TOKEN --prod

test:
  deno test {{default-permission}}

update-snapshot:
  deno test {{default-permission}} --allow-write -- --update
