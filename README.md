# Email Client v2

Desktop app (Go + [Fyne](https://fyne.io)) for sending personalized HTML emails through the Gmail API.

## Features

- **Campaign tab** – choose the recipients CSV, the HTML template, the sender and the subject.
  - *Load & check* (dry run): validates every row (bad addresses, duplicates, missing templates, empty placeholders) without sending.
  - *Preview selected*: opens the personalized email in your browser.
  - *Send test to me*: sends one personalized email to the From address, subject prefixed with `[TEST]`.
  - *Send all*: progress bar, per-row status, Stop button, configurable delay between emails, automatic retry on Gmail rate limits.
    Results are written to `send-log-YYYYMMDD-HHMMSS.csv` next to the recipients file.
- **Template editor tab** – HTML source editor with snippet buttons (heading, paragraph, bold, link, button, image…),
  placeholder insertion, browser preview and Ctrl+S.
- **Account & settings tab** – choose `credentials.json`, log in with Google in the browser, log out, CSV encoding/delimiter.
- Last-used values are remembered (`%AppData%\email-client-v2\settings.json`).

## Recipients CSV

```
Email;Template;Greeting
mari@example.com;welcome;Mari
jyri@example.com;;Jüri
```

- The header row is optional. Without one, the columns are `email;template;name`.
- With a header, columns are matched by name (`Email`, `Template`, `Name`/`Greeting`/`Nimi`) and **every column becomes a placeholder**,
  e.g. a `City` column can be used as `{{city}}`.
- The Template column is optional. When it's empty (or the checkbox is off) the template chosen in the GUI is used.
  Names are looked up in the chosen template's folder (`welcome` → `welcome.html`).
- Encoding is detected automatically (UTF-8, otherwise Windows-1257); `;`, `,` and tab delimiters are detected too.

## Templates

Plain HTML files. Placeholders like `{{name}}` are replaced per recipient (case-insensitive, HTML-escaped) in the body and the subject.

## Gmail setup

1. In Google Cloud Console enable the **Gmail API**.
2. Create an OAuth client of type **Desktop app** and download it as `credentials.json`.
3. In the app: *Account & settings → Log in with Google*. The token is saved as `token.json` next to `credentials.json`.

`credentials.json`, `token.json` and send logs are git-ignored.

## Build

Requires Go and a C compiler (MinGW-w64 on Windows) for Fyne.

```
go build -ldflags "-H=windowsgui" -o email-client.exe .
go test ./...
```
