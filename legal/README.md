# Legal documents: Save Point Project Hub

- [Terms of Service](terms-of-service.md)
- [Privacy Policy](privacy-policy.md)

These are the source text for the public page at
**<https://savepoint.co.nz/legal/discord-bot>**, which the Discord Developer
Portal links to (`#terms` and `#privacy`). The page lives in the Save Point
website repo at `app/legal/discord-bot/page.tsx`.

When you change one, change the other in the same week, and update the
"Last updated" date on both.

**These were written without legal review.** Before relying on them, for
example before opening the bot to clients, have a New Zealand lawyer review
them.

Keep them true to the code. In particular:

- The Privacy Policy lists exactly what the bot collects. If a change makes the bot
  collect something new (e.g. message content), update section 2.1 first.
- The policy promises size-limited logs. Keep the log rotation in
  `docker-compose.yml`.
- The policy says the bot's records stay on Save Point's own server in New
  Zealand. Update section 2.3 if hosting moves.

General Save Point policies for the website and client work are at
<https://savepoint.co.nz/legal>.
