package agent

import (
	"github.com/sebastian93921/artifex/locale"
	"log"
	"path/filepath"

	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/noaadapter"
)

// noaWarn returns a diagnostics sink tagging non-fatal noa messages with the
// session, routed through the package logger (agents have no per-instance one).
func noaWarn(session string) func(string) {
	return func(msg string) { log.Printf("[noa] %s: %s", session, msg) }
}

// noa is norma v0.4.0's model-driven context compaction, exposed as an experimental
// setting. It is mutually exclusive with built-in compaction. noaadapter.Enable
// installs the Compactor, Compress tool, and three persistent prompt blocks.
// Without Enable, ordinary compaction remains active. Each agent's noaEnabledFn
// is read once per run, so changes affect subsequent runs without rebuilding agents.

// enableNoa adds noa to opts when its resolver enables it. archiveRoot is the
// persistent global workDir; archives share <workDir>/noa instead of task/intent dirs.
// The globally unique sessionID names each archive subdirectory without collisions.
//
// As an experiment, setup failure must not interrupt tasks: warn and retain built-in compaction.
// On success clear opts.Compaction to avoid the warning about two context managers.
func enableNoa(opts *agentcore.Options, enabled func() bool, archiveRoot, sessionID string, onWarn func(string)) {
	if enabled == nil || !enabled() {
		return
	}
	if opts.OnWarn == nil {
		opts.OnWarn = onWarn
	}
	if err := noaadapter.Enable(opts, noaadapter.Options{
		ArchiveBaseDir: filepath.Join(archiveRoot, "noa"),
		SessionID:      sessionID,
		OnWarn:         onWarn,
	}); err != nil {
		if onWarn != nil {
			onWarn(locale.Text(locale.ServerDefault(), "Could not enable noa compaction; falling back to built-in compaction: ") + err.Error())
		}
		return
	}
	// Compactor supersedes Compaction; explicitly clear the latter to avoid repeated warnings.
	opts.Compaction = nil
}
