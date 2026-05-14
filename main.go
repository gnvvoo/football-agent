package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/joho/godotenv"

	"football-agent/agent"
)

func main() {
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
	fmt.Println("준비 완료. 질문을 입력하세요 (빈 입력 2회 또는 Ctrl+C로 종료).\n")

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
