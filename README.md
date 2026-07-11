# chartbump

CLI tool that inspects a directory of Helm charts and reports which dependencies have newer versions available.

pretty much only made for me tbh, and working on Loco. there are likely more pragmatic and safer options for bumping helm charts
through helm plugins or similar.

## Usage

```
chartbump [flags] [charts-dir]

Flags:
  -charts string    path to charts directory (default "./charts")
  -pre              include pre-release versions when finding latest
  -changelog        fetch GitHub release notes for each outdated dependency
  -token string     GitHub token (falls back to GITHUB_TOKEN env, then: gh auth token)
  -verbose          show dependencies that are already up to date
  -h                show this help
```

## Install

```
go install github.com/nikumar1206/chartbump@latest
```

Or clone and run `make install`.

## Dependencies

| Tool | Required | Purpose |
|------|----------|---------|
| `helm` | For OCI repos only | Resolves latest version via `helm show chart` |
| `gh` | Optional | Fallback token source when `-token` and `GITHUB_TOKEN` are not set |
| `bat` | Optional | Pretty pager when `-changelog` is used (falls back to `less`) |
| `less` | Optional | Pager fallback if `bat` is not installed |
