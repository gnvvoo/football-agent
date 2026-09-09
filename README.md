# football-agent

Google Gemini 2.5 Flash의 Tool Use 기능으로 football-cli를 도구 삼아 유럽 축구 데이터를 조회하는 AI 에이전트 CLI.

## 설치

```bash
# 의존성 설치 및 빌드
go mod tidy
go build -o football-agent .
```

## 환경변수 설정

```bash
cp .env.example .env
# .env 파일에 API 키 입력
```

| 변수 | 설명 | 기본값 |
|------|------|--------|
| `GEMINI_API_KEY` | Google Gemini API 키 | 필수 |
| `FOOTBALL_CLI_PATH` | football-cli 바이너리 경로 | `./football-cli` |
| `FOOTBALL_DATA_API_KEY` | football-data.org API 키 (football-cli용) | 필수 |

## 실행

```bash
./football-agent
```

```
football-agent 초기화 중...
준비 완료. 질문을 입력하세요 (빈 입력 2회 또는 Ctrl+C로 종료).

> 오늘 EPL 경기 일정 알려줘
[도구 실행] football-cli matches --league EPL --date 2026-05-13 --json --no-color --quiet

오늘 EPL 경기 일정입니다:
...
```

## 비대화형 모드 (the-half-space 백엔드 연동)

`--type` 플래그를 지정하면 REPL 대신 단발성으로 실행되어, 표준 출력에
JSON 오브젝트 한 줄만 출력하고 종료한다. the-half-space 백엔드의
`AgentService`가 이 모드로 프로세스를 실행해 결과를 파싱한다.

```bash
./football-agent --type preview --match 12345 --output json
./football-agent --type review --match 12345 --output json
```

- `--type`: `preview`(경기 전 분석) 또는 `review`(경기 후 리뷰). 필수.
- `--match`: 경기 ID (양의 정수). 필수.
- `--output`: `json`만 지원. 생략 가능하지만 지정 시 `json`이어야 한다.
- 초기화 로그, 도구 실행 로그 등 진행 상황은 모두 표준 에러(stderr)로만
  출력되며, 표준 출력(stdout)에는 아래 JSON 오브젝트 한 줄만 출력된다.
- 성공 시 종료 코드 `0`, 실패 시 0이 아닌 종료 코드와 함께 stderr에
  오류 메시지를 출력한다.

내부적으로 다음 두 단계로 동작한다:

1. 기존 에이전트 루프(`agent.Agent.RunTraced`)로 football-cli 도구를
   호출해가며 한국어 자연어 답변을 생성하고, 실행된 도구 호출 목록을
   함께 기록한다.
2. Gemini의 `responseSchema`(JSON 모드)를 사용하는 별도 호출
   (`agent.Agent.Structure`)로 1번의 답변을 `summary`/`body`/
   `reasoning`/(`preview`의 경우) `prediction` 필드를 갖는 JSON으로
   구조화한다. Gemini API는 Function Calling(Tools)과 응답 스키마를
   같은 요청에서 함께 사용할 수 없으므로 두 단계로 분리했다. 이 호출이
   실패하거나 결과가 유효한 JSON이 아니면 1번의 원본 답변을 `body`로,
   그 앞부분을 `summary`로 사용하는 방식으로 안전하게 대체(fallback)한다.

### 출력 예시 (preview)

```json
{
  "type": "preview",
  "matchId": 12345,
  "body": "## 첼시 vs 아스널 프리뷰\n...(마크다운 본문)...",
  "summary": "첼시가 최근 5경기 3승으로 상승세이며, 무패 행진 중인 아스널과 대등한 승부가 예상된다.",
  "reasoning": {
    "keyFactors": ["첼시 최근 5경기 3승 1무 1패", "아스널 원정 3연승", "직전 상대전적 2무 1패"],
    "notes": "부상자 명단은 반영되지 않음"
  },
  "prediction": {
    "homeWinPct": 45.0,
    "drawPct": 27.0,
    "awayWinPct": 28.0,
    "predictedScore": "2-1"
  },
  "toolsCalled": [
    {"name": "football_cli", "args": {"args": ["standings", "--league", "EPL", "--json", "--no-color", "--quiet"]}},
    {"name": "football_cli", "args": {"args": ["matches", "--league", "EPL", "--date", "2026-05-13", "--json", "--no-color", "--quiet"]}}
  ],
  "totalTokens": 4521,
  "modelVersion": "gemini-2.5-flash-002"
}
```

`review` 타입은 `prediction`이 `null`로 출력된다(백엔드는 `prediction`이
`null`이 아닐 때만 예측 정보를 저장한다).

## 구조

```
football-agent/
├── main.go              # REPL 진입점 + --type 비대화형 진입점
├── agent/
│   ├── agent.go         # 에이전트 루프 (Run/RunTraced), 구조화 호출(Structure)
│   ├── response.go      # AgentResponse 조립, 프롬프트/스키마 구성, JSON 파싱
│   ├── tool.go          # Gemini Tool 정의
│   └── runner.go        # football-cli 실행기
├── go.mod
└── .env.example
```

## 요구사항

- Go 1.21+
- [football-cli](https://github.com/gnvvoo/football-cli) 바이너리
- Gemini API 키
- football-data.org API 키
