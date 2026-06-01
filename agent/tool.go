package agent

import "google.golang.org/genai"

const toolName = "football_cli"

// FootballCLITool은 Gemini에 전달할 football_cli 도구 정의
var FootballCLITool = &genai.Tool{
	FunctionDeclarations: []*genai.FunctionDeclaration{
		{
			Name: toolName,
			Description: `football-cli를 실행해 유럽 5대 리그(EPL, LaLiga, Bundesliga, SerieA, Ligue1) 축구 데이터를 조회한다.
항상 --json --no-color --quiet 플래그를 args에 포함해야 한다.

사용 가능한 커맨드:
  matches     경기 일정/결과 조회 (--league 필수)
  standings   리그 순위표 조회 (--league 필수)
  team-info   팀 정보 조회 (--team 필수)
  player-stats 선수 통계 조회 (--name 필수)
  predict     경기 예측 (--home, --away 필수)

종료 코드 의미:
  0: 성공
  2: 잘못된 인자 — args를 수정해 재시도
  3: 데이터 없음 — 날짜 범위나 팀명 등 조건을 바꿔 재시도
  4: API 실패
  5: 인증 실패

exit_code가 3이면 조건을 변경해 반드시 재시도한다.
stderr의 error.suggestions 필드가 있으면 팀/선수명을 제안값으로 교정해 재시도한다.`,
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"args": {
						Type:        genai.TypeArray,
						Description: `football-cli에 전달할 인자 배열. 예: ["matches", "--league", "EPL", "--json", "--no-color", "--quiet"]`,
						Items: &genai.Schema{
							Type: genai.TypeString,
						},
					},
				},
				Required: []string{"args"},
			},
		},
	},
}
