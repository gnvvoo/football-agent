package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"football-agent/agent"
)

// nonInteractiveTimeout은 --type/--match로 실행되는 비대화형 모드 전체
// (에이전트 루프 + 구조화 호출)에 적용되는 제한 시간이다. the-half-space
// 백엔드(AgentService)의 프로세스 타임아웃(120초)보다 짧게 설정한다.
const nonInteractiveTimeout = 100 * time.Second

func main() {
	typeFlag := flag.String("type", "", "콘텐츠 타입: preview | review")
	matchFlag := flag.Int64("match", 0, "경기(match) ID")
	outputFlag := flag.String("output", "", "출력 형식 (json)")
	flag.Parse()

	// --type이 지정되면 비대화형 모드로 동작한다. 플래그가 없으면 기존
	// REPL 동작을 그대로 유지한다(하위 호환).
	if *typeFlag != "" {
		os.Exit(runNonInteractive(*typeFlag, *matchFlag, *outputFlag))
	}

	runREPL()
}

// runNonInteractive는 `football-agent --type preview|review --match <id> --output json`
// 형태의 단발성 실행을 처리한다. 표준 출력에는 AgentResponse에 대응하는
// JSON 오브젝트 한 줄만 출력하고, 그 외 모든 진행 로그는 표준 에러로 보낸다.
// 성공 시 0, 실패 시 0이 아닌 값을 반환한다(호출자는 os.Exit로 전달).
func runNonInteractive(agentType string, matchID int64, output string) int {
	if agentType != "preview" && agentType != "review" {
		fmt.Fprintf(os.Stderr, "오류: --type 값은 'preview' 또는 'review' 여야 합니다 (입력값: %q)\n", agentType)
		return 1
	}
	if matchID <= 0 {
		fmt.Fprintln(os.Stderr, "오류: --match 값이 유효하지 않습니다")
		return 1
	}
	if output != "" && output != "json" {
		fmt.Fprintf(os.Stderr, "오류: --output 값은 'json'만 지원합니다 (입력값: %q)\n", output)
		return 1
	}

	_ = godotenv.Load()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "오류: GEMINI_API_KEY 환경변수가 설정되지 않았습니다")
		return 1
	}

	fmt.Fprintln(os.Stderr, "football-agent 초기화 중...")
	a, err := agent.New(apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "에이전트 초기화 실패: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), nonInteractiveTimeout)
	defer cancel()

	fmt.Fprintf(os.Stderr, "%s 생성 중 (matchId=%d)...\n", agentType, matchID)
	prompt := agent.BuildPrompt(agentType, matchID)
	result, err := a.RunTraced(ctx, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "에이전트 실행 실패: %v\n", err)
		return 1
	}

	fmt.Fprintln(os.Stderr, "구조화된 응답 생성 중...")
	schema := agent.StructureSchema(agentType)
	structurePrompt := agent.BuildStructurePrompt(agentType, matchID, result.Answer)

	var structured *agent.StructuredAnswer
	totalTokens := result.TotalTokens
	modelVersion := result.ModelVersion

	structureResult, err := a.Structure(ctx, structurePrompt, schema)
	if err != nil {
		fmt.Fprintf(os.Stderr, "경고: 구조화 응답 생성 실패, 원본 답변으로 대체합니다: %v\n", err)
	} else {
		totalTokens += structureResult.TotalTokens
		if structureResult.ModelVersion != "" {
			modelVersion = structureResult.ModelVersion
		}
		structured, err = agent.ParseStructuredJSON(structureResult.RawJSON)
		if err != nil {
			fmt.Fprintf(os.Stderr, "경고: 구조화 응답 파싱 실패, 원본 답변으로 대체합니다: %v\n", err)
		}
	}

	response := agent.BuildAgentResponse(agentType, matchID, structured, result.Answer, result.ToolCalls, totalTokens, modelVersion)

	encoded, err := json.Marshal(response)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: 응답 직렬화 실패: %v\n", err)
		return 1
	}

	fmt.Println(string(encoded))
	return 0
}

// runREPL은 기존의 대화형(REPL) 진입점이다.
func runREPL() {
	_ = godotenv.Load()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "오류: GEMINI_API_KEY 환경변수가 설정되지 않았습니다")
		os.Exit(1)
	}

	fmt.Println("football-agent 초기화 중...")
	a, err := agent.New(apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "에이전트 초기화 실패: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("준비 완료. 질문을 입력하세요 (빈 입력 2회 또는 Ctrl+C로 종료).")
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\n종료합니다.")
		cancel()
		os.Exit(0)
	}()

	scanner := bufio.NewScanner(os.Stdin)
	emptyCount := 0

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			emptyCount++
			if emptyCount >= 2 {
				fmt.Println("종료합니다.")
				break
			}
			continue
		}
		emptyCount = 0

		answer, err := a.Run(ctx, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "오류: %v\n", err)
			continue
		}
		fmt.Printf("\n%s\n\n", answer)
	}
}
