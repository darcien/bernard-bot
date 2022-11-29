set dotenv-load

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

# Register all commands to Discord application
register:
  deno run --allow-read=./ --allow-env --allow-net=discord.com ./register.ts

# Get all registered commands in Discord application
get-registered:
  deno run --allow-read=./ --allow-env --allow-net=discord.com ./registered.ts

# Delete a registered command from Discord
delete-registered commandId:
  deno run --allow-read=./ --allow-env --allow-net=discord.com ./delete.ts --commandId="{{commandId}}"

# Deploy the slash commands request handler to Deno Deploy
deploy:
  # Make sure $DISCORD_PUBLIC_KEY is set in the deployed env
  deployctl deploy --project="bernard-the-4th" ./mod.ts --token=$DENO_DEPLOY_TOKEN
