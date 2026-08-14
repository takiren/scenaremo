package scenaremo

import "embed"

//go:embed renderer/src renderer/package.json renderer/pnpm-lock.yaml renderer/pnpm-workspace.yaml renderer/remotion.config.ts renderer/tsconfig.json
var Renderer embed.FS
