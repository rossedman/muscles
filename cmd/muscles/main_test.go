package main

import "testing"

func TestRun(t *testing.T) {
	defer func(f func() int) { execute = f }(execute)
	execute = func() int { return 5 }
	if run() != 5 {
		t.Fatal("run doesn't return the CLI's exit code")
	}
}
