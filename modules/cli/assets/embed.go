package assets

import "embed"

//go:generate cp ../../../config/default/cli.yaml ./config.yaml
//go:generate cp ../../../config/default/cli.toml ./config.toml
//go:generate cp ../../../config/default/cli.json ./config.json

//go:embed *
var FS embed.FS
