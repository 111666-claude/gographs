# gographs

[![gographs](https://gographs.io/badge.svg)](https://gographs.io/repo/github.com/siggy/gographs?cluster=true)
[![Go Report Card](https://goreportcard.com/badge/github.com/siggy/gographs)](https://goreportcard.com/report/github.com/siggy/gographs)

[gographs](https://gographs.io) renders dependency graphs for Go packages.

[![gographs dependency example](https://gographs.io/graph/github.com/siggy/gographs.svg?cluster=true)](https://gographs.io)

## Badge Markdown

```md
[![gographs](https://gographs.io/badge.svg)](https://gographs.io/repo/GO_REPO?[cluster=true|false])
```

Example
```md
[![gographs](https://gographs.io/badge.svg)](https://gographs.io/repo/github.com/siggy/gographs?cluster=true)
```

## HTTP Endpoints

| Endpoint | Desc |
| --- | --- |
| [/](https://gographs.io) | Defaults to rendering this Go repo. |
| [/repo/GO_REPO?cluster=false\|true](https://gographs.io/repo/github.com/siggy/gographs?cluster=true) | Permalink to a repo. Use `POST` to refresh. |
| [/graph/GO_REPO.svg?cluster=false\|true&theme=light\|dark](https://gographs.io/graph/github.com/siggy/gographs.svg?cluster=true) | SVG direct link. Use `POST` to refresh. |
| [/graph/GO_REPO.dot?cluster=false\|true&theme=light\|dark](https://gographs.io/graph/github.com/siggy/gographs.dot?cluster=true) | GraphViz DOT direct link. Use `POST` to refresh. |
| [/svg?url=SVG_URL](https://gographs.io/svg?url=https://upload.wikimedia.org/wikipedia/commons/0/05/Go_Logo_Blue.svg) | Permalink to view an arbitrary SVG URL. |

## Themes

Both graph endpoints accept an optional `theme` query parameter:

| Value | Output |
| --- | --- |
| omitted (default) | Historical output: goda's colors, white background, black text. Nothing changes for existing links. |
| `light` | White canvas with dark text, for light pages and printouts. |
| `dark` | Dark canvas (`#12161b`) with light text and lightened node/edge colors, for dark pages. |

```md
![dark dependency graph](https://gographs.io/graph/github.com/siggy/gographs.svg?cluster=true&theme=dark)
```

An unknown `theme` value is rejected with `400 Bad Request` rather than
silently falling back to the default, and it is never interpolated into the
generated DOT. Cache entries are stored per repo, cluster flag, and theme, so
the three variants never overwrite each other. A `refresh` (`POST`) clears
every theme/cluster variant for that repo.

An empty graph (a repo with no renderable packages) returns `422 Unprocessable
Entity`; a Graphviz failure returns `500` with the Graphviz message in the
logs.

## Local dev

### First-time setup

```bash
go install tool
brew install graphviz # or equivalent
brew install valkey # or equivalent
valkey-cli ping
```

### Boot server

```bash
go run main.go --log-level debug
```

Browse to http://localhost:8888

## Lint check

```bash
bin/lint
```

## Credits

This tool is built using many open source packages, but two in particular
deserve special mention, as this site is essentially a mashup of them:

- [goda](https://github.com/loov/goda)
- [SVGPan](https://github.com/bumbu/svg-pan-zoom)

[`pkg/repo`](./pkg/repo) is based on [Go Report Card](https://github.com/gojp/goreportcard)

[![gographs](https://gographs.io/badge.svg)](https://gographs.io/repo/github.com/siggy/gographs?cluster=true) courtesy of [shields.io](https://shields.io/)
