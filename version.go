package main

import "runtime/debug"

var TheVersion = "devel"

func Version() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return TheVersion
	}
	return bi.Main.Version
}
