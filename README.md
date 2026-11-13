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
cp .env.example .env   # set EMAIL_WHITELIST to your address
make run-postgres
make run
```

Then open http://localhost:8080 and register with a whitelisted email. Without
`MAILTRAP_API_TOKEN` the one-time codes are printed to the server log.

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

## License

Pelokalan is open source software under the [MIT License](LICENSE). Its source code is available at
[github.com/BlankOn/pelokalan](https://github.com/BlankOn/pelokalan). Bug reports, suggestions and code
contributions are very welcome.
