// Package assets embeds public trust material; it contains no private keys.
package assets

import _ "embed"

//go:embed github-public-key.txt
var GitHubPublicKey string

//go:embed certs/github-roots.pem
var GitHubRoots []byte

//go:embed certs/probe-roots.pem
var ProbeRoots []byte
