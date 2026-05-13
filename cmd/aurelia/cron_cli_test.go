package main

import "testing"

func TestRunCronCLI_BootstrapsInstanceDirectory(t *testing.T) {
	t.Setenv("AURELIA_HOME", t.TempDir())

	if err := runCronCLI([]string{"list"}); err != nil {
		t.Fatalf("cron list with fresh AURELIA_HOME: %v", err)
	}
}
