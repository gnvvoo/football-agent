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

## 구조

```
football-agent/
├── main.go          # REPL 진입점
├── agent/
│   ├── agent.go     # 에이전트 루프
│   ├── tool.go      # Gemini Tool 정의
│   └── runner.go    # football-cli 실행기
├── go.mod
└── .env.example
```

## 요구사항

- Go 1.21+
- [football-cli](https://github.com/gnvvoo/football-cli) 바이너리
- Gemini API 키
- football-data.org API 키
