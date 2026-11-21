// Package fixtures holds fixed test data shared by the graph, render, and cache
// tests.
package fixtures

// Repo is the repo name used in tests.
const Repo = "example.com/demo"

// DOT is a fixed goda-shaped dependency graph. It mirrors the structure goda
// emits (graph defaults, a package cluster, per-package colors) so tests can
// assert on DOT and SVG attributes without cloning a repository or running
// goda.
const DOT = `digraph G {
    node [penwidth=2 fontsize=10 shape=rectangle target="_graphviz"];
    edge [tailport=e penwidth=2];
    compound=true;
    rankdir=LR;
    newrank=true;
    ranksep="1.5";
    quantum="0.5";
subgraph "cluster_example.com/demo" {
    label="example.com/demo (local)"
    tooltip="example.com/demo (local)"
    href="https://pkg.go.dev/example.com/demo@"
    "example.com/demo" [label="" tooltip="example.com/demo" shape=circle color="#079155b2" rank=0];
    "example.com/demo/a" [label="a\l12 / 0.4KB\l" tooltip="example.com/demo/a" href="https://pkg.go.dev/example.com/demo/a" color="#4a0791b2"];
    "example.com/demo/b" [label="b\l8 / 0.3KB\l" tooltip="example.com/demo/b" href="https://pkg.go.dev/example.com/demo/b" color="#159107b2"];
}
    "example.com/demo" -> "example.com/demo/a" [tooltip="example.com/demo -> example.com/demo/a" color="#4a0791b2"];
    "example.com/demo" -> "example.com/demo/b" [tooltip="example.com/demo -> example.com/demo/b" color="#159107b2"];
    "example.com/demo/a" -> "example.com/demo/b" [tooltip="example.com/demo/a -> example.com/demo/b" color="#159107b2"];
}
`

// EmptyDOT is what goda emits when no packages match: a graph with defaults
// but no node or edge statements.
const EmptyDOT = `digraph G {
    node [penwidth=2 fontsize=10 shape=rectangle target="_graphviz"];
    edge [tailport=e penwidth=2];
    compound=true;
    rankdir=LR;
    newrank=true;
    ranksep="1.5";
    quantum="0.5";
}
`

// SVG is a minimal but complete SVG, used for cache-hit tests.
const SVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg"><g><text>demo</text></g></svg>`
