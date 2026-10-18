# gonana

Go packages shared by the PlatanosVerdes services, in one module. A program compiles only the
packages it imports, so pulling the module in costs nothing for the ones it does not use.

```bash
go get github.com/PlatanosVerdes/gonana@latest
```

| Package | What it does |
| :--- | :--- |
| [`botstats`](botstats) | Who talks to a bot, how much, and what it turned away, served at `/bot-stats` for [muninn](https://github.com/PlatanosVerdes/muninn) |

## Versions

Plain semver, `v0.MINOR.PATCH`. Every push to `main` that passes the tests is tagged with the next
patch by the **Release** workflow. A change that breaks a caller bumps the minor by hand: tag
`v0.N.0` on that commit before pushing anything else.

Not the date tags the apps use (`v2026.10.18`): Go reads the first number as the major version,
and from v2 on it must also appear in every import path (`.../gonana/v2026/botstats`).

## botstats: the `/bot-stats` contract

A bot that serves this document at a URL muninn can reach shows up on muninn's page. Any language
can serve it; Go bots get it from `botstats.Recorder`.

```json
{
  "platform": "telegram",
  "bot": "TorreFlightsBot",
  "name": "Torre",
  "users": { "total": 12, "active_24h": 3, "active_7d": 8, "new_24h": 1 },
  "people": [
    { "id": "123", "name": "Ana", "username": "ana",
      "first_seen": "2026-10-01T09:00:00Z", "last_seen": "2026-10-18T21:40:00Z", "messages": 40 }
  ],
  "traffic": { "per_minute": [0, 2, 1, "... 60 values, oldest first"], "messages_24h": 120, "rejected_24h": 3 },
  "extra": [ { "label": "Flights followed", "value": "12" } ]
}
```

- `platform` and `bot` name the bot; `name` is how to show it.
- `users` counts people by when they last and first wrote: active means a message in the window.
- `people` is everyone who ever wrote, most recent first.
- `traffic.per_minute` is messages in each of the last 60 minutes; `messages_24h` and
  `rejected_24h` sum the last day. Rejected means turned away by a rate limit or as abuse.
- `extra` is the bot's own figures. muninn shows them as they come and reads nothing into them.

**Serve it on a port of its own that is never published.** `people` holds names, and a public bot's
web port is reachable from the internet.
