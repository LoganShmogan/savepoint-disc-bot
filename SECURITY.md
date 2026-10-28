# Security Policy

Save Point Project Hub is maintained by **Save Point Limited**.

## Reporting a problem

If you find a security issue in this bot, please email **[support@savepoint.co.nz](mailto:support@savepoint.co.nz)** with the subject line "Security: Project Hub". **Do not** open a public GitHub issue for it.

Please include:

- what you found and how to reproduce it
- what an attacker could do with it
- your contact details, if you'd like us to follow up

We will acknowledge your report within 2 working days and keep you updated until it is fixed. Please give us a reasonable time to fix the issue before sharing it publicly.

## If a token leaks

| Secret | What to do |
|---|---|
| Discord bot token | Discord Developer Portal → Bot → **Reset Token**, then update `.env` and run `make secrets` |
| GitHub token | Revoke it at github.com/settings/personal-access-tokens, create a new read-only one, then update `.env` and run `make secrets` |
| A teammate's docsync token | They run `/token` in Discord, which replaces the old token straight away |

## How the bot is protected

How the bot stores data, what it can access and what it keeps private is covered in [docs/07-data-and-security.md](docs/07-data-and-security.md).
