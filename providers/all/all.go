// Package all blank-imports every provider package shipped in this module,
// registering all of them into the root notify package's registry as a
// side effect. Import this package for its side effects only —
//
//	import _ "github.com/Wikid82/go_notify_yourself/providers/all"
//
// — when you want every built-in provider type available to notify.New
// without importing each provider package individually. This is the
// closest equivalent Go offers to true runtime auto-discovery: Go has no
// mechanism to discover and load packages that were not compiled into the
// binary, so *some* single import is unavoidable — providers/all exists so
// that import is exactly one line, added once, rather than one line per
// provider that must be kept in sync by hand as the provider list grows.
//
// Tradeoff: importing this package links every provider package's
// transitive dependencies into your binary, even ones you never configure.
// A consumer that wants tighter control over what's linked should
// hand-pick individual provider imports instead (e.g.
// import _ "github.com/Wikid82/go_notify_yourself/providers/discord") —
// both styles are equally supported by the registry; neither requires a
// different notify.Register/New API.
package all

import (
	_ "github.com/Wikid82/go_notify_yourself/providers/discord"
	_ "github.com/Wikid82/go_notify_yourself/providers/email"
	_ "github.com/Wikid82/go_notify_yourself/providers/gotify"
	_ "github.com/Wikid82/go_notify_yourself/providers/ntfy"
	_ "github.com/Wikid82/go_notify_yourself/providers/pushover"
	_ "github.com/Wikid82/go_notify_yourself/providers/slack"
	_ "github.com/Wikid82/go_notify_yourself/providers/telegram"
	_ "github.com/Wikid82/go_notify_yourself/providers/webhook"
)
