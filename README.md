# Pelokalan

Web-based gettext (PO/POT) translation editor for teams, built with Go and htmx.
The interface is in Bahasa Indonesia (default) and English.

## Features

- Passwordless sign-in: registration and login with one-time codes sent by email (Mailtrap)
- Optional email whitelist (`EMAIL_WHITELIST`); without it registration is open to everyone
- Projects are public: anyone can browse projects, their glossary and members without signing in
- Projects with members and roles (owner, manager, translator) and email invitations
- Personal workspace ("Berkas Anda"): each member uploads and translates their own
  POT/PO files, which other members cannot see
- Upload a POT template and PO files; start new languages from the template
- Update translations from a changed template (new strings added, removed ones kept as obsolete)
- Desktop-style translation editor: entry list, source/translation panes, plural forms,
  "needs work" (fuzzy) flag, translator comments, notes, references, keyboard shortcuts
- Validation of format placeholders and newlines
- AI pre-translation (OpenAI-compatible API) that puts the glossary first: exact glossary
  matches are filled without calling the model, glossary terms are mandatory hints for the
  model, and results that ignore the glossary or break placeholders are marked for review.
  Cost estimate before running, actual cost tracking and an optional monthly budget
- A page per glossary string (`/projects/{id}/glossary/<sha256>`) with all translations,
  their history across versions and a discussion; comments with @mentions (email
  notifications) and a Discussions tab per project
- One "Praterjemah" (pre-translate) action that fills empty entries from translation memory,
  then the glossary, then (optionally) the AI model; machine translations carry an orange bot
  marker and their own filter until a person saves them
- Simple version control per file: commit with a message, automatic safety versions before
  risky operations, a history timeline with progress chart and per-version diff, restore any
  version (including the original), discard uncommitted changes
- Translation memory suggestions and pre-translation from your other projects
- Concurrent editing protection (a stale save is rejected instead of overwriting)
- Download PO files or compiled MO files, or edit the PO file as raw text in the browser
- Translator identity (name, email, default language team) synced into PO headers on
  download: `Last-Translator`, `Language-Team` and the contributor lines; file properties
  editable per file
- Project glossary per release version (e.g. `gnome-51`): upload existing translations
  (`.po`, `.tar.gz`, `.tgz`, `.tar`, `.zip`) or set a download URL that is fetched daily,
  then explore how each term is translated, how often, in which modules and versions,
  and where translations are inconsistent

## Dependencies

- Go 1.24+
- Docker (for PostgreSQL)
- `golang-migrate` (optional; the server runs migrations on start) - https://github.com/golang-migrate/migrate

## Spin up

```
make prep
make run
```

Then open http://localhost:8080 and register. Without `MAILTRAP_API_TOKEN` the one-time codes are printed
to the server log; `EMAIL_WHITELIST` in `.env` limits who may sign in (empty = anyone).

`make prep` (`scripts/prep.sh`) is safe to run again at any time. It:

- creates `.env` from `.env.example` when there is none (an existing `.env` is never overwritten);
- starts PostgreSQL in Docker (container `pelokalan-postgres`), creating it the first time with a named
  volume (`pelokalan-pgdata`) so the data survives restarts;
- picks the next free port when 5432 is taken by something else, and writes it to `.env` as `DB_PORT`
  (an existing container moves to the new port and keeps its data);
- waits for the database and downloads the Go modules.

`make stop-postgres` stops the database (its data is kept); `make prep` starts it again.

## Configuration

See `.env.example`. The important settings:

| Variable | Description |
| --- | --- |
| `EMAIL_WHITELIST` | Comma separated emails allowed to register and sign in. `@domain.com` allows a whole domain. Removing an address revokes access immediately. Empty = open registration. |
| `MAILTRAP_API_TOKEN` | Mailtrap API token. Empty = dev mode (emails logged). |
| `MAILTRAP_INBOX_ID` | Set to deliver to a Mailtrap sandbox inbox instead of sending. |
| `MAIL_FROM_EMAIL` | Sender address (must be on a verified Mailtrap domain in production). |
| `DEFAULT_LANG` | `id` (default) or `en`. |
| `GLOSSARY_FETCH_INTERVAL_HOURS` | How often each glossary download URL is fetched (default 24). |
| `GLOSSARY_ALLOW_PRIVATE_HOSTS` | Allow download URLs on local/private networks (default false). |
| `AI_SECRET_KEY` | Encrypts stored AI API keys (defaults to `JWT_SECRET`). |
| `AI_ALLOW_PRIVATE_HOSTS` | Allow AI API URLs on local/private networks (default false). |
| `JWT_SECRET` | Signs sessions and one-time code hashes. Change in production. |

## Keyboard shortcuts (editor)

| Keys | Action |
| --- | --- |
| Ctrl+Enter | Save and go to the next entry |
| Ctrl+↑ / Ctrl+↓ | Previous / next entry (saves changes) |
| Ctrl+U | Toggle "needs work" |
| Ctrl+B | Copy source text to translation |
| Ctrl+1…5 | Use a translation suggestion |
| Ctrl+F | Search |

## Public API

Read-only JSON, no key or sign-in needed, callable from any site (CORS `*`, cached for 60 s).

`GET /api/projects/{slug}/members` lists a project's members (owners first, then managers and translators)
with their sign-in email addresses.

```json
{
  "project": { "slug": "gnome-id", "name": "GNOME ID", "url": "https://pelokalan.blankon.id/projects/gnome-id" },
  "total": 1,
  "members": [
    { "username": "admin", "name": "Admin", "email": "admin@example.org", "role": "owner", "joined_at": "2026-11-08T12:09:18Z" }
  ]
}
```

An unknown project gives `404` with `{"error": "project not found"}`.

## License

Pelokalan is open source software under the [MIT License](LICENSE). Its source code is available at
[github.com/BlankOn/pelokalan](https://github.com/BlankOn/pelokalan). Bug reports, suggestions and code
contributions are very welcome.
