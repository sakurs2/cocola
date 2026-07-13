package doctor

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/cocola-project/cocola/apps/cli/internal/compose"
	"github.com/cocola-project/cocola/apps/cli/internal/config"
)

type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type Report struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

func Run(ctx context.Context, paths config.Paths) Report {
	report := Report{OK: true}
	add := func(check Check) {
		report.Checks = append(report.Checks, check)
		if !check.OK {
			report.OK = false
		}
	}
	docker, err := compose.DockerBinary()
	if err != nil {
		add(Check{Name: "docker", Message: err.Error()})
		return report
	}
	add(Check{Name: "docker", OK: true, Message: "command found"})

	if err := run(ctx, docker, "info"); err != nil {
		add(Check{Name: "docker daemon", Message: "unavailable"})
	} else {
		add(Check{Name: "docker daemon", OK: true, Message: "available"})
	}
	if err := run(ctx, docker, "compose", "version"); err != nil {
		add(Check{Name: "docker compose", Message: "Compose v2 is unavailable"})
	} else {
		add(Check{Name: "docker compose", OK: true, Message: "available"})
	}

	if _, err := os.Stat(paths.Environment); err != nil {
		add(Check{Name: "installation", Message: "not installed in " + paths.Home})
		return report
	}
	runner, err := compose.New(paths, nil, io.Discard, io.Discard)
	if err != nil {
		add(Check{Name: "installation", Message: err.Error()})
		return report
	}
	add(Check{Name: "installation", OK: true, Message: paths.Home})
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := runner.Validate(checkContext); err != nil {
		add(Check{Name: "compose config", Message: err.Error()})
	} else {
		add(Check{Name: "compose config", OK: true, Message: "valid"})
	}
	return report
}

func run(ctx context.Context, command string, args ...string) error {
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return exec.CommandContext(checkContext, command, args...).Run()
}
