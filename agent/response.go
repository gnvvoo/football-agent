package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/genai"
)

// AgentResponse는 the-half-space 백엔드의
// com.thehalfspace.dto.response.AgentResponse 레코드와 필드가 정확히
// 일치해야 하는 비대화형 모드의 최종 출력이다. 이 구조체가 표준 출력에
// 한 줄의 JSON 오브젝트로만 출력된다.
type AgentResponse struct {
	Type         string            `json:"type"`
	MatchID      int64             `json:"matchId"`
	Body         string            `json:"body"`
	Summary      string            `json:"summary"`
	Reasoning    map[string]any    `json:"reasoning"`
	Prediction   *PredictionResult `json:"prediction"`
	ToolsCalled  []ToolCall        `json:"toolsCalled"`
	TotalTokens  *int              `json:"totalTokens"`
	ModelVersion string            `json:"modelVersion"`
}

// PredictionResult는 AgentResponse.PredictionResult 레코드와 필드가
// 정확히 일치해야 한다.
type PredictionResult struct {
	HomeWinPct     *float64 `json:"homeWinPct"`
	DrawPct        *float64 `json:"drawPct"`
	AwayWinPct     *float64 `json:"awayWinPct"`
	PredictedScore string   `json:"predictedScore"`
}

// StructuredAnswer는 Structure() 호출로 얻은 모델의 JSON 응답을 담는
// 파싱 대상 구조체다.
type StructuredAnswer struct {
	Summary    string            `json:"summary"`
	Body       string            `json:"body"`
	Reasoning  map[string]any    `json:"reasoning"`
	Prediction *PredictionResult `json:"prediction,omitempty"`
}

const summaryFallbackMaxRunes = 200

var fencedJSONPattern = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// StructureSchema는 agentType(preview|review)에 맞는 genai 응답 스키마를
// 구성한다. preview는 prediction 필드를 필수로 요구하고, review는 요구하지
// 않는다(백엔드는 prediction이 null이면 저장을 건너뛴다).
func StructureSchema(agentType string) *genai.Schema {
	properties := map[string]*genai.Schema{
		"summary": {
			Type:        genai.TypeString,
			Description: "2~3문장으로 된 핵심 요약 (한국어)",
		},
		"body": {
			Type:        genai.TypeString,
			Description: "마크다운 형식의 본문 전체 (한국어)",
		},
		"reasoning": {
			Type:        genai.TypeObject,
			Description: "분석 근거",
			Properties: map[string]*genai.Schema{
				"keyFactors": {
					Type:        genai.TypeArray,
					Description: "분석에 사용한 핵심 근거 목록",
					Items:       &genai.Schema{Type: genai.TypeString},
				},
				"notes": {
					Type:        genai.TypeString,
					Description: "추가 설명 (선택)",
				},
			},
			Required: []string{"keyFactors"},
		},
	}
	required := []string{"summary", "body", "reasoning"}

	if agentType == "preview" {
		properties["prediction"] = &genai.Schema{
			Type:        genai.TypeObject,
			Description: "승부 예측",
			Properties: map[string]*genai.Schema{
				"homeWinPct":     {Type: genai.TypeNumber, Description: "홈팀 승리 확률 (0-100)"},
				"drawPct":        {Type: genai.TypeNumber, Description: "무승부 확률 (0-100)"},
				"awayWinPct":     {Type: genai.TypeNumber, Description: "원정팀 승리 확률 (0-100)"},
				"predictedScore": {Type: genai.TypeString, Description: "예상 스코어, 예: 2-1"},
			},
			Required: []string{"homeWinPct", "drawPct", "awayWinPct", "predictedScore"},
		}
		required = append(required, "prediction")
	}

	return &genai.Schema{
		Type:       genai.TypeObject,
		Properties: properties,
		Required:   required,
	}
}

// BuildPrompt는 agentType(preview|review)과 matchID로 에이전트 루프에
// 전달할 최초 자연어 프롬프트를 구성한다.
func BuildPrompt(agentType string, matchID int64) string {
	switch agentType {
	case "preview":
		return fmt.Sprintf(`아래 경기에 대한 경기 전 프리뷰를 작성하세요.

matchId: %d

football_cli 도구로 양 팀의 최근 폼, 리그 순위, 상대 전적 등 관련 데이터를 조회한 뒤,
다음 내용을 포함해 한국어로 작성하세요:
- 양 팀의 최근 폼과 리그 순위
- 이번 경기의 관전 포인트
- 승부 예측(홈 승/무/원정 승 확률)과 예상 스코어

데이터 조회가 끝나면 지금까지 조사한 내용을 바탕으로 위 항목을 모두 포함한
완결된 분석을 자연어로 답하세요.`, matchID)
	case "review":
		return fmt.Sprintf(`아래 경기에 대한 경기 후 리뷰를 작성하세요.

matchId: %d

football_cli 도구로 해당 경기의 최종 스코어, 주요 기록 등 관련 데이터를 조회한 뒤,
다음 내용을 포함해 한국어로 작성하세요:
- 최종 스코어와 경기 흐름
- 주요 사건(득점, 카드 등)
- 이 결과가 양 팀에 갖는 의미

데이터 조회가 끝나면 지금까지 조사한 내용을 바탕으로 위 항목을 모두 포함한
완결된 리뷰를 자연어로 답하세요.`, matchID)
	default:
		return fmt.Sprintf("matchId %d 에 대한 %s 콘텐츠를 한국어로 작성하세요.", matchID, agentType)
	}
}

