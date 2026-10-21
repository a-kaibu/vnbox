// Package vnbox holds the files the vnbox command needs at run time besides its code: the
// sources of the helper programs it builds for Wine (tools/), the window manager settings
// (config/openbox-rc.xml), the patches of its own Wine build (wine-build/) and the runtimes some
// DRM needs (runtimes/). They are built into the binary, so `go install` is enough.
package vnbox

import "embed"

//go:embed tools config/openbox-rc.xml wine-build/*.patch wine-build/third_party/fitudao-wine-patches runtimes
var Files embed.FS
