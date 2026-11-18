// Package assets embeds public trust material; it contains no private keys.
package assets

import _ "embed"

//go:embed github-public-key.txt
var GitHubPublicKey string

//go:generate sh -c "gzip -n -9 -c certs/github-roots.pem > certs/github-roots.pem.gz"

//go:embed certs/github-roots.pem.gz
var GitHubRootsGzip string

//go:embed certs/probe-roots.pem
var ProbeRoots []byte
