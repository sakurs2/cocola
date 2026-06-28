// opensandbox-verify is a MANUAL, real-server verification harness for the
// OpenSandbox SandboxProvider (#22, ADR-0013). It is NOT a unit test and NOT on
// the runtime data path: it exists so a developer in a compliant environment
// (where the OpenSandbox server may bind a port and each sandbox may run execd)
// can drive the full Create -> Health -> Exec(streaming) -> Pause -> Resume ->
// Destroy lifecycle against a live server and eyeball every ExecEvent plus the
// resume latency that informs the #15 RAM-kept-resume decision.
//
// It talks DIRECTLY to the opensandbox.Provider (no gRPC, no sandbox-manager),
// so it isolates the provider <-> OpenSandbox seam from the rest of cocola.
//
// Usage:
//
//	export COCOLA_OPENSANDBOX_URL="http://localhost:8090/v1"
//	export COCOLA_OPENSANDBOX_API_KEY="..."        # omit if server.api_key empty
//	go run ./cmd/opensandbox-verify                 # full lifecycle, default image (python:3.12-slim)
//	go run ./cmd/opensandbox-verify -image python:3.12-slim -keep
//	go run ./cmd/opensandbox-verify -cpu 0.5 -mem 512 -egress pypi.org,files.pythonhosted.org
//
// Flags:
//
//	-image    OCI image for the sandbox (default: python:3.12-slim)
//	-cpu      CPU cores (default 0.5; 0 = omit — but the server requires resourceLimits)
//	-mem      memory MiB (default 512; 0 = omit — but the server requires resourceLimits)
//	-egress   comma-separated egress allowlist domains; empty = no egress policy
//	-timeout  overall wall-clock budget for the whole run (default 5m)
//	-keep     do NOT Destroy at the end (leave the sandbox for manual poking)
//	-skip-pause  skip the Pause/Resume stage (some runtimes don't support it yet)
//
// Exit code is 0 only if every stage the harness ran succeeded.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider/opensandbox"
)

// defaultImage is the OCI image the harness launches when -image is not given.
// OpenSandbox requires the create request to carry exactly one of image or
// snapshotId (there is no server-side default image), so we must always send one.
const defaultImage = "python:3.12-slim"

