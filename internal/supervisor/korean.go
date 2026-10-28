package supervisor

import "os"

// korean is set when the baton window started this session in Korean
// (BATON_LANG=ko): the notices printed in the session window are then
// Korean, and point to the window rather than to commands.
var korean = os.Getenv("BATON_LANG") == "ko"

// tr returns the Korean form of a notice format (same verbs, same order).
func tr(format string) string {
	if !korean {
		return format
	}
	if k, ok := koreanNotices[format]; ok {
		return k
	}
	return format
}

var koreanNotices = map[string]string{
	"session ended as requested":                                                             "요청대로 세션을 끝냈습니다",
	"warning: account %q signs in with %q, not a Claude subscription":                        "주의: %q 계정은 Claude 구독이 아닌 %q 방식으로 로그인되어 있습니다",
	"warning: account %q was not signed in at the last check (sign in with: baton login %s)": "주의: %q 계정이 마지막 확인 때 로그인되어 있지 않았습니다 (baton 창에서 다시 로그인하세요: %s)",
	"Claude Code exited during startup (exit code %d); see its message above":                "Claude Code가 시작 중에 종료되었습니다 (종료 코드 %d). 위의 메시지를 확인하세요",
	"Claude Code never reported to baton on %q, so usage limits could not be detected there " +
		"(hooks disabled by settings or policy, or the folder trust prompt was not answered)": "%q 계정에서 Claude Code가 baton에 상태를 알리지 않아 한도를 감지할 수 없었습니다 " +
		"(설정이나 정책으로 hooks가 꺼져 있거나, 폴더 신뢰 질문에 답하지 않았습니다)",
	"Claude Code moved (app update?); now using %s":                                                     "Claude Code 위치가 바뀌었습니다 (앱 업데이트). 이제 %s를 사용합니다",
	"ignored a `baton switch` request made while the previous switch was in progress":                   "앞선 전환 중에 들어온 전환 요청은 무시했습니다",
	"account %q needs to sign in again: baton login %s":                                                 "%q 계정은 다시 로그인해야 합니다 (baton 창에서 다시 로그인: %s)",
	"account %q was blocked (%s); once that is resolved, run: baton enable %s":                          "%q 계정이 차단되었습니다 (%s). 해결한 뒤 baton 창에서 다시 켜세요: %s",
	"account %q is limited until %s; starting on it anyway as requested":                                "%q 계정은 %s까지 한도입니다. 요청대로 그래도 시작합니다",
	"skipping %q: marked blocked (%s); once resolved, run: baton enable %s":                             "%q 계정은 차단 표시가 있어 건너뜁니다 (%s). 해결한 뒤 다시 켜세요: %s",
	"automatic switching is off; staying on %q":                                                         "자동 전환이 꺼져 있어 %q 계정에 그대로 있습니다",
	"switching too often (at most %d per hour, %ds apart); continuing at %s (or `baton switch <name>`)": "전환이 너무 잦습니다 (시간당 최대 %d번, %d초 간격). %s에 이어 갑니다 (baton 창에서 바로 전환할 수도 있습니다)",
	"stopped":       "멈췄습니다",
	"staying on %q": "%q 계정에 그대로 있습니다",
	"cannot switch to %q (unknown, disabled, signed out or blocked)":                        "%q 계정으로 전환할 수 없습니다 (없거나, 꺼졌거나, 로그인이 필요하거나, 차단됨)",
	"no other account is available; staying on %q":                                          "쓸 수 있는 다른 계정이 없어 %q 계정에 그대로 있습니다",
	"no usable account: every account is disabled, signed out or blocked (see: baton list)": "쓸 수 있는 계정이 없습니다. 모든 계정이 꺼졌거나, 로그인이 필요하거나, 차단되었습니다 (baton 창의 계정 목록을 확인하세요)",
	"every account is limited; %q frees up first, at %s":                                    "모든 계정이 한도에 걸렸습니다. %q 계정이 %s에 가장 먼저 풀립니다",
	"every account is limited; waiting for %q at %s (Ctrl+C to stop, or `baton switch <name>`)": "모든 계정이 한도에 걸렸습니다. %q 계정이 풀리는 %s까지 기다렸다가 이어 갑니다 " +
		"(멈추려면 Ctrl+C, baton 창에서 바로 전환할 수도 있습니다)",
	"cannot switch to %q (unknown, disabled, signed out or blocked); still waiting":              "%q 계정으로 전환할 수 없어 계속 기다립니다",
	"this session is open in another claude window (pid %d); close it and run the command below": "이 대화가 다른 Claude Code 창(pid %d)에서 열려 있습니다. 그 창을 닫고 아래 방법으로 이어 가세요",
	"%s -> continuing on %q": "%s → %q 계정에서 이어 갑니다",
	"%q is in use elsewhere, so its folder trust could not be set; if claude asks, choose \"Yes, I trust this folder\"": "%q 계정이 다른 곳에서 사용 중이라 폴더 신뢰를 미리 설정하지 못했습니다. " +
		"Claude Code가 물으면 \"Yes, I trust this folder\"를 고르세요",
	"claude may ask whether you trust this folder on %q; choose \"Yes, I trust this folder\" to continue": "%q 계정에서 Claude Code가 이 폴더를 신뢰하는지 물을 수 있습니다. " +
		"계속하려면 \"Yes, I trust this folder\"를 고르세요",
	"stopped while waiting": "기다리는 중에 멈췄습니다",
	"%q was marked blocked (on hold, billing problem or revoked login); " +
		"baton will not use it until you fix that and run: baton enable %s": "%q 계정이 차단으로 표시되었습니다 (계정 정지·결제 문제·로그인 취소). " +
		"해결한 뒤 baton 창에서 다시 켜기 전까지는 쓰지 않습니다: %s",
	// describe
	"usage limit on %q (resets %s)":                 "%q 계정이 한도에 걸렸습니다 (%s에 풀림)",
	"usage limit on %q":                             "%q 계정이 한도에 걸렸습니다",
	"login failed on %q (fix with: baton login %s)": "%q 계정의 로그인이 만료되었습니다 (baton 창에서 다시 로그인: %s)",
	"switch requested from %q":                      "%q 계정에서 전환을 요청했습니다",
	"[answer needed] ":                              "[확인 필요] ",
	// confirm question and resume hint
	"%s. Continue this session on %q? [Y/n] ":            "%s. %q 계정에서 이어서 할까요? [Y/n] ",
	"continue later with:  baton run -- --resume \"%s\"": "나중에 baton 창의 ‘최근 세션’에서 이어서 하거나, 이 명령으로 이어 갈 수 있습니다:  baton run -- --resume \"%s\"",
}
