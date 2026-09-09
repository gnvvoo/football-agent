package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
)

const (
	modelName     = "gemini-2.5-flash"
	maxHistoryLen = 100 // Content 항목 최대 개수 (초과 시 오래된 항목 제거)
	maxIterations = 10  // 에이전트 루프 최대 반복 횟수
)

// Agent는 Gemini 기반 football 에이전트
type Agent struct {
	client       *genai.Client
	history      []*genai.Content
	systemPrompt string
}

// New는 --manifest를 실행해 시스템 프롬프트를 구성하고 에이전트 생성
func New(apiKey string) (*Agent, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("Gemini 클라이언트 생성 실패: %w", err)
	}

	manifest, err := RunCLI(ctx, []string{"--manifest", "--json"})
	if err != nil {
		return nil, fmt.Errorf("manifest 실행 실패: %w", err)
	}

	systemPrompt := fmt.Sprintf(`당신은 유럽 축구 데이터 전문 AI 어시스턴트입니다.
football_cli 도구를 사용해 사용자의 질문에 정확히 답하세요.
모든 답변은 한국어로 작성하세요.
데이터를 조회할 때 exit_code가 3이면 조건을 바꿔 반드시 재시도하세요.
error.suggestions가 있으면 해당 값으로 팀/선수명을 교정해 재시도하세요.

football-cli 매니페스트:
%s`, manifest.Stdout)

	return &Agent{
		client:       client,
		systemPrompt: systemPrompt,
	}, nil
}

// ToolCall은 에이전트 루프 중 실행된 도구 호출 1건(이름+인자)을 기록한다
type ToolCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// RunResult는 RunTraced의 결과: 최종 답변, 실행된 도구 호출 목록, 모델 버전, 총 토큰 수
type RunResult struct {
	Answer       string
	ToolCalls    []ToolCall
	ModelVersion string
	TotalTokens  int32
}

// Run은 유저 메시지를 받아 에이전트 루프를 실행하고 최종 답변 반환
func (a *Agent) Run(ctx context.Context, userMessage string) (string, error) {
	result, err := a.RunTraced(ctx, userMessage)
	if err != nil {
		return "", err
	}
	return result.Answer, nil
}

