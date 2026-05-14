package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const (
	defaultCLIPath = "./football-cli"
	cliTimeout     = 10 * time.Second
)

// CLIResult는 football-cli 실행 결과 구조체
type CLIResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

func cliPath() string {
	if p := os.Getenv("FOOTBALL_CLI_PATH"); p != "" {
		return p
	}
	return defaultCLIPath
}

// RunCLI는 football-cli 바이너리를 실행하고 결과 반환
func RunCLI(args []string) (*CLIResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cliPath(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return nil, fmt.Errorf("CLI 실행 실패: %w", err)
		}
		exitCode = exitErr.ExitCode()
	}

	return &CLIResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, nil
}

// RunCLIJSON는 CLIResult를 JSON 문자열로 직렬화해 반환
func RunCLIJSON(args []string) (string, error) {
	result, err := RunCLI(args)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("JSON 직렬화 실패: %w", err)
	}
	return string(b), nil
}