func main() {
	image := flag.String("image", defaultImage, "OCI image to launch the sandbox from")
	cpu := flag.Float64("cpu", 0.5, "CPU cores, e.g. 0.5 (0 = omit, but the server requires resourceLimits)")
	mem := flag.Int64("mem", 512, "memory MiB, e.g. 512 (0 = omit, but the server requires resourceLimits)")
	egress := flag.String("egress", "", "comma-separated egress allowlist domains")
	timeout := flag.Duration("timeout", 5*time.Minute, "overall wall-clock budget")
	keep := flag.Bool("keep", false, "do not Destroy the sandbox at the end")
	skipPause := flag.Bool("skip-pause", false, "skip the Pause/Resume stage")
	flag.Parse()

	// Construct the provider straight from env (COCOLA_OPENSANDBOX_URL /
	// COCOLA_OPENSANDBOX_API_KEY). New() fails fast if the URL is unset, which
	// is the single most common misconfiguration.
	p, err := opensandbox.New()
	if err != nil {
		fatal("provider init: %v\n\nhint: export COCOLA_OPENSANDBOX_URL=\"http://localhost:8090/v1\"", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	spec := provider.SandboxSpec{
		UserID:    "verify-user",
		SessionID: "verify-session",
		Image:     *image,
		Resources: provider.Resources{CPUCores: *cpu, MemoryMiB: *mem},
	}
	if *egress != "" {
		spec.Networking.EgressAllowlist = splitCSV(*egress)
	}

	run := &runner{p: p, ctx: ctx}

	// ---- Stage 1: Create -----------------------------------------------------
	stage("1. Create")
	sb, err := p.Create(ctx, spec)
	if err != nil {
		fatal("create: %v", err)
	}
	fmt.Printf("   sandbox id = %s\n   endpoint   = %s\n", sb.ID, sb.Endpoint)
	sid := sb.ID

	// From here on, always attempt Destroy on exit unless -keep was set.
	defer func() {
		if *keep {
			fmt.Printf("\n[keep] leaving sandbox %s alive (use your client to DELETE it)\n", sid)
			return
		}
		stage("Cleanup: Destroy")
		dctx, dcancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer dcancel()
		if err := p.Destroy(dctx, sid); err != nil {
			fmt.Printf("   destroy FAILED: %v\n", err)
			run.fail("destroy")
			return
		}
		fmt.Println("   destroyed OK")
	}()

	// ---- Stage 2: Health-poll to Running ------------------------------------
	stage("2. Health (poll until Running)")
	if err := waitRunning(ctx, p, sid, 90*time.Second); err != nil {
		fatal("sandbox never reached Running: %v", err)
	}
	fmt.Println("   Running OK")

	// ---- Stage 3: Exec streaming matrix -------------------------------------
	stage("3. Exec (streaming)")
	run.exec("3a. basic stdout", sid, provider.ExecRequest{
		Cmd: []string{"sh", "-c", "echo hello-from-cocola; uname -a"},
	}, 0)
	run.exec("3b. stderr capture", sid, provider.ExecRequest{
		Cmd: []string{"sh", "-c", "echo to-stderr 1>&2"},
	}, 0)
	run.exec("3c. non-zero exit", sid, provider.ExecRequest{
		Cmd: []string{"sh", "-c", "exit 3"},
	}, 3)
	run.exec("3d. env + cwd", sid, provider.ExecRequest{
		Cmd: []string{"sh", "-c", "echo cwd=$(pwd); echo FOO=$FOO"},
		Cwd: "/tmp",
		Env: map[string]string{"FOO": "bar"},
	}, 0)
	run.exec("3e. large stdout (~1MiB)", sid, provider.ExecRequest{
		Cmd: []string{"sh", "-c", "head -c 1048576 /dev/zero | tr '\\0' 'x' | wc -c"},
	}, 0)

	// ---- Stage 4: write-then-read via Exec ----------------------------------
	// WriteFile/ReadFile are deferred in the PoC, so the harness proves
	// round-trip file IO through shell exec instead (the same capability the
	// runtime relies on today).
	stage("4. file round-trip via exec")
	const marker = "cocola-verify-marker-42"
	run.exec("4a. write file", sid, provider.ExecRequest{
		Cmd: []string{"sh", "-c", "echo " + marker + " > /tmp/cocola-verify.txt"},
	}, 0)
	run.exec("4b. read file back", sid, provider.ExecRequest{
		Cmd: []string{"sh", "-c", "cat /tmp/cocola-verify.txt"},
	}, 0)
	fmt.Printf("   (expect 4b stdout to contain %q)\n", marker)

	// ---- Stage 5: Pause / Resume + latency ----------------------------------
	if *skipPause {
		fmt.Println("\n[skip-pause] Pause/Resume stage skipped by flag")
	} else {
		stage("5. Pause / Resume")
		if err := p.Pause(ctx, sid); err != nil {
			fmt.Printf("   pause FAILED: %v\n", err)
			run.fail("pause")
		} else {
			fmt.Println("   paused OK")
			// Best-effort: confirm the server reports a non-Running state.
			if hs, err := p.Health(ctx, sid); err == nil {
				fmt.Printf("   health after pause: healthy=%v detail=%q\n", hs.Healthy, hs.Detail)
			}
			start := time.Now()
			if err := p.Resume(ctx, sid); err != nil {
				fmt.Printf("   resume FAILED: %v\n", err)
				run.fail("resume")
			} else {
				resumeAccepted := time.Since(start)
				// Measure time until the sandbox is actually Running again —
				// this is the number the #15 RAM-kept-resume decision needs.
				rerr := waitRunning(ctx, p, sid, 60*time.Second)
				toRunning := time.Since(start)
				if rerr != nil {
					fmt.Printf("   resumed but never Running again: %v\n", rerr)
					run.fail("resume->running")
				} else {
					fmt.Printf("   resume accepted in %s; back to Running in %s\n",
						resumeAccepted.Round(time.Millisecond), toRunning.Round(time.Millisecond))
				}
				// Prove state survived the pause/resume: the marker file from
				// stage 4 must still be there.
				run.exec("5a. file survived resume", sid, provider.ExecRequest{
					Cmd: []string{"sh", "-c", "cat /tmp/cocola-verify.txt"},
				}, 0)
			}
		}
	}

	// ---- Summary -------------------------------------------------------------
	fmt.Println()
	if run.failures == 0 {
		fmt.Println("VERIFY OK — all stages passed")
		return
	}
	fmt.Printf("VERIFY FAIL — %d stage(s) failed: %s\n", run.failures, strings.Join(run.failed, ", "))
	// Defer (Destroy) still runs; force a non-zero exit via os.Exit after it.
	defer os.Exit(1)
}

// runner accumulates pass/fail across stages so the harness reports a single
// verdict at the end instead of aborting on the first non-fatal mismatch.
type runner struct {
	p        *opensandbox.Provider
	ctx      context.Context
	failures int
	failed   []string
}

func (r *runner) fail(stage string) {
	r.failures++
	r.failed = append(r.failed, stage)
}

// exec runs one command, prints every streamed ExecEvent verbatim, and checks
// the final exit code against wantExit. Stdout/stderr are echoed with a prefix
// so the wire-level streaming behaviour is visible (ordering, chunking).
func (r *runner) exec(label, sid string, req provider.ExecRequest, wantExit int32) {
	fmt.Printf("  [%s] $ %s\n", label, strings.Join(req.Cmd, " "))
	ch, err := r.p.Exec(r.ctx, sid, req)
	if err != nil {
		fmt.Printf("     exec start FAILED: %v\n", err)
		r.fail(label)
		return
	}
	var (
		gotExit  int32
		sawExit  bool
		gotErr   error
		outBytes int
		errBytes int
	)
	for ev := range ch {
		switch ev.Kind {
		case provider.ExecEventStdout:
			outBytes += len(ev.Stdout)
			fmt.Printf("     [stdout] %s", indent(ev.Stdout))
		case provider.ExecEventStderr:
			errBytes += len(ev.Stderr)
			fmt.Printf("     [stderr] %s", indent(ev.Stderr))
		case provider.ExecEventExit:
			gotExit = ev.Exit
			sawExit = true
		case provider.ExecEventError:
			gotErr = ev.Err
		}
	}
	fmt.Printf("     (stdout=%dB stderr=%dB)\n", outBytes, errBytes)
	switch {
	case gotErr != nil:
		fmt.Printf("     -> ERROR event: %v\n", gotErr)
		r.fail(label)
	case !sawExit:
		fmt.Printf("     -> FAIL: stream ended without an exit event\n")
		r.fail(label)
	case gotExit != wantExit:
		fmt.Printf("     -> FAIL: exit=%d want=%d\n", gotExit, wantExit)
		r.fail(label)
	default:
		fmt.Printf("     -> OK (exit=%d)\n", gotExit)
	}
}

// waitRunning polls Health until the sandbox reports Running or the budget
// elapses. It tolerates transient errors (the sandbox may briefly 404 right
// after Create/Resume) and only fails on timeout.
func waitRunning(ctx context.Context, p *opensandbox.Provider, sid string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	var last string
	for time.Now().Before(deadline) {
		hs, err := p.Health(ctx, sid)
		if err == nil {
			last = hs.Detail
			if hs.Healthy {
				return nil
			}
		} else {
			last = err.Error()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("timeout after %s (last state: %s)", budget, last)
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// indent ensures a trailing newline so multi-line command output stays readable
// under the prefix, without swallowing intentional partial chunks.
func indent(b []byte) string {
	s := string(b)
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func stage(name string) { fmt.Printf("\n=== %s ===\n", name) }

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "opensandbox-verify: "+format+"\n", a...)
	os.Exit(1)
}