// RunTraced는 Run과 동일하게 에이전트 루프를 실행하되, 실행된 도구 호출 목록과
// 모델 버전/토큰 사용량 등 비대화형 모드에서 필요한 부가 정보를 함께 반환한다
func (a *Agent) RunTraced(ctx context.Context, userMessage string) (*RunResult, error) {
	userContent := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: userMessage}},
	}

	// 현재 턴에서 사용할 전체 컨텐츠 (히스토리 + 새 메시지)
	contents := make([]*genai.Content, len(a.history))
	copy(contents, a.history)
	contents = append(contents, userContent)

	// 이번 턴에 추가될 히스토리 항목
	var newTurn []*genai.Content
	newTurn = append(newTurn, userContent)

	now := time.Now()
	dateInfo := fmt.Sprintf(
		"오늘 날짜: %s (%s)\n날짜 표현(오늘, 어제, 내일, 이번 주/저번 주 요일 등)은 반드시 YYYY-MM-DD 형식으로 변환해 --date 인자에 사용하세요.\n\n",
		now.Format("2006-01-02"), weekdayKo(now),
	)

	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{{Text: dateInfo + a.systemPrompt}},
		},
		Tools: []*genai.Tool{FootballCLITool},
	}

	var toolCalls []ToolCall
	var modelVersion string
	var totalTokens int32

	for range maxIterations {
		resp, err := a.client.Models.GenerateContent(ctx, modelName, contents, config)
		if err != nil {
			return nil, fmt.Errorf("Gemini API 호출 실패: %w", err)
		}
		if len(resp.Candidates) == 0 {
			return nil, fmt.Errorf("Gemini 응답 후보 없음")
		}

		if resp.ModelVersion != "" {
			modelVersion = resp.ModelVersion
		}
		if resp.UsageMetadata != nil {
			totalTokens += resp.UsageMetadata.TotalTokenCount
		}

		modelContent := resp.Candidates[0].Content
		contents = append(contents, modelContent)
		newTurn = append(newTurn, modelContent)

		// 함수 호출 파트 수집
		var fcs []*genai.FunctionCall
		for _, part := range modelContent.Parts {
			if part.FunctionCall != nil {
				fcs = append(fcs, part.FunctionCall)
			}
		}

		// 함수 호출이 없으면 최종 텍스트 응답
		if len(fcs) == 0 {
			var sb strings.Builder
			for _, part := range modelContent.Parts {
				if part.Text != "" {
					sb.WriteString(part.Text)
				}
			}
			a.history = append(a.history, newTurn...)
			if len(a.history) > maxHistoryLen {
				a.history = a.history[len(a.history)-maxHistoryLen:]
			}
			return &RunResult{
				Answer:       sb.String(),
				ToolCalls:    toolCalls,
				ModelVersion: modelVersion,
				TotalTokens:  totalTokens,
			}, nil
		}

		// 함수 호출 실행 후 결과를 FunctionResponse로 반환
		var respParts []*genai.Part
		for _, fc := range fcs {
			if fc.Name != toolName {
				continue
			}
			args := extractArgs(fc.Args)
			toolCalls = append(toolCalls, ToolCall{Name: fc.Name, Args: fc.Args})
			fmt.Fprintf(os.Stderr, "[도구 실행] football-cli %s\n", strings.Join(args, " "))

			jsonStr, err := RunCLIJSON(ctx, args)
			if err != nil {
				b, _ := json.Marshal(map[string]any{"error": err.Error(), "exit_code": -1})
				jsonStr = string(b)
			}

			var result map[string]any
			if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
				result = map[string]any{"raw": jsonStr}
			}

			respParts = append(respParts, &genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					Name:     toolName,
					Response: result,
				},
			})
		}

		toolContent := &genai.Content{
			Role:  "user",
			Parts: respParts,
		}
		contents = append(contents, toolContent)
		newTurn = append(newTurn, toolContent)
	}
	return nil, fmt.Errorf("에이전트 루프 최대 반복 횟수(%d) 초과", maxIterations)
}

// StructureResult는 Structure 호출의 결과: 스키마에 맞춘 원문 JSON 텍스트와
// 모델 버전/토큰 사용량
type StructureResult struct {
	RawJSON      string
	ModelVersion string
	TotalTokens  int32
}

// Structure는 도구 호출 없이(Function Calling 미사용) 응답 스키마를 지정해
// 모델이 JSON 형식으로만 답하도록 강제하는 별도 호출이다. Gemini API는 같은
// 요청에서 Tools(Function Calling)와 ResponseSchema를 함께 사용할 수 없으므로,
// 에이전트 루프(RunTraced)가 자연어 답변을 만든 뒤 이 메서드로 구조화한다.
func (a *Agent) Structure(ctx context.Context, prompt string, schema *genai.Schema) (*StructureResult, error) {
	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: prompt}}},
	}
	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   schema,
	}

	resp, err := a.client.Models.GenerateContent(ctx, modelName, contents, config)
	if err != nil {
		return nil, fmt.Errorf("구조화 응답 생성 실패: %w", err)
	}
	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("구조화 응답 후보 없음")
	}

	var totalTokens int32
	if resp.UsageMetadata != nil {
		totalTokens = resp.UsageMetadata.TotalTokenCount
	}

	return &StructureResult{
		RawJSON:      resp.Text(),
		ModelVersion: resp.ModelVersion,
		TotalTokens:  totalTokens,
	}, nil
}

func weekdayKo(t time.Time) string {
	return [...]string{"일", "월", "화", "수", "목", "금", "토"}[t.Weekday()] + "요일"
}

// extractArgs는 Gemini FunctionCall Args에서 "args" 배열 추출
func extractArgs(raw map[string]any) []string {
	argsRaw, ok := raw["args"]
	if !ok {
		return nil
	}
	argsSlice, ok := argsRaw.([]any)
	if !ok {
		return nil
	}
	args := make([]string, 0, len(argsSlice))
	for _, v := range argsSlice {
		if s, ok := v.(string); ok {
			args = append(args, s)
		}
	}
	return args
}
