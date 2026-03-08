# MAINTs triage tool (triage)

A CLI tool that uses `cursor-agent` to triage Jira maintenance issues against a
checklist.

It fetches issue data from Jira, prepares a temporary workspace with the issue
content and a checklist, and then instructs the AI agent to verify
completeness.

## Installation

### Homebrew (macOS)

```bash
brew tap codcod/taps
brew install maints-triage
```

### From source

#### Prerequisites

- Go 1.25+
- `cursor-agent` CLI installed and available in your `$PATH`
- A Jira account (Cloud or Data Center)
- A Cursor API key (for the agent)

#### Install

```bash
git clone https://github.com/codcod/maints-triage.git
cd maints-triage
go install .
```

Verify installation:

```bash
triage --version
triage --help
```

### Build from source

If you prefer to build the binary without installing it to your `$GOPATH/bin`:

```bash
git clone https://github.com/codcod/maints-triage.git
cd maints-triage
go build -o triage .
```

This creates a `triage` binary in the current directory. You can then run it
with `./triage`.

You can also use `just`, which automatically stamps the binary with the current
git tag:

```bash
just build
```

The version embedded in the binary reflects the latest git tag (e.g. `v0.2.0`).
Binaries built outside of a tagged commit show the tag plus a commit suffix
(e.g. `v0.2.0-3-gabcdef`). Builds with no git context report `dev`.

#### Checking the version

```bash
triage --version
```

## Configuration

The tool requires credentials for both Jira and Cursor. You can provide these
via environment variables or a `.env` file in the current directory.

1. Copy the example file:
   ```bash
   cp .env.example .env
   ```
2. Edit `.env` and fill in your details:
   ```bash
   # Jira Configuration
   JIRA_URL=https://your-company.atlassian.net
   JIRA_USERNAME=your-email@company.com
   JIRA_API_TOKEN=your-jira-api-token

   # Cursor Agent API Key
   CURSOR_API_KEY=your-cursor-api-key
   ```

### Configuration directory

`triage` looks for configuration files (checklist, prompt template, field
mappings) in the following order:

1. Explicit command-line flags (where available)
2. `$TRIAGE_HOME/<filename>`
3. `$XDG_CONFIG_HOME/triage/<filename>` (defaulting to `~/.config/triage/<filename>`)
4. `./<filename>` (current directory)

You can customize the behavior by placing the following files in your
configuration directory:
- `checklist.md`: The default checklist to evaluate issues against.
- `triage-prompt.md`: The prompt template used to instruct the AI agent.
- `fields-mapping.json`: Mappings for custom Jira fields.

### Custom field mappings

You can extract additional fields from Jira issues by creating a
`fields-mapping.json` file in your triage configuration directory.

The file should contain a JSON array of mappings, where each mapping has a
`field` (display name) and a `path` (dot-notation path to the value in the Jira
JSON response).

Example `fields-mapping.json`:

```json
[
  {
    "field": "Customer Impact",
    "path": "fields.customfield_12345.value"
  },
  {
    "field": "Root Cause",
    "path": "fields.customfield_67890"
  }
]
```

## Usage

### Basic triage

Triage a single issue:

```bash
triage MAINT-123
```

Triage multiple issues in one go:

```bash
triage MAINT-123 MAINT-456 MAINT-789
```

### Custom checklist

To use an explicit checklist (overriding default locations):

```bash
triage --checklist ./my-custom-checklist.md MAINT-123
```

### Custom prompt

To use a custom prompt template (overriding default locations):

```bash
triage --prompt ./my-custom-prompt.md MAINT-123
```

### AI model selection

Specify which AI model the agent should use (e.g., `sonnet-4`, `gpt-4o`):

```bash
triage --model sonnet-4 MAINT-123
```

### Output format

Output the triage report as JSON (useful for piping to other tools):

```bash
triage --output json MAINT-123 | jq .
```

### Concurrent batch triage

By default, up to **5** issues are triaged in parallel. Each triage involves a
Jira HTTP round-trip and a `cursor-agent` invocation, so concurrency cuts wall
time roughly proportionally. Use `--concurrency` / `-j` to tune the limit:

```bash
# triage 10 issues with 3 parallel workers
triage --concurrency 3 MAINT-100 MAINT-101 MAINT-102 MAINT-103 MAINT-104 \
                       MAINT-105 MAINT-106 MAINT-107 MAINT-108 MAINT-109

# force sequential execution
triage --concurrency 1 MAINT-100 MAINT-101
```

Results are always printed in the order the issue keys were supplied, regardless
of completion order.

## How it works

1. **Fetch**: Connects to the Jira REST API to retrieve the issue summary,
   description, comments, and metadata (status, priority, versions, components,
   etc.).
2. **Prepare**: Creates a `triaged-maints/KEY/` directory in the current
   working directory and writes two files into it:
   - `issue-KEY.md`: A Markdown-formatted representation of the Jira issue.
   - `checklist.md`: The triage checklist.
3. **Analyze**: Invokes `cursor-agent` in headless mode with
   `triaged-maints/KEY/` as its workspace. The agent is instructed via the
   prompt template (e.g., `triage-prompt.md`) to read both files and evaluate
   the issue against each checklist item.
4. **Report**: The agent's response is printed to stdout and also saved as
   `triaged-maints/KEY/report-KEY.md`.

After a run the directory is kept so you can review or commit the artefacts:

```
triaged-maints/
└── MAINT-123/
    ├── issue-MAINT-123.md
    ├── checklist.md
    └── report-MAINT-123.md
```

## License

MIT
