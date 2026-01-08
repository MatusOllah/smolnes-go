package main

import "flag"

var (
	logLevelFlag = flag.String("log-level", "info", "Log level (\"debug\", \"info\", \"warn\", \"error\")")
)
