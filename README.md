# llm-guard

[![CI](https://github.com/densub/llm-guard/actions/workflows/ci.yml/badge.svg)](https://github.com/densub/llm-guard/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

A local proxy that sits between your AI agents and remote LLM APIs (OpenAI,
Anthropic, or any other HTTP API). It scans outgoing requests for secrets,
API keys, and other sensitive data, replaces them with placeholder tokens
before they leave your machine, and restores the original values in the
response so your agent keeps working normally.

## How it works

```
Agent --> llm-guard (redacts secrets) --> real LLM provider
Agent <-- llm-guard (restores secrets) <-- real LLM provider
```

llm-guard is a transparent reverse proxy: it forwards whatever method, path,
query string, and headers your agent sends, but rewrites the JSON request
body before it leaves your machine, and rewrites the response body before it
reaches your agent.

1. The request body is parsed (if JSON) and every string value is scanned
   against a set of regex detectors (AWS/GCP/GitHub/GitLab/Slack/Stripe/
   OpenAI/Anthropic keys, private key blocks, JWTs, generic
   `key=value`/`key: value` secret assignments, emails, SSNs, credit
   card numbers, phone numbers, and IBANs — see
   `internal/redact/detectors/regex.go`), plus an optional local LLM pass
   for free-form sensitive content (see below).
2. Each match is replaced with a placeholder token like `⟦RG:a1b2c3d4⟧` and
   recorded in an in-memory map (`hash -> original value`). The same value
   always maps to the same placeholder.
3. The redacted body is forwarded to `upstream`.
4. The response (including `text/event-stream` / chunked streaming
   responses) is scanned for placeholder tokens, which are swapped back for
   their original values before being returned to the agent.

Redaction mappings live in memory only for the life of the `llmguard`
process and are never written to disk. Logs record which categories were
redacted and how many, never the values themselves.

Add your own patterns (e.g. internal project codenames, customer IDs) via
`detectors.regex.custom_patterns` in the config file.

## Install

### One command (recommended)

```sh
curl -fsSL https://raw.githubusercontent.com/densub/llm-guard/main/scripts/install.sh | bash
```

This will:

1. Clone the repo and build the `llmguard` binary (requires [Go](https://go.dev/dl/))
2. Install it to `~/.local/bin/llmguard`
3. Ask which agents you use (OpenAI/Codex, Claude Code, Cursor)
4. Write config, add `BASE_URL` exports to your shell profile, and start the proxy in the background
5. Print a ready summary with the env vars to use

Re-run anytime to reconfigure. From a git checkout you can also run:

```sh
./scripts/install.sh
```

For **Claude Code**, the installer also writes `ANTHROPIC_BASE_URL` to
`~/.claude/settings.json` (the recommended persistent config). Exit any
running `claude` session and start a new one after installing.

### Manual install

Requires Go (see `go.mod` for the version used to build).

```sh
git clone https://github.com/densub/llm-guard.git && cd llm-guard
go build -o llmguard ./cmd/llmguard
```

This produces a single self-contained `llmguard` binary with no runtime
dependencies (no cgo, no C toolchain needed). Put it somewhere on your
`PATH`, e.g.:

```sh
sudo mv llmguard /usr/local/bin/
```

It cross-compiles to any platform/architecture Go supports — e.g. to build
for Linux arm64 from macOS:

```sh
GOOS=linux GOARCH=arm64 go build -o llmguard-linux-arm64 ./cmd/llmguard
```

## Quick start

### 1. Configure

```sh
llmguard init
```

This prompts you for which LLM API to proxy to (OpenAI, Anthropic, or a
custom URL), then writes `~/.config/llmguard/config.yaml` (see
`configs/config.example.yaml` for the full set of options).

### 2. Start the proxy

```sh
llmguard start            # foreground
llmguard start --detach   # background; logs to ~/.local/share/llmguard/daemon.log
llmguard restart          # stop (if running) and start in background
llmguard status
llmguard stop
```

### 3. Sanity check (optional)

```sh
llmguard test
```

Runs a built-in sample payload containing fake secrets through the redactor
(no network calls) and prints what gets redacted and restored.

## Hook it up to your agent

Once the proxy is running on `127.0.0.1:8317` (the default — change via
`listen` in the config), point your agent's API base URL at it instead of
the real provider. Your existing API key env vars (`OPENAI_API_KEY`,
`ANTHROPIC_API_KEY`, etc.) keep working as-is — llm-guard only rewrites the
request/response *bodies* and passes headers straight through, so
authentication with the real provider is unaffected.

### Claude Code

```sh
export ANTHROPIC_BASE_URL=http://127.0.0.1:8317
```

Run `claude` as normal in the same shell/session. Every request from Claude
Code now flows through llm-guard before reaching `api.anthropic.com`.

### Codex CLI / other OpenAI-API agents

```sh
export OPENAI_BASE_URL=http://127.0.0.1:8317/v1
```

### Any agent built on the OpenAI or Anthropic SDKs

Most SDKs accept a `base_url` (or `baseURL`) constructor option as an
alternative to the env vars above, e.g.:

```python
client = OpenAI(base_url="http://127.0.0.1:8317/v1", api_key=os.environ["OPENAI_API_KEY"])
client = anthropic.Anthropic(base_url="http://127.0.0.1:8317", api_key=os.environ["ANTHROPIC_API_KEY"])
```

```ts
const client = new OpenAI({ baseURL: "http://127.0.0.1:8317/v1", apiKey: process.env.OPENAI_API_KEY });
const client = new Anthropic({ baseURL: "http://127.0.0.1:8317", apiKey: process.env.ANTHROPIC_API_KEY });
```

### Anything else (generic HTTP)

If your tool lets you set a custom "API base URL" / "endpoint" setting
(custom integrations, IDE plugins, internal scripts), point it at
`http://127.0.0.1:8317` (or `/v1` if it's an OpenAI-shaped client) — llm-guard
forwards everything else (path, query string, headers, streaming) unchanged.

To confirm traffic is actually flowing through the proxy, tail the redaction
log while your agent runs:

```sh
tail -f ~/.local/share/llmguard/redactions.log
```

## Optional: local LLM fallback detector

Regex catches structured secrets (keys, tokens, emails, SSNs, credit
cards, phone numbers, IBANs) but misses free-form sensitive content —
names, internal project codenames, customer IDs, addresses. llm-guard can
optionally run a small local LLM
(`Qwen2.5-0.5B-Instruct`, ~0.5B params, ~490MB as a Q4 GGUF) as an additional
detection pass over each string field.

This is implemented by spawning [`llama-server`](https://github.com/ggml-org/llama.cpp)
— llama.cpp's prebuilt HTTP server binary — as a local subprocess and talking
to it over `127.0.0.1`. The core `llmguard` binary itself has no C
dependencies and cross-compiles to any platform Go supports; the LLM fallback
is available wherever ggml-org/llama.cpp publishes a prebuilt CPU binary
(macOS arm64/x64, Linux x64/arm64/s390x, Windows x64/arm64). On any other
platform, or if the binary/model haven't been downloaded, llm-guard logs a
warning and runs with regex-only detection — the proxy never fails to start
or hang because of this.

### Set it up

```sh
llmguard models pull     # downloads llama-server + the GGUF model (~490MB)
llmguard models status    # check what's installed and whether enabled
```

`models pull` updates `server_path`/`model_path` in your config and asks
whether to set `detectors.llm_fallback.enabled: true`. Restart llm-guard
afterwards (`llmguard restart`).

### What it costs

- ~500MB of disk for the model, plus a few MB for `llama-server`.
- Each request gets one bounded "budget" (`overall_timeout_ms`, default
  4000ms) for all LLM detector calls combined — if the budget runs out or
  the local server is slow/unreachable, llm-guard just skips the LLM pass
  for the rest of that request and returns the regex-only result.
- String fields outside `min_text_len`/`max_text_len` (default 8–2000 bytes)
  are skipped entirely.
- Matches are flagged under the `llm_sensitive` category. Every candidate the
  model returns is verified to be a verbatim substring of the original text
  before being redacted, to guard against hallucinated spans.

See `configs/config.example.yaml` for all `llm_fallback` options.

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for
development setup, testing, and pull request guidelines.

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md).

## Security

If you discover a security vulnerability, please follow our
[security policy](SECURITY.md) and report it privately — do not open a public
issue.

## License

llm-guard is licensed under the [Apache License 2.0](LICENSE).
