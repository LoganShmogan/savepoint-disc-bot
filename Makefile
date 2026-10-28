.PHONY: test cli deploy secrets

# The server the bot runs on, reached over SSH (e.g. its Tailscale name).
DEPLOY_HOST ?= loganserver
DEPLOY_DIR  ?= project-hub-bot

test:
	go vet ./... && go test ./...

# Build docsync for teammates on every OS into dist/.
cli:
	GOOS=linux   GOARCH=amd64 go build -trimpath -o dist/docsync-linux-amd64     ./cmd/docsync
	GOOS=windows GOARCH=amd64 go build -trimpath -o dist/docsync-windows-amd64.exe ./cmd/docsync
	GOOS=darwin  GOARCH=arm64 go build -trimpath -o dist/docsync-macos-arm64     ./cmd/docsync

# Copy the code to the server and rebuild. Secrets are never sent by this
# target; use `make secrets` for those.
deploy:
	rsync -az --delete --exclude .git --exclude data --exclude dist --exclude .env --exclude projects.yaml ./ $(DEPLOY_HOST):$(DEPLOY_DIR)/
	ssh $(DEPLOY_HOST) 'chmod 700 $(DEPLOY_DIR) && cd $(DEPLOY_DIR) && docker compose up -d --build'

# Copy .env and projects.yaml (both git-ignored) to the server inside a
# private folder, and restart so the bot picks them up.
secrets:
	ssh $(DEPLOY_HOST) 'mkdir -p $(DEPLOY_DIR) && chmod 700 $(DEPLOY_DIR)'
	rsync -z --chmod=F600 .env $(DEPLOY_HOST):$(DEPLOY_DIR)/
	rsync -z --chmod=F644 projects.yaml $(DEPLOY_HOST):$(DEPLOY_DIR)/ # the container's user must read it
	ssh $(DEPLOY_HOST) 'cd $(DEPLOY_DIR) && docker compose up -d --force-recreate'
