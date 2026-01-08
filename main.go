package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/MatusOllah/slogcolor"
	"github.com/ncruces/zenity"
)

// getLogLevel gets the log level from command-line flags.
func getLogLevel() slog.Leveler {
	switch s := strings.ToLower(*logLevelFlag); s {
	case "":
		return slog.LevelInfo
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		panic(fmt.Sprintf("invalid log level: \"%s\"; should be one of \"debug\", \"info\", \"warn\", \"error\"", s))
	}
}

func main() {
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s path/to/rom.nes\n", os.Args[0])
		if err := zenity.Warning("Please provide a path to the ROM file.", zenity.Title("smolnes-go")); err != nil {
			slog.Error("failed to show warning dialog", "error", err)
		}
		os.Exit(1)
	}

	// Logger
	opts := slogcolor.DefaultOptions
	opts.Level = getLogLevel()
	opts.SrcFileLength = 16
	slog.SetDefault(slog.New(slogcolor.NewHandler(os.Stderr, opts)))

	slog.Info("smolnes-go version", "version", Version())
	slog.Info("Go version", "version", runtime.Version(), "os", runtime.GOOS, "arch", runtime.GOARCH)

	rom, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		slog.Error("failed to read ROM file", "path", flag.Arg(0), "error", err)
		handleError(fmt.Errorf("failed to read ROM file: %w", err))
	}

	slog.Info("initializing emulator")
	g, err := NewGame(rom)
	if err != nil {
		slog.Error("failed to initialize game", "error", err)
		handleError(fmt.Errorf("failed to initialize game: %w", err))
	}

	slog.Info("initializing ebiten")
	g.InitEbiten()

	slog.Info("starting game")
	if err := g.Start(); err != nil {
		slog.Error("game exited with error", "error", err)
		handleError(fmt.Errorf("game exited with error: %w", err))
	}
}

func handleError(err error) {
	if err := zenity.Error(err.Error(), zenity.Title("smolnes-go")); err != nil {
		// really?!
		slog.Error("failed to show error dialog", "error", err)
	}
	os.Exit(1)
}