// BuildStructurePrompt는 에이전트 루프가 만든 자연어 답변(draft)을
// StructureSchema에 맞춰 JSON으로 재구성하도록 요청하는 프롬프트를 만든다.
func BuildStructurePrompt(agentType string, matchID int64, draft string) string {
	var predictionNote string
	if agentType == "preview" {
		predictionNote = "\n- prediction: 홈팀 승률(homeWinPct), 무승부 확률(drawPct), 원정팀 승률(awayWinPct)을 0~100 사이 숫자로 제공하고, 세 값의 합이 100에 최대한 가깝도록 하세요. 예상 스코어는 predictedScore에 '2-1'과 같은 형식으로 제공하세요."
	}

	return fmt.Sprintf(`아래는 matchId %d 에 대한 %s 초안입니다. 이 내용을 바탕으로 지정된 JSON 스키마에 맞춰
구조화된 응답을 생성하세요. 새로운 사실을 추가하지 말고 초안의 내용만 정리하세요.

- summary: 2~3문장 핵심 요약
- body: 마크다운 형식의 본문 전체 (초안 내용을 다듬어 재구성)
- reasoning.keyFactors: 분석에 사용한 핵심 근거를 문자열 배열로 정리%s

--- 초안 ---
%s`, matchID, agentType, predictionNote, draft)
}

// ParseStructuredJSON은 Structure() 호출로 받은 원문 텍스트에서 JSON을
// 파싱한다. 모델이 순수 JSON 대신 코드펜스나 부가 설명을 덧붙인 경우에
// 대비해 여러 후보를 순서대로 시도하는 폴백을 포함한다.
func ParseStructuredJSON(raw string) (*StructuredAnswer, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("빈 응답")
	}

	candidates := []string{raw}
	if m := fencedJSONPattern.FindStringSubmatch(raw); m != nil {
		candidates = append([]string{m[1]}, candidates...)
	}
	if start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}"); start >= 0 && end > start {
		candidates = append(candidates, raw[start:end+1])
	}

	var lastErr error
	for _, c := range candidates {
		var sa StructuredAnswer
		if err := json.Unmarshal([]byte(c), &sa); err != nil {
			lastErr = err
			continue
		}
		if sa.Reasoning == nil {
			sa.Reasoning = map[string]any{}
		}
		return &sa, nil
	}
	return nil, fmt.Errorf("JSON 파싱 실패: %w", lastErr)
}

// BuildAgentResponse는 구조화 결과(structured, 파싱 실패 시 nil)와
// 폴백 원문(fallbackAnswer), 도구 호출 기록, 토큰 사용량, 모델 버전을 받아
// 백엔드 계약에 맞는 최종 AgentResponse를 조립한다.
func BuildAgentResponse(
	agentType string,
	matchID int64,
	structured *StructuredAnswer,
	fallbackAnswer string,
	toolCalls []ToolCall,
	totalTokens int32,
	modelVersion string,
) *AgentResponse {
	if toolCalls == nil {
		toolCalls = []ToolCall{}
	}
	if modelVersion == "" {
		modelVersion = modelName
	}

	var tokensPtr *int
	if totalTokens > 0 {
		t := int(totalTokens)
		tokensPtr = &t
	}

	fallbackBody := strings.TrimSpace(fallbackAnswer)

	if structured == nil {
		return &AgentResponse{
			Type:         agentType,
			MatchID:      matchID,
			Body:         fallbackBody,
			Summary:      truncateSummary(fallbackBody, summaryFallbackMaxRunes),
			Reasoning:    map[string]any{},
			Prediction:   nil,
			ToolsCalled:  toolCalls,
			TotalTokens:  tokensPtr,
			ModelVersion: modelVersion,
		}
	}

	body := strings.TrimSpace(structured.Body)
	if body == "" {
		body = fallbackBody
	}

	summary := strings.TrimSpace(structured.Summary)
	if summary == "" {
		summary = truncateSummary(body, summaryFallbackMaxRunes)
	}

	reasoning := structured.Reasoning
	if reasoning == nil {
		reasoning = map[string]any{}
	}

	var prediction *PredictionResult
	if agentType == "preview" {
		prediction = structured.Prediction
	}

	return &AgentResponse{
		Type:         agentType,
		MatchID:      matchID,
		Body:         body,
		Summary:      summary,
		Reasoning:    reasoning,
		Prediction:   prediction,
		ToolsCalled:  toolCalls,
		TotalTokens:  tokensPtr,
		ModelVersion: modelVersion,
	}
}

func truncateSummary(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}
