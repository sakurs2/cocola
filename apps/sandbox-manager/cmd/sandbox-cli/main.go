// sandbox-cli is a thin debug client for the SandboxService gRPC API.
//
// It exists so a developer can exercise the full create -> exec -> destroy
// loop without writing code. It is NOT part of the runtime data path; the
// real caller is agent-runtime. Keep it dependency-light.
//
//	sandbox-cli -addr :50051 demo                  # full create/exec/destroy
//	sandbox-cli -addr :50051 create -user u1 -session s1
//	sandbox-cli -addr :50051 exec   -id sbx-xxx -- echo hello
//	sandbox-cli -addr :50051 destroy -id sbx-xxx
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	sandboxv1 "github.com/cocola-project/cocola/packages/proto/gen/go/cocola/sandbox/v1"
)

func main() {
	addr := flag.String("addr", ":50051", "sandbox-manager gRPC address")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: sandbox-cli [-addr :50051] <demo|create|exec|destroy> ...")
		os.Exit(2)
	}

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fatal("dial %s: %v", *addr, err)
	}
	defer conn.Close()
	cli := sandboxv1.NewSandboxServiceClient(conn)

	switch args[0] {
	case "demo":
		runDemo(cli)
	case "create":
		fs := flag.NewFlagSet("create", flag.ExitOnError)
		user := fs.String("user", "demo-user", "user id")
		session := fs.String("session", "demo-session", "session id")
		image := fs.String("image", "", "image (default provider image)")
		_ = fs.Parse(args[1:])
		sb := mustCreate(cli, *user, *session, *image)
		fmt.Println(sb.GetId())
	case "exec":
		fs := flag.NewFlagSet("exec", flag.ExitOnError)
		id := fs.String("id", "", "sandbox id")
		_ = fs.Parse(args[1:])
		cmd := fs.Args()
		if *id == "" || len(cmd) == 0 {
			fatal("exec requires -id and a command after --")
		}
		code := mustExec(cli, *id, cmd)
		os.Exit(int(code))
	case "destroy":
		fs := flag.NewFlagSet("destroy", flag.ExitOnError)
		id := fs.String("id", "", "sandbox id")
		_ = fs.Parse(args[1:])
		if *id == "" {
			fatal("destroy requires -id")
		}
		mustDestroy(cli, *id)
		fmt.Println("destroyed", *id)
	default:
		fatal("unknown subcommand %q", args[0])
	}
}

func runDemo(cli sandboxv1.SandboxServiceClient) {
	fmt.Println("[1/3] create")
	sb := mustCreate(cli, "demo-user", "demo-session", "")
	fmt.Println("      ->", sb.GetId(), sb.GetEndpoint())

	fmt.Println("[2/3] exec: echo hello-from-cocola")
	code := mustExec(cli, sb.GetId(), []string{"sh", "-c", "echo hello-from-cocola; uname -a"})
	fmt.Println("      -> exit", code)

	fmt.Println("[3/3] destroy")
	mustDestroy(cli, sb.GetId())
	fmt.Println("      -> ok")
	fmt.Println("DEMO OK")
}

func mustCreate(cli sandboxv1.SandboxServiceClient, user, session, image string) *sandboxv1.Sandbox {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	resp, err := cli.Create(ctx, &sandboxv1.CreateRequest{
		Spec: &sandboxv1.SandboxSpec{
			UserId:    user,
			SessionId: session,
			Image:     image,
		},
	})
	if err != nil {
		fatal("create: %v", err)
	}
	return resp.GetSandbox()
}

func mustExec(cli sandboxv1.SandboxServiceClient, id string, cmd []string) int32 {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	stream, err := cli.Exec(ctx, &sandboxv1.ExecRequest{SandboxId: id, Cmd: cmd})
	if err != nil {
		fatal("exec: %v", err)
	}
	var exit int32
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			fatal("exec recv: %v", err)
		}
		switch ev.GetKind() {
		case sandboxv1.ExecEventKind_EXEC_EVENT_KIND_STDOUT:
			os.Stdout.Write(ev.GetStdout())
		case sandboxv1.ExecEventKind_EXEC_EVENT_KIND_STDERR:
			os.Stderr.Write(ev.GetStderr())
		case sandboxv1.ExecEventKind_EXEC_EVENT_KIND_EXIT:
			exit = ev.GetExitCode()
		case sandboxv1.ExecEventKind_EXEC_EVENT_KIND_ERROR:
			fatal("exec error: %s", ev.GetError())
		}
	}
	return exit
}

func mustDestroy(cli sandboxv1.SandboxServiceClient, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := cli.Destroy(ctx, &sandboxv1.DestroyRequest{SandboxId: id}); err != nil {
		fatal("destroy: %v", err)
	}
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "sandbox-cli: "+format+"\n", a...)
	os.Exit(1)
}
