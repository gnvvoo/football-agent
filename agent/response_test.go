package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseStructuredJSON_PlainJSON(t *testing.T) {
	raw := `{"summary":"요약","body":"본문","reasoning":{"keyFactors":["a","b"]},"prediction":{"homeWinPct":55,"drawPct":25,"awayWinPct":20,"predictedScore":"2-1"}}`

	sa, err := ParseStructuredJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sa.Summary != "요약" || sa.Body != "본문" {
		t.Fatalf("unexpected fields: %+v", sa)
	}
	if sa.Prediction == nil || sa.Prediction.PredictedScore != "2-1" {
		t.Fatalf("unexpected prediction: %+v", sa.Prediction)
	}
}

func TestParseStructuredJSON_FencedCodeBlock(t *testing.T) {
	raw := "여기 결과입니다:\n```json\n{\"summary\":\"요약\",\"body\":\"본문\",\"reasoning\":{\"keyFactors\":[]}}\n```\n감사합니다."

	sa, err := ParseStructuredJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sa.Summary != "요약" || sa.Body != "본문" {
		t.Fatalf("unexpected fields: %+v", sa)
	}
}

func TestParseStructuredJSON_LooseBraces(t *testing.T) {
	raw := "some preamble text {\"summary\":\"S\",\"body\":\"B\",\"reasoning\":{}} trailing text"

	sa, err := ParseStructuredJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sa.Summary != "S" || sa.Body != "B" {
		t.Fatalf("unexpected fields: %+v", sa)
	}
	if sa.Reasoning == nil {
		t.Fatalf("expected reasoning to default to empty map, got nil")
	}
}

func TestParseStructuredJSON_Invalid(t *testing.T) {
	_, err := ParseStructuredJSON("이것은 JSON이 아닙니다")
	if err == nil {
		t.Fatal("expected error for non-JSON input, got nil")
	}
}

func TestParseStructuredJSON_Empty(t *testing.T) {
	_, err := ParseStructuredJSON("   ")
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
}

func TestBuildAgentResponse_WithStructuredPreview(t *testing.T) {
	structured := &StructuredAnswer{
		Summary:   "요약",
		Body:      "본문",
		Reasoning: map[string]any{"keyFactors": []any{"a"}},
		Prediction: &PredictionResult{
			HomeWinPct:     floatPtr(50),
			DrawPct:        floatPtr(30),
			AwayWinPct:     floatPtr(20),
			PredictedScore: "1-1",
		},
	}
	toolCalls := []ToolCall{{Name: "football_cli", Args: map[string]any{"args": []any{"matches", "--league", "EPL"}}}}

	resp := BuildAgentResponse("preview", 42, structured, "fallback", toolCalls, 1234, "gemini-2.5-flash-001")

	if resp.Type != "preview" || resp.MatchID != 42 {
		t.Fatalf("unexpected type/matchId: %+v", resp)
	}
	if resp.Body != "본문" || resp.Summary != "요약" {
		t.Fatalf("unexpected body/summary: %+v", resp)
	}
	if resp.Prediction == nil || resp.Prediction.PredictedScore != "1-1" {
		t.Fatalf("expected prediction to be carried through, got %+v", resp.Prediction)
	}
	if len(resp.ToolsCalled) != 1 || resp.ToolsCalled[0].Name != "football_cli" {
		t.Fatalf("unexpected toolsCalled: %+v", resp.ToolsCalled)
	}
	if resp.TotalTokens == nil || *resp.TotalTokens != 1234 {
		t.Fatalf("unexpected totalTokens: %+v", resp.TotalTokens)
	}
	if resp.ModelVersion != "gemini-2.5-flash-001" {
		t.Fatalf("unexpected modelVersion: %s", resp.ModelVersion)
	}

	// review 타입에서는 prediction이 있어도 무시되어야 한다(백엔드는
	// prediction != null일 때만 AiPrediction을 저장하므로, 리뷰에는
	// 예측을 실어 보내지 않는다).
	reviewResp := BuildAgentResponse("review", 42, structured, "fallback", nil, 0, "")
	if reviewResp.Prediction != nil {
		t.Fatalf("expected review response to omit prediction, got %+v", reviewResp.Prediction)
	}
	if reviewResp.ModelVersion == "" {
		t.Fatalf("expected modelVersion to fall back to a default, got empty string")
	}
	if reviewResp.TotalTokens != nil {
		t.Fatalf("expected nil totalTokens for zero tokens, got %v", *reviewResp.TotalTokens)
	}
	if reviewResp.ToolsCalled == nil {
		t.Fatalf("expected toolsCalled to default to an empty slice, not nil")
	}
}

func TestBuildAgentResponse_FallbackWhenStructuredIsNil(t *testing.T) {
	resp := BuildAgentResponse("review", 7, nil, "  원본 답변 내용  ", nil, 0, "")

	if resp.Body != "원본 답변 내용" {
		t.Fatalf("expected trimmed fallback body, got %q", resp.Body)
	}
	if resp.Summary == "" {
		t.Fatalf("expected a non-empty fallback summary")
	}
	if resp.Reasoning == nil {
		t.Fatalf("expected reasoning to default to an empty map")
	}
	if resp.Prediction != nil {
		t.Fatalf("expected nil prediction in fallback, got %+v", resp.Prediction)
	}
}

func TestBuildAgentResponse_JSONFieldNamesMatchBackendContract(t *testing.T) {
	resp := BuildAgentResponse("preview", 1, &StructuredAnswer{
		Summary:   "S",
		Body:      "B",
		Reasoning: map[string]any{},
		Prediction: &PredictionResult{
			HomeWinPct: floatPtr(1), DrawPct: floatPtr(1), AwayWinPct: floatPtr(1), PredictedScore: "0-0",
		},
	}, "", nil, 10, "gemini-2.5-flash")

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	s := string(b)

	// the-half-space backend's AgentResponse record expects exactly these
	// camelCase field names.
	for _, field := range []string{
		`"type"`, `"matchId"`, `"body"`, `"summary"`, `"reasoning"`,
		`"prediction"`, `"toolsCalled"`, `"totalTokens"`, `"modelVersion"`,
		`"homeWinPct"`, `"drawPct"`, `"awayWinPct"`, `"predictedScore"`,
	} {
		if !strings.Contains(s, field) {
			t.Fatalf("expected field %s in JSON output, got: %s", field, s)
		}
	}
}

func TestStructureSchema_PreviewRequiresPrediction(t *testing.T) {
	schema := StructureSchema("preview")
	if _, ok := schema.Properties["prediction"]; !ok {
		t.Fatal("expected preview schema to define a prediction property")
	}
	if !containsString(schema.Required, "prediction") {
		t.Fatalf("expected preview schema to require prediction, got required=%v", schema.Required)
	}
}

func TestStructureSchema_ReviewHasNoPrediction(t *testing.T) {
	schema := StructureSchema("review")
	if _, ok := schema.Properties["prediction"]; ok {
		t.Fatal("expected review schema not to define a prediction property")
	}
	if containsString(schema.Required, "prediction") {
		t.Fatalf("expected review schema not to require prediction, got required=%v", schema.Required)
	}
}

func floatPtr(f float64) *float64 { return &f }

func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}
