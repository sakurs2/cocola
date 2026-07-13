package main

import (
	"context"
	"os"

	"github.com/cocola-project/cocola/apps/cli/internal/command"
)

func main() {
	if err := command.Execute(context.Background(), os.Args[1:], command.IO{
		In: os.Stdin, Out: os.Stdout, Err: os.Stderr,
	}); err != nil {
		os.Exit(1)
	}
}
