// Package sandbox provides the standalone Go SDK for Genesis Sandbox,
// including both the low-level HTTP client and the higher-level session helpers.
//
// Sandbox operates in two modes:
//   - Job mode (default): each Run composes SubmitJob + WaitJob
//   - Session mode (after Open): Run reuses the same container (ExecNamedSession)
//
// File operations (Upload/Download/Files) auto-trigger Open() if not already open.
//
// # Quick Start — Job Mode (one-shot)
//
//	result, err := sandbox.QuickPython(ctx, client, `print("hello")`)
//
// # Session Mode — stateful multi-step
//
//	sb, err := sandbox.New(client, sandbox.WithHints("runtime.python"))
//	if err != nil { ... }
//	defer sb.Close()
//	sb.Open(ctx)
//
//	sb.Upload(ctx, "input.csv", reader)
//	result, err := sb.RunPython(ctx, `
//	    import pandas as pd
//	    df = pd.read_csv("input.csv")
//	    print(df.describe())
//	`)
//
// # Concurrent Fan-out (Job mode per job)
//
//	results, err := sandbox.RunBatch(ctx, client, []sandbox.BatchJob{
//	    {CmdOrCode: "print(6*7)", Opts: []sandbox.ExecOption{sandbox.WithLang("python")}},
//	    {CmdOrCode: "echo $((6*7))", Opts: []sandbox.ExecOption{sandbox.WithLang("shell")}},
//	    {CmdOrCode: "console.log(6*7)", Opts: []sandbox.ExecOption{sandbox.WithLang("javascript")}},
//	})
package sandbox
