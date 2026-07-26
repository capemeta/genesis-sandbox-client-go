package sandbox

import "testing"

func TestBuildSubmitJobRequestUsesLanguageForCode(t *testing.T) {
	req := buildSubmitJobRequest("print(1)", resolveExecOptions([]ExecOption{WithLang("python")}), defaultSandboxOptions())
	if req.Code != "print(1)" {
		t.Fatalf("Code = %q", req.Code)
	}
	if req.Language != "python" {
		t.Fatalf("Language = %q, want python", req.Language)
	}
	if len(req.Command) != 0 {
		t.Fatalf("Command = %v, want empty", req.Command)
	}
	if req.Environment != nil {
		t.Fatalf("Environment = %+v, want nil default profile resolution", req.Environment)
	}
}

func TestBuildSubmitJobRequestShellUsesCommand(t *testing.T) {
	req := buildSubmitJobRequest("echo hi", resolveExecOptions([]ExecOption{WithLang("shell")}), defaultSandboxOptions())
	if req.Code != "" || req.Language != "" {
		t.Fatalf("job shell should not use code/language: %+v", req)
	}
	if len(req.Command) != 3 || req.Command[0] != "/bin/sh" || req.Command[1] != "-c" || req.Command[2] != "echo hi" {
		t.Fatalf("Command = %v", req.Command)
	}
}

func TestBuildExecSessionRequestShellUsesCommand(t *testing.T) {
	req := buildExecSessionRequest("echo hi", resolveExecOptions([]ExecOption{WithLang("shell")}))
	if req.Code != "" || req.Language != "" {
		t.Fatalf("session shell should not use code/language: %+v", req)
	}
	if len(req.Command) != 3 || req.Command[0] != "/bin/sh" || req.Command[1] != "-c" || req.Command[2] != "echo hi" {
		t.Fatalf("Command = %v", req.Command)
	}
}
