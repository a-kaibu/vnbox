// Package vnbox holds the files the vnbox command needs at run time besides its code: the
// sources of the helper programs it builds for Wine (tools/), the window manager settings
// (config/openbox-rc.xml) and the patches of its own Wine build (wine-build/). They are built
// into the binary, so `go install` is enough.
package vnbox

import "embed"

//go:embed tools config/openbox-rc.xml wine-build/*.patch wine-build/third_party/fitudao-wine-patches
var Files embed.FS
