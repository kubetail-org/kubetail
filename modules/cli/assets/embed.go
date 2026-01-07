package assets

import "embed"

//go:generate cp ../../../config/default/cli.yaml ./config.yaml

//go:embed *
var FS embed.FS
