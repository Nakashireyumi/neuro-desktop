package main

// The executor protocol lives in `docs/EXECUTOR_PROTOCOL.md`. This file holds
// the one authoritative list of commands the bridge forwards to the executor
// process (the Python agent in `desktop/backend/python/controller/agent.py`).
//
// Everything that is *not* in this list is handled inside the bridge itself:
// the desktop shell intents (`open_windows_menu`, …), the catalog, the
// extension commands, and the `game_*` interface. Keeping the list explicit
// means a typo or a half-finished refactor fails loudly here instead of turning
// into "the action silently did nothing" on a user's machine.
var executorCommands = map[CommandType]bool{
	// Input primitives (also used by the game layer).
	CmdMouseMove:         true,
	CmdMouseClick:        true,
	CmdMoveMouseRelative: true,
	CmdMouseHoldFor:      true,
	CmdKeyPress:          true,
	CmdKeyCombo:          true,
	CmdKeyHoldFor:        true,
	CmdKeyReleaseAll:     true,
	CmdTypeText:          true,
	CmdRunScript:         true,

	// Queue control.
	CmdExecuteQueue:     true,
	CmdClearActionQueue: true,

	// Machine capability.
	CmdShellCommand: true,
	CmdGetStatus:    true,

	// Session control.
	CmdShutdownGracefully:  true,
	CmdShutdownImmediately: true,
}

// isExecutorCommand reports whether the bridge must send this command to the
// executor instead of handling it locally.
func isExecutorCommand(name CommandType) bool {
	return executorCommands[name]
}
