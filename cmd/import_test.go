package cmd

import (
	"strings"
	"testing"
)

func TestImportCommandRegistered(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"import"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name() != "import" {
		t.Errorf("got %q", cmd.Name())
	}
	if cmd.Flags().Lookup("from") == nil || cmd.Flags().Lookup("file") == nil || cmd.Flags().Lookup("dry-run") == nil {
		t.Fatal("expected --from, --file, --dry-run flags")
	}
	if !strings.Contains(cmd.Short, "Import SSH") {
		t.Errorf("short = %q", cmd.Short)
	}

	// Verify completions include termius, finalshell, and json
	if cmd.ValidArgsFunction != nil {
		args, _ := cmd.ValidArgsFunction(cmd, []string{}, "")
		hasTermius, hasFinalShell, hasJSON := false, false, false
		for _, a := range args {
			if a == "termius" {
				hasTermius = true
			}
			if a == "finalshell" {
				hasFinalShell = true
			}
			if a == "json" {
				hasJSON = true
			}
		}
		if !hasTermius || !hasFinalShell || !hasJSON {
			t.Errorf("expected termius, finalshell, and json in completions, got %v", args)
		}
	}
}
