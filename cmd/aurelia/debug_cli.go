package main

import "fmt"

// debugCommand is the CLI entrypoint for 'aurelia debug <subcommand>'.
// Mirrors the /debug last|run|errors Telegram commands exposed via the
// observability spec, but accessible from the command line.
// Full implementation pending — see internal/telegram/commands.go for the
// BotController handlers this should mirror.
func debugCommand(args []string) error {
	if len(args) == 0 {
		fmt.Println("Usage: aurelia debug <subcommand>")
		fmt.Println("")
		fmt.Println("Subcommands:")
		fmt.Println("  last      Show the last pipeline run for a chat")
		fmt.Println("  run       Show a specific run by ID")
		fmt.Println("  errors    Show recent failed runs")
		return nil
	}
	return fmt.Errorf("debug subcommand %q not yet implemented via CLI — use Telegram /debug commands", args[0])
}
