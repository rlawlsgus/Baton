'use strict';
// baton GUI. Plain DOM, no framework; every string from the server is inserted
// as text, never as HTML.

const T = {
  ko: {
    tab_sessions: '세션', tab_accounts: '계정', tab_settings: '설정', tab_help: '도움말',
    claude_desktop: 'Claude 데스크톱 앱의 Claude Code {v}', claude_cli: 'Claude Code {v}', claude_checking: 'Claude Code 확인 중…',
    claude_missing: 'Claude Code 없음',
    mode_auto: '자동 전환', mode_confirm: '전환 전 확인', mode_off: '자동 전환 꺼짐', mode_unset: '전환 방식 미설정',
    lang_label: '언어',
    // sessions
    sessions_title: '실행 중인 세션', sessions_sub: '세션은 각각 별도의 창에서 실행됩니다. 이 창을 닫아도 세션은 계속됩니다.',
    new_session: '새 세션 시작', no_sessions_title: '실행 중인 세션이 없습니다',
    no_sessions_body: '작업할 폴더를 골라 Claude Code를 시작하세요. 한도에 걸리면 baton이 같은 대화를 다음 계정에서 이어 갑니다.',
    need_account_first: '먼저 계정을 추가하세요.', go_accounts: '계정 추가하러 가기',
    no_usable: '사용할 수 있는 계정이 없습니다. 계정 탭에서 로그인하거나 사용을 켜세요.', go_accounts_tab: '계정 탭으로',
    run_running: '실행 중', run_stopping: '현재 응답이 끝나면 전환', run_switching: '계정 전환 중', run_waiting: '기다리는 중',
    waiting_hint: '모든 계정이 한도에 걸렸거나 전환이 너무 잦아 기다리는 중입니다. 다시 시작하는 시각은 세션 창에 나옵니다.',
    waiting_first: '처음 쓸 계정이 풀리기를 기다리는 중', run_on_none: '계정 기다리는 중',
    run_asking: '세션 창에서 확인을 기다리는 중', run_ending: '세션을 끝내는 중', run_on: '{a} 계정', run_since: '{t} 시작',
    asking_stop_hint: '세션을 끝내려면 세션 창에서 Ctrl+C를 누르세요 (n은 지금 계정에서 계속합니다).',
    ended_console: '끝난 세션 창이 아직 열려 있습니다. 그 창에서 Enter를 누르면 닫힙니다.', retry_now: '{a} 계정으로 지금 다시 시도',
    switch_btn: '다른 계정으로', switch_auto: '다음 계정으로 (자동 선택)', switch_to: '{a}(으)로', open_folder: '폴더 열기', open_log: '기록',
    switch_asked: '전환을 요청했습니다. 현재 응답이 끝나면(최대 1분) 전환됩니다.', asking_hint: '세션 창에 나타난 질문에 답하세요.',
    switch_asked_confirm: '전환을 요청했습니다. 현재 응답이 끝나면 세션 창에서 전환할지 묻습니다. Enter로 확인하세요.',
    recent_title: '최근 전환', recent_none: '아직 전환 기록이 없습니다.',
    reason_usage_limit: '한도', reason_authentication_failed: '로그인 실패', reason_manual: '직접 전환',
    job_run: '세션 창을 여는 중…',
    stop_btn: '세션 끝내기', stop_title: '이 세션을 끝낼까요?', stop_body: 'Claude Code를 안전하게 멈춥니다. 대화는 저장되어 나중에 ‘최근 세션’에서 이어서 할 수 있습니다. 진행 중인 답변이 있으면 최대 10초 기다리고, 그래도 끝나지 않으면 중단합니다.',
    stop_go: '끝내기', stop_asked: '세션을 끝내도록 요청했습니다.', switch_confirm_hint: '확인 모드: 세션 창에서 Enter로 전환을 확인하세요.',
    recent_sessions: '최근 세션', recent_sessions_none: '끝난 세션이 여기에 표시됩니다.', continue_btn: '이어서 하기',
    // session details
    cl_busy: 'Claude 작업 중', cl_idle: '입력 기다리는 중', cl_waiting: '세션 창에서 확인 필요',
    cl_waiting_hint: '세션 창에서 답해야 계속됩니다: {w}', ctx: '컨텍스트 {p}% 사용',
    ch_none: '바뀐 파일 없음', ch_files: '파일 {n}개', ch_files1: '파일 1개', ch_btn: '변경사항', ch_title: '{f}의 변경사항',
    ch_norepo: 'git 저장소가 아닌 폴더라 변경사항을 볼 수 없습니다.', ch_nogit: 'git을 찾지 못했습니다. Git for Windows를 설치하면 변경사항을 볼 수 있습니다.',
    ch_note: '마지막 커밋 이후 이 폴더에서 바뀐 파일입니다(커밋하지 않은 것, 새 파일 포함).', ch_loading: '변경사항을 읽는 중…', ch_error: 'git이 변경사항을 읽지 못했습니다: {e}',
    gs_modified: '수정', gs_added: '추가', gs_deleted: '삭제', gs_renamed: '이름 바뀜', gs_untracked: '새 파일', gs_conflict: '충돌',
    rc_on: '리모트 컨트롤', details_more: '자세히', details_less: '접기', d_path: '폴더', d_started: '시작', d_changes: '변경', ctx_short: '컨텍스트', rc_where: '휴대폰의 Claude 앱이나 claude.ai/code에서 {a} 계정으로 로그인하면 이 세션이 보입니다. 계정이 바뀌면 바뀐 계정에서 보입니다.',
    start_rc: '리모트 컨트롤 켜기', start_rc_hint: '휴대폰의 Claude 앱이나 claude.ai/code에서 이 세션을 보고 이어서 쓸 수 있습니다. 세션이 실행 중인 계정으로 로그인해야 보이고, 계정이 바뀌면 그 계정에서 다시 켜집니다.',
    set_rc: '새 세션에서 리모트 컨트롤 켜기를 기본으로',
    show_window: '창 보기', show_window_tip: '이 세션의 Claude Code 창을 앞으로 가져옵니다',
    cut_title: '끊긴 세션 {n}개', cut_title1: '끊긴 세션 1개',
    cut_body: 'PC가 다시 시작되었거나 세션이 예기치 않게 멈췄습니다. 다시 열면 같은 대화를 같은 설정으로 이어 가고, 작업 중이던 세션은 하던 일을 계속합니다.',
    cut_restart: 'PC 다시 시작', cut_stopped: '예기치 않게 멈춤', cut_working: '작업 중이었음',
    restore_btn: '다시 열기', restore_all: '모두 다시 열기', dismiss_btn: '지우기', dismiss_all: '모두 지우기',
    restore_started: '세션을 다시 여는 중입니다. 별도의 창에서 열립니다.', err_not_cut_off: '이 세션은 이미 다시 열렸거나 목록에서 지워졌습니다.',
    set_rules: '폴더별 계정', set_rules_hint: '규칙이 있는 폴더(와 그 안의 폴더)의 세션은 고른 계정에서만 시작하고 전환합니다. 계정을 하나만 고르면 그 세션은 다른 계정으로 옮기지 않습니다. 회사 저장소를 회사 계정에만 두고 싶을 때처럼 쓰세요.',
    rules_none: '규칙이 없습니다. 모든 폴더의 세션이 모든 계정을 씁니다.', rule_add: '규칙 추가', rule_edit: '바꾸기', rule_remove: '지우기',
    rule_title: '폴더별 계정 규칙', rule_folder: '폴더', rule_accounts: '이 폴더에서 쓸 계정', rule_save: '저장', rule_missing: '없는 계정: {a}',
    rule_saved: '규칙을 저장했습니다.', rule_removed: '규칙을 지웠습니다.', rule_note: '이미 실행 중인 세션에는 더 엄격해지는 변경만 적용되고, 나머지는 다음 세션부터 적용됩니다.',
    err_rule_empty: '계정을 하나 이상 고르세요.', err_rule_account: '이 폴더의 규칙이 허용하지 않는 계정입니다.', start_rule: '이 폴더는 규칙에 따라 {a} 계정만 씁니다.',
    err_no_window: '세션 창을 찾지 못했습니다. 이미 닫혔을 수 있습니다.', err_focus: '세션 창을 앞으로 가져오지 못했습니다. 작업 표시줄에서 여세요.',
    set_notify: 'Windows 알림', set_notify_hint: '세션 창을 보고 있지 않을 때 Windows 알림으로 알려 줍니다. 알림을 누르면 그 세션 창이 앞으로 나옵니다.',
    notify_all: '모두: 계정 전환과 한도, Claude가 답을 기다릴 때, 30초 넘게 걸린 응답이 끝났을 때',
    notify_important: '중요한 것만: 응답이 끝났을 때는 빼고', notify_off: '끄기', notify_test: '알림 보내 보기',
    notify_sent: '알림을 보냈습니다. 보이지 않으면 Windows 설정 → 시스템 → 알림에서 baton과 방해 금지를 확인하세요.',
    set_awake: '세션이 작업하는 동안 PC가 절전 모드로 들어가지 않게 하기',
    set_awake_hint: 'Claude가 작업하거나 세션이 한도가 풀리기를 기다리는 동안에만 그렇습니다. 화면은 꺼질 수 있고, 직접 절전하거나 노트북 덮개를 닫으면 잠듭니다.',
    trust_q: '이 폴더를 신뢰하나요?', trust_body: 'Claude Code가 이 폴더의 파일을 읽고 고치고 명령을 실행할 수 있게 됩니다. 믿을 수 있는 폴더일 때만 체크하세요.',
    trust_ok: '이 폴더를 신뢰합니다', trust_needed: '신뢰하는 폴더에서만 시작할 수 있습니다. 체크한 뒤 다시 누르세요.',
    folder_bad: '있는 폴더를 고르세요 (내 PC의 폴더, 예: C:\\Users\\…).', folder_long: '경로가 너무 깁니다. 더 짧은 경로의 폴더를 고르세요.',
    first_start: '처음 쓰는 계정은 준비에 1~2분 걸릴 수 있습니다.',
    open_elsewhere: '이 대화는 다른 창(데스크톱 앱 등)에서 열려 있습니다. 그쪽을 닫고 다시 시도하세요.',
    claude_app_setup: 'Claude 데스크톱 앱이 아직 Claude Code를 준비하지 않았습니다. 데스크톱 앱에서 Code 탭을 한 번 열면 준비됩니다.',
    claude_too_old: 'Claude Code 버전이 너무 낮습니다. Claude 데스크톱 앱을 업데이트하세요 (도움말 → 업데이트 확인).',
    claude_cli_old: '설치된 Claude Code CLI가 너무 오래되었습니다. PowerShell에서 claude update를 실행하세요.',
    claude_cli_old_app: 'CLI를 따로 쓰지 않는다면 Claude 데스크톱 앱을 최신으로 업데이트하고 Code 탭을 한 번 여는 것으로도 됩니다. 그러면 baton이 앱에 들어 있는 Claude Code를 씁니다.',
    claude_path_old: '설정한 claude_path의 Claude Code가 너무 오래되었습니다. 그 Claude Code를 업데이트하거나, 명령줄에서 baton config claude_path auto로 자동 찾기로 돌아가세요.',
    claude_old_cli_skipped: '설치된 Claude Code CLI {v}는 너무 오래되어 쓰지 않고, Claude 데스크톱 앱의 Claude Code를 씁니다. CLI를 업데이트하려면 PowerShell에서 claude update를 실행하세요.',
    claude_bundle_off: 'Claude 데스크톱 앱에 들어 있는 Claude Code 사용이 꺼져 있습니다. 설정 → 고급 설정에서 다시 켜세요.',
    recheck: '다시 확인', elevated: 'baton이 관리자 권한으로 실행 중입니다. 세션도 관리자 권한으로 실행되니, 창을 닫고 일반 권한으로 다시 여세요.',
    claude_custom_cli: '다른 위치의 claude.exe를 쓰려면 명령줄에서: baton config claude_path <경로>',
    // start dialog
    start_title: '새 세션 시작', start_folder: '작업할 폴더', browse: '찾아보기…', recent_folders: '최근 폴더',
    start_account: '시작할 계정', account_auto: '자동 (사용 가능한 계정 중 우선순위대로)',
    start_conv: '대화', conv_new: '새 대화', conv_continue: '이 폴더의 마지막 대화 이어하기', conv_pick: '목록에서 고르기',
    conv_none: '이 폴더에서 한 대화가 없습니다.', conv_loading: '대화 목록을 불러오는 중…', conv_untitled: '(제목 없음)', conv_latest: '이어 갈 대화: {p}',
    conv_where_home: '평소 Claude Code (데스크톱 앱 포함)', conv_where: '{a} 계정에 저장',
    start_go: '시작', cancel: '취소', close: '닫기',
    start_done: 'Claude Code가 새 창에서 열립니다. 작업은 그 창에서 하세요.', choose_folder_first: '폴더를 고르세요.',
    pick_conv_first: '이어 갈 대화를 고르세요.',
    // bring in a conversation
    import_btn: '기존 대화 가져오기', import_title: 'Claude Code 대화 가져오기',
    import_sub: 'Claude 데스크톱 앱이나 터미널에서 하던 대화를 baton에서 이어 갑니다. 대화를 고른 뒤 시작할 계정을 정합니다.',
    import_search: '제목, 첫 질문, 폴더로 찾기', import_none: '가져올 대화가 없습니다. Claude Code에서 대화하면 여기에 나옵니다.',
    import_no_match: '찾는 대화가 없습니다.', import_go: '이 대화 이어 가기',
    src_desktop: 'Claude 데스크톱 앱', src_cli: '터미널', src_vscode: 'VS Code', src_other: 'Claude Code',
    conv_open: '다른 곳에서 열려 있음', conv_gone: '폴더 없음',
    conv_open_hint: '이 대화는 다른 Claude Code 창에서 열려 있어 사본으로만 이어 갈 수 있습니다. 사본은 지금까지의 내용을 이어받은 새 대화로, 원래 대화와 따로 진행됩니다.',
    held_desktop: 'Claude 데스크톱 앱', held_cli: '터미널의 Claude Code(CLI)', held_vscode: 'VS Code', held_other: '다른 Claude Code 창',
    held_title: '이 대화는 지금 {app}에서 열려 있습니다',
    held_close_desktop: '같은 대화로 이어 가려면 먼저 Claude 데스크톱 앱을 완전히 종료하세요(작업 표시줄 트레이 아이콘에서도 종료). 그다음 아래의 다시 확인을 누르세요.',
    held_close_cli: '같은 대화로 이어 가려면 먼저 그 터미널에서 Claude Code를 종료하세요(/exit, 또는 Ctrl+C 두 번). 그다음 아래의 다시 확인을 누르세요.',
    held_close_other: '같은 대화로 이어 가려면 먼저 그 창에서 이 대화를 닫으세요. 그다음 아래의 다시 확인을 누르세요.',
    held_copy: '그대로 계속하면 사본으로 가져옵니다. 사본은 지금까지의 내용을 이어받은 새 대화이고, 원래 대화와 따로 진행되어 서로 동기화되지 않습니다.',
    held_recheck: '다시 확인', import_go_copy: '사본으로 이어 가기', held_still: '아직 열려 있습니다.', held_free: '이제 닫혔습니다. 같은 대화로 이어 갈 수 있습니다.',
    conv_gone_hint: '이 대화를 하던 폴더가 없어져서 이어 갈 수 없습니다.',
    conv_unusable: '쓸 수 없는 폴더',
    conv_bad_hint: 'baton은 내 PC의 폴더(C:\\… 등)이면서 경로가 248자보다 짧은 곳에서만 세션을 시작하므로, 이 대화는 여기서 이어 갈 수 없습니다.',
    import_more: '최근 대화 200개만 보입니다. 더 오래된 대화는 명령줄에서 baton sessions --max 1000으로 찾아 baton resume <id>로 이어 가세요.',
    copy_ok: '사본으로 이어 가기 (원래 대화는 그대로 둡니다)',
    // accounts
    accounts_title: '계정', accounts_sub: '계정마다 처음 한 번만 로그인(2차 인증 포함)하면 로그인이 유지됩니다. 전환할 때 다시 로그인하지 않습니다.',
    add_account: '계정 추가', check_logins: '로그인 상태 확인', checking: '확인 중…',
    no_accounts_title: '아직 계정이 없습니다',
    no_accounts_body: '사용할 Claude 계정을 하나씩 추가하세요. 계정마다 브라우저에서 한 번 로그인합니다.',
    st_ok: '사용 가능', st_limited: '{t}까지 한도', st_needs_login: '다시 로그인 필요', st_signed_out: '로그인 안 됨',
    st_blocked: '차단됨', st_off: '꺼짐', st_dup: '같은 계정 중복', st_signing: '로그인 진행 중…',
    kept: '로그인됨', checked_at: '{t} 확인', not_checked: '아직 확인 안 함',
    use_5h: '5시간 한도', use_7d: '주간 한도', use_resets: '{t} 초기화', use_asof: '{t} 기준',
    use_unknown: '사용량은 이 계정으로 baton 세션을 쓰면 표시됩니다.',
    login_again: '다시 로그인', enabled_label: '자동 전환에 사용', use_short: '사용', up: '위로', down: '아래로', remove: '삭제',
    unblock: '해결됨 — 다시 사용',
    order_hint: '위에 있는 계정부터 사용합니다. Claude 데스크톱 앱에서 쓰는 계정을 맨 아래에 두면 데스크톱 채팅의 한도를 아낄 수 있습니다.',
    blocked_hint: '계정 정지·결제 문제·로그인 취소로 차단되었습니다. 해결했다면 ‘해결됨 — 다시 사용’을 누르세요(다시 로그인할 필요 없음). 로그인이 취소된 경우에만 다시 로그인하세요.',
    dup_hint: '이미 등록된 계정과 같은 Claude 계정으로 로그인되어 꺼졌습니다. 다른 계정으로 다시 로그인하세요.',
    // add dialog
    add_title: '계정 추가', add_name: '계정 이름 (baton에서만 쓰는 이름)', add_name_hint: '영문 소문자·숫자·- 만 (예: personal, work)',
    add_email: '이 계정의 이메일 (권장)', add_email_hint: '로그인 화면에 미리 채워져, 엉뚱한 계정으로 로그인하는 일을 줄여 줍니다.',
    add_private: '브라우저가 이미 다른 Claude 계정으로 로그인되어 있어요 (InPrivate 창에서 로그인)',
    add_go: '로그인 창 열기',
    signin_title: '‘{a}’ 로그인 진행 중', signin_close_hint: '로그인 창(검은 창)을 닫으면 로그인이 취소됩니다. 계정은 ‘로그인 안 됨’으로 목록에 남고, ‘다시 로그인’으로 이어서 할 수 있습니다.',
    signin_steps_normal: ['검은 로그인 창과 함께 브라우저에 Claude 로그인 화면이 열립니다.', '로그인하고 2단계 인증을 마칩니다. 이 계정은 이번 한 번만 하면 됩니다.', '끝나면 이 화면에 결과가 나타납니다.'],
    signin_steps_private: ['Claude 로그인 화면이 InPrivate 창에서 열립니다. 평소 브라우저의 로그인은 그대로입니다.', '그 창에서 로그인과 2단계 인증을 마칩니다. 끝나면 이 화면에 결과가 나타납니다.', '평소 브라우저가 대신 열렸거나(그 탭은 쓰지 말고 닫으세요) InPrivate 창을 닫았다면: 검은 로그인 창의 https://claude… 주소를 마우스로 끌어 선택해 Ctrl+C로 복사하고 아래 버튼을 누르세요.'],
    open_private: 'InPrivate 창에서 다시 열기', private_opened: 'InPrivate 창을 열었습니다. 그 창에서 로그인하세요.',
    private_no_url: '복사한 주소가 없습니다. 검은 창의 https://claude… 주소를 선택해 Ctrl+C로 복사한 뒤 다시 누르세요.',
    res_ok: '로그인 완료: {e}', res_ok_plan: '로그인 완료: {e} ({p})', res_still_off: '이 계정은 꺼 둔 상태라 아직 사용하지 않습니다. 쓰려면 목록 오른쪽의 ‘사용’ 스위치를 켜세요.',
    res_unfinished: '‘{a}’ 로그인 창이 결과 없이 닫혔습니다. 다시 시도하세요.',
    res_api: '이 계정은 Claude 구독이 아닌 방식(API 키 등)으로 로그인되었습니다. 구독 계정으로 다시 로그인하세요.',
    res_duplicate: '‘{a}’은(는) 이미 추가한 ‘{d}’과(와) 같은 Claude 계정({e})이라 꺼 두었습니다. 다른 계정으로 다시 로그인하세요.',
    res_login_failed: '‘{a}’ 로그인이 끝나지 않았습니다. 다시 시도하세요.', res_not_signed_in: '‘{a}’이(가) 로그인되지 않았습니다. 다시 시도하세요.',
    res_busy: '‘{a}’ 계정으로 실행 중인 세션이 있어 다시 로그인할 수 없습니다. 세션을 끝낸 뒤 시도하세요.',
    res_error: '‘{a}’ 로그인 중 문제가 생겼습니다: {m}', retry_private: 'InPrivate 창으로 다시 로그인', retry: '다시 시도', dismiss: '닫기',
    in_use: '사용 중', in_use_hint: '이 계정으로 세션이 실행 중입니다.',
    add_how: '로그인 창(검은 창)이 열리고 브라우저에서 Claude 로그인 화면이 뜹니다. 로그인과 2차 인증을 마치면 이 목록에 나타납니다. 브라우저가 자동으로 열리지 않으면 검은 창에 나온 주소를 브라우저에 붙여 넣으세요.',
    add_tip_second: '브라우저가 이미 다른 Claude 계정으로 로그인되어 있으면 그 계정으로 연결됩니다. 로그인하기 전에 브라우저에서 claude.ai를 로그아웃하거나, 검은 창에 나온 주소를 InPrivate(시크릿) 창에 붙여 넣어 로그인하세요.',
    add_started: '로그인 창을 열었습니다. 브라우저에서 로그인과 2차 인증을 마치세요.',
    login_started: '로그인 창을 열었습니다.',
    bad_name: '이름은 영문 소문자·숫자·- 로 1~32자, 첫 글자는 문자나 숫자여야 합니다.',
    // remove
    remove_title: '‘{a}’ 계정 삭제', remove_body: 'baton에서 이 계정을 로그아웃하고 지웁니다. 다시 쓰려면 로그인(2차 인증 포함)을 처음부터 해야 합니다. 이 계정에서 시작한 대화는 지우지 않고 평소 Claude Code 폴더로 옮겨 보존합니다. Claude 계정 자체는 그대로입니다.',
    remove_go: '삭제', removed: '‘{a}’ 계정을 삭제했습니다.',
    // notice / onboarding
    notice_title: '시작하기 전에 꼭 읽어 주세요',
    notice_points: [
      '본인 계정 여러 개에 미리 로그인해 두는 것 자체는 이 안내의 대상이 아닙니다. 아래는 한도에 걸린 뒤 다른 계정으로 이어서 쓰는 것에 관한 안내입니다.',
      '사용 중인 계정이 한도에 걸리면 baton은 Claude Code를 멈추고 같은 대화를 다른 계정에서 이어 갑니다.',
      '본인 소유의 계정만 추가하세요. 다른 사람과 계정을 공유하는 것은 Anthropic 약관 위반입니다.',
      'Anthropic 이용 정책은 허가 없이 제품의 제한을 의도적으로 우회하거나 여러 계정으로 안전장치를 피하는 것을 금지합니다. 한도에 걸린 뒤 다른 계정으로 이어 쓰는 것이 이에 해당한다고 판단될 수 있고, Anthropic은 계정을 예고 없이 정지할 수 있습니다. 이를 허용한다는 Anthropic의 공식 입장은 없습니다. 본인 판단과 책임으로 사용하세요.',
      '전환할 때마다 다음 계정이 대화 전체를 다시 읽으므로 그 계정의 한도를 그만큼 사용합니다.',
      'baton은 로그인 정보를 읽거나 저장하지 않습니다. 로그인과 갱신은 Claude Code가 직접 합니다.',
    ],
    notice_ack: '읽었으며, baton이 계정을 전환하는 데 동의합니다.',
    choose_mode: '전환 방식',
    mode_auto_desc: '묻지 않고 바로 다음 계정에서 이어 갑니다.',
    mode_confirm_desc: '전환할 때마다 세션 창에서 물어봅니다. 세션 창에서 Enter로 확인합니다. (권장)',
    mode_off_desc: '자동으로 바꾸지 않습니다. 필요할 때 이 화면의 버튼으로 전환합니다.',
    ob_welcome_title: 'baton에 오신 것을 환영합니다',
    ob_welcome_points: [
      '계정마다 처음 한 번만 로그인하면(2차 인증 포함) 로그인이 계속 유지됩니다.',
      '폴더를 골라 세션을 시작하면 Claude Code가 별도의 창에서 열립니다.',
      '한도에 걸리면 같은 대화가 다음 계정에서 자동으로 이어집니다.',
    ],
    ob_claude_title: '사용할 Claude Code',
    ob_claude_desktop: 'Claude 데스크톱 앱에 들어 있는 Claude Code를 사용합니다 (버전 {v}). 따로 설치할 것이 없습니다.',
    ob_claude_cli: '설치된 Claude Code를 사용합니다 (버전 {v}).',
    ob_claude_missing: 'Claude Code를 찾지 못했습니다. Claude 데스크톱 앱(claude.ai/download)을 설치하면 함께 들어 있는 Claude Code를 사용합니다.',
    ob_claude_wait: 'Claude Code를 찾는 중입니다…',
    next: '다음', back: '이전', finish: '시작하기', save: '저장', apply: '적용',
    ack_needed: '동의에 체크해야 자동 전환이나 확인 후 전환을 켤 수 있습니다.',
    // settings
    settings_title: '설정', set_mode: '계정 전환 방식', set_mode_hint: '확인 질문은 해당 세션 창에 나타납니다.',
    set_exhausted: '모든 계정이 한도에 걸리면', ex_wait: '가장 먼저 풀리는 계정을 기다렸다가 이어 가기', ex_stop: '멈추고 이어 하는 방법 알려 주기',
    set_advanced: '고급 설정', set_cooldown: '전환 사이 최소 간격(초)', set_max: '시간당 최대 전환 횟수', set_retry: '해제 시각을 모를 때 다시 시도할 때까지(분)',
    set_prompt: '다음 계정에 보낼 이어 가기 안내 문장', set_share: '평소 Claude Code 설정(skills·CLAUDE.md 등)을 모든 계정에 공유',
    set_claude: 'Claude Code 위치', set_claude_auto: '자동으로 찾기',
    set_claude_which: '사용할 Claude Code', claude_which_auto: '자동 (설치된 CLI가 있으면 CLI, 없거나 오래되었으면 데스크톱 앱의 것)',
    claude_which_desktop: '항상 Claude 데스크톱 앱에 들어 있는 것',
    claude_which_note: '어느 쪽이든 세션은 별도의 Claude Code 창(터미널 화면)에서 열립니다. 데스크톱 앱 화면 안에서 열 수는 없습니다.', set_claude_bundle: 'Claude 데스크톱 앱에 들어 있는 Claude Code도 사용',
    set_claude_custom: '직접 지정 (claude.exe 전체 경로)', saved: '저장했습니다.', set_lang: '화면 언어', lang_auto: 'Windows 설정 따르기',
    // help
    help_title: 'baton은 이렇게 동작합니다',
    help_points: [
      '계정마다 별도의 Claude Code 프로필이 있고, 로그인은 Claude Code가 직접 관리합니다.',
      '세션은 Claude Code 창으로 열립니다. 평소처럼 그 창에서 작업하세요.',
      '사용 중인 계정이 한도에 걸리면 baton이 Claude Code를 안전하게 멈추고, 같은 대화를 다음 계정에서 다시 엽니다(약 5~10초).',
      '전환되어도 대화 내용과 작업 폴더는 그대로입니다. 실행 중이던 백그라운드 명령은 끝나고, 다시 실행되지 않습니다.',
      'Claude 데스크톱 앱의 로그인은 바뀌지 않습니다. baton 계정들은 데스크톱 앱과 따로 로그인되어 있습니다.',
    ],
    notice_short: '사용 전 주의 사항', diag_title: '진단', diag_run: '진단 실행', diag_running: '진단 중…',
    open_data: '데이터 폴더 열기', cli_title: '명령줄(CLI)', cli_body: '같은 폴더의 baton.exe로 모든 기능을 PowerShell에서도 쓸 수 있습니다. 그 폴더에서 예: .\\baton.exe list, .\\baton.exe run',
    version: '버전 {v}',
    install_title: '시작 메뉴에 설치', install_body: '지금은 압축을 푼 폴더에서 실행 중입니다. 설치하면 시작 메뉴에서 baton을 열 수 있고, 작업 표시줄에 고정한 아이콘도 계속 동작합니다. 관리자 권한은 필요 없습니다.',
    install_go: '설치하고 다시 열기', installed_note: '시작 메뉴에 설치되어 있습니다.', install_failed: '설치하지 못했습니다: {e}',
    uninstall_title: 'baton 제거', uninstall_body: 'baton이 모든 계정을 로그아웃하고 baton의 데이터(계정별 로그인 정보)를 지웁니다. baton에서 시작한 대화는 지우기 전에 평소 Claude Code 폴더(%USERPROFILE%\\.claude)로 옮겨 보존하므로, claude --resume으로 다시 열 수 있습니다. Claude 계정 자체와 평소 쓰던 Claude Code 설정, Claude 데스크톱 앱의 세션은 그대로입니다.',
    upd_title: 'baton 업데이트', upd_current: '지금 버전 {v}', upd_newest: '최신 버전입니다.', upd_checked: '{t} 확인',
    upd_new: '새 버전 {v}이(가) 나왔습니다.', upd_check: '업데이트 확인', upd_checking: '확인하는 중…', upd_go: '{v}(으)로 업데이트',
    upd_err: '업데이트를 확인하지 못했습니다: {e}', upd_notes: '바뀐 점', upd_page: '릴리스 페이지 열기',
    upd_confirm_title: 'baton {v}(으)로 업데이트',
    upd_confirm_body: 'GitHub에서 baton {v}을(를) 내려받아 체크섬으로 확인한 뒤 설치하고, baton 창을 새로 엽니다. 계정, 설정, 대화는 그대로입니다.',
    upd_portable: '지금은 압축을 푼 폴더에서 실행 중이라, 업데이트한 baton은 시작 메뉴에 설치되고 거기서 다시 열립니다.',
    upd_installing: '내려받아 설치하는 중… (1분쯤 걸릴 수 있습니다)', upd_done: 'baton {v}을(를) 설치했습니다. 새 창을 엽니다.',
    upd_failed: '업데이트하지 못했습니다: {e}', upd_err_sessions: '실행 중인 세션이 있습니다. 세션을 모두 끝낸 뒤 업데이트하세요(세션이 baton.exe를 쓰고 있습니다).',
    upd_err_windows: 'baton이 연 창(세션 창이나 로그인 창)이 아직 열려 있습니다. 그 창들을 닫은 뒤 업데이트하세요.',
    upd_banner: 'baton {v}이(가) 나왔습니다.', upd_banner_go: '자세히', set_upd: 'baton을 열 때 새 버전이 있는지 확인 (GitHub)',
    uninstall_go: '제거…', uninstall_confirm: '이 창이 닫히고, 제거 창에서 한 번 더 확인합니다. 실행 중인 세션이 있으면 먼저 끝내세요.', uninstall_ok: '제거 창 열기',
    // banners / errors
    problem: '파일을 읽지 못했습니다: {e}', claude_warn: '{w}', need_claude: 'Claude Code를 찾지 못해 세션을 시작할 수 없습니다.',
    need_mode: '계정 전환 방식을 아직 고르지 않았습니다.', choose_now: '지금 고르기',
    error: '실패했습니다: {e}', details: '자세히',
    expired: '이 baton 창은 만료되었습니다. baton을 다시 실행하세요.',
    err_expired: '이 baton 창은 만료되었습니다. baton을 다시 실행하세요.',
    err_exists: '같은 이름의 계정이 이미 있습니다. 목록에서 ‘다시 로그인’을 누르거나 다른 이름을 쓰세요.',
    err_signin_open: '이미 열린 로그인 창이 있습니다. 그 창에서 끝내거나 닫은 뒤 다시 시도하세요.',
    err_bad_email: '이메일 주소 형식이 아닙니다.', err_bad_name: '이름은 영문 소문자·숫자·- 로 1~32자, 첫 글자는 문자나 숫자여야 합니다.',
    err_bad_transcript: '이 대화를 열 수 없습니다. 다시 고르세요.', err_no_account: '그런 이름의 계정이 없습니다.',
    err_no_edge: 'Microsoft Edge를 찾지 못했습니다. 주소를 다른 브라우저의 시크릿 창에 붙여 넣으세요.',
    err_no_conversation: '이 폴더에서 한 대화가 없습니다. ‘새 대화’로 시작하세요.',
    err_sessions_running: '실행 중인 세션이 있습니다. 세션을 모두 끝낸 뒤 제거하세요.',
    err_windows_open: 'baton이 연 창(세션 창이나 로그인 창)이 아직 열려 있습니다. 그 창들을 닫은 뒤 제거하세요.',
    switch_now: '요청했습니다. 곧 {a} 계정에서 시작합니다.', switch_now_any: '요청했습니다. 쓸 수 있는 계정에서 곧 시작합니다.',
    num_bad: '1 이상의 정수를 입력하세요.',
    err_open_elsewhere: '이 대화는 다른 창(데스크톱 앱 등)에서 열려 있습니다. 그쪽을 닫고 다시 시도하세요.',
    err_bad_folder: '있는 폴더를 고르세요 (내 PC의 폴더, 예: C:\\Users\\…).', err_long_folder: '경로가 너무 깁니다. 더 짧은 경로의 폴더를 고르세요.',
    ago_now: '방금', ago_min: '{n}분 전', ago_hour: '{n}시간 전', ago_day: '{n}일 전',
  },
  en: {
    tab_sessions: 'Sessions', tab_accounts: 'Accounts', tab_settings: 'Settings', tab_help: 'Help',
    claude_desktop: 'Claude Code {v} from the Claude desktop app', claude_cli: 'Claude Code {v}', claude_checking: 'Checking Claude Code…',
    claude_missing: 'Claude Code not found',
    mode_auto: 'Auto switch', mode_confirm: 'Ask before switching', mode_off: 'Switching off', mode_unset: 'Switch mode not set',
    lang_label: 'Language',
    sessions_title: 'Running sessions', sessions_sub: 'Each session runs in its own window. Closing this window does not stop them.',
    new_session: 'Start a session', no_sessions_title: 'No session is running',
    no_sessions_body: 'Pick a project folder and start Claude Code. When an account hits its limit, baton continues the same conversation on the next one.',
    need_account_first: 'Add an account first.', go_accounts: 'Add an account',
    no_usable: 'No account can be used. Sign one in or turn one on in the Accounts tab.', go_accounts_tab: 'Go to Accounts',
    run_running: 'Running', run_stopping: 'Switching after this turn', run_switching: 'Switching account', run_waiting: 'Waiting',
    waiting_hint: 'Every account is limited, or switching is happening too often, so baton waits. The session window says when it continues.',
    waiting_first: 'Waiting for the first account to free up', run_on_none: 'waiting for an account',
    run_asking: 'Waiting for your answer in the session window', run_ending: 'Ending the session', run_on: 'on {a}', run_since: 'started {t}',
    asking_stop_hint: 'To end the session, press Ctrl+C in the session window (answering n keeps it on the current account).',
    ended_console: 'An ended session window is still open; press Enter in it to close it.', retry_now: 'Try {a} again now',
    switch_btn: 'Switch account', switch_auto: 'Next account (automatic)', switch_to: 'To {a}', open_folder: 'Open folder', open_log: 'Log',
    switch_asked: 'Switch requested. It happens when the current turn ends (within a minute).', asking_hint: 'Answer the question shown in the session window.',
    switch_asked_confirm: 'Switch requested. When the current turn ends, the session window asks you to confirm it with Enter.',
    recent_title: 'Recent switches', recent_none: 'No switches yet.',
    reason_usage_limit: 'limit', reason_authentication_failed: 'login failed', reason_manual: 'manual',
    job_run: 'Opening the session window…',
    stop_btn: 'End session', stop_title: 'End this session?', stop_body: 'Claude Code is stopped safely. The conversation is saved and can be continued later from Recent sessions. If an answer is in progress, baton waits up to 10 seconds, then interrupts it.',
    stop_go: 'End', stop_asked: 'Asked the session to end.', switch_confirm_hint: 'Confirm mode: confirm the switch with Enter in the session window.',
    recent_sessions: 'Recent sessions', recent_sessions_none: 'Ended sessions show up here.', continue_btn: 'Continue',
    cl_busy: 'Claude is working', cl_idle: 'Waiting for you', cl_waiting: 'Needs an answer in the session window',
    cl_waiting_hint: 'Answer in the session window to go on: {w}', ctx: 'context {p}% used',
    ch_none: 'No changed files', ch_files: '{n} files', ch_files1: '1 file', ch_btn: 'Changes', ch_title: 'Changes in {f}',
    ch_norepo: 'This folder is not a git repository, so there are no changes to show.', ch_nogit: 'git was not found. Install Git for Windows to see changes.',
    ch_note: 'Files changed in this folder since the last commit (uncommitted work, new files included).', ch_loading: 'Reading changes…', ch_error: 'git could not read the changes: {e}',
    gs_modified: 'modified', gs_added: 'added', gs_deleted: 'deleted', gs_renamed: 'renamed', gs_untracked: 'new', gs_conflict: 'conflict',
    rc_on: 'Remote Control', details_more: 'Details', details_less: 'Less', d_path: 'Folder', d_started: 'Started', d_changes: 'Changes', ctx_short: 'Context', rc_where: 'Sign in to the Claude app on your phone or to claude.ai/code with the account {a} to see this session. After a switch it shows on the new account.',
    start_rc: 'Turn on Remote Control', start_rc_hint: 'See and continue this session from the Claude app on your phone or claude.ai/code. It shows for the account the session runs on, and comes back on for the next account after a switch.',
    set_rc: 'Turn on Remote Control for new sessions by default',
    show_window: 'Show window', show_window_tip: "Bring this session's Claude Code window to the front",
    cut_title: '{n} sessions were cut off', cut_title1: '1 session was cut off',
    cut_body: 'The PC restarted, or a session stopped unexpectedly. Reopening one goes on with the same conversation and options, and a session that was working carries on with its task.',
    cut_restart: 'PC restarted', cut_stopped: 'Stopped unexpectedly', cut_working: 'Was working',
    restore_btn: 'Reopen', restore_all: 'Reopen all', dismiss_btn: 'Dismiss', dismiss_all: 'Dismiss all',
    restore_started: 'Reopening; each session opens in its own window.', err_not_cut_off: 'That session was reopened or dismissed already.',
    set_rules: 'Accounts per folder', set_rules_hint: 'Sessions in a folder with a rule (and in the folders in it) start and switch only on the accounts you choose. With one account, such a session never moves to another. Use it, say, to keep a company repository on the company account.',
    rules_none: 'No rules: sessions in every folder use every account.', rule_add: 'Add a rule', rule_edit: 'Change', rule_remove: 'Remove',
    rule_title: 'Accounts for a folder', rule_folder: 'Folder', rule_accounts: 'Accounts to use in this folder', rule_save: 'Save', rule_missing: 'No such account: {a}',
    rule_saved: 'Rule saved.', rule_removed: 'Rule removed.', rule_note: 'A session that is already running only takes on a stricter rule; anything else applies from the next session.',
    err_rule_empty: 'Choose at least one account.', err_rule_account: "This folder's rule does not allow that account.", start_rule: 'By its rule, this folder uses only {a}.',
    err_no_window: "The session's window could not be found; it may be closed.", err_focus: "The session's window could not be brought forward; open it from the taskbar.",
    set_notify: 'Windows notifications', set_notify_hint: "baton tells you with a Windows notification when you are not looking at the session window. Clicking one brings up that session's window.",
    notify_all: 'All: account switches and limits, when Claude waits for an answer, and when a response that took over 30 seconds is done',
    notify_important: 'Important only: all but finished responses', notify_off: 'Off', notify_test: 'Send a test notification',
    notify_sent: 'Sent. If nothing showed up, check baton and Do not disturb in Windows Settings → System → Notifications.',
    set_awake: 'Keep the PC from sleeping while a session works',
    set_awake_hint: "Only while Claude works or a session waits for a limit to reset. The screen can still turn off, and Sleep or closing a laptop's lid still puts it to sleep.",
    trust_q: 'Do you trust this folder?', trust_body: 'Claude Code will be able to read and change files in it and run commands. Tick only for a folder you trust.',
    trust_ok: 'I trust this folder', trust_needed: 'Sessions start only in a trusted folder. Tick the box and press Start again.',
    folder_bad: 'Choose an existing folder on this PC (e.g. C:\\Users\\…).', folder_long: 'That path is too long. Choose a folder with a shorter path.',
    first_start: 'An account used for the first time can take a minute or two to get ready.',
    open_elsewhere: 'This conversation is open in another window (the desktop app or another session). Close it there and try again.',
    claude_app_setup: 'The Claude desktop app has not set up Claude Code yet. Open its Code tab once.',
    claude_too_old: 'Claude Code is too old. Update the Claude desktop app (Help → Check for updates).',
    claude_cli_old: 'The installed Claude Code CLI is too old. Run claude update in PowerShell.',
    claude_cli_old_app: 'If you do not use the CLI itself, you can instead update the Claude desktop app and open its Code tab once. baton then uses the Claude Code that comes with the app.',
    claude_path_old: 'The Claude Code that claude_path points to is too old. Update it, or go back to finding it automatically with: baton config claude_path auto',
    claude_old_cli_skipped: 'The installed Claude Code CLI {v} is too old, so baton uses the Claude Code from the Claude desktop app instead. To update the CLI, run claude update in PowerShell.',
    claude_bundle_off: 'Using the Claude Code inside the Claude desktop app is turned off. Turn it on again in Settings → Advanced.',
    recheck: 'Check again', elevated: 'baton is running as administrator, so sessions would too. Close it and open it again normally.',
    claude_custom_cli: 'To use claude.exe from another place, run: baton config claude_path <path>',
    start_title: 'Start a session', start_folder: 'Project folder', browse: 'Browse…', recent_folders: 'Recent folders',
    start_account: 'Account', account_auto: 'Automatic (first available, in order)',
    start_conv: 'Conversation', conv_new: 'New conversation', conv_continue: 'Continue the last conversation in this folder', conv_pick: 'Choose from the list',
    conv_none: 'No conversations in this folder yet.', conv_loading: 'Loading conversations…', conv_untitled: '(untitled)', conv_latest: 'Continues: {p}',
    conv_where_home: 'your usual Claude Code (incl. the desktop app)', conv_where: 'stored on {a}',
    start_go: 'Start', cancel: 'Cancel', close: 'Close',
    start_done: 'Claude Code opens in a new window. Work there.', choose_folder_first: 'Choose a folder.',
    pick_conv_first: 'Choose a conversation to continue.',
    import_btn: 'Bring in a conversation', import_title: 'Bring in a Claude Code conversation',
    import_sub: 'Continue in baton a conversation you had in the Claude desktop app or a terminal. Choose one, then the account to start on.',
    import_search: 'Search by title, first prompt or folder', import_none: 'No conversations yet. Conversations you have in Claude Code show up here.',
    import_no_match: 'No conversation matches.', import_go: 'Continue this conversation',
    src_desktop: 'Claude desktop app', src_cli: 'Terminal', src_vscode: 'VS Code', src_other: 'Claude Code',
    conv_open: 'Open elsewhere', conv_gone: 'Folder gone',
    conv_open_hint: 'This conversation is open in another Claude Code window, so it can only continue in a copy: a new conversation that carries everything said so far and goes on apart from the original.',
    held_desktop: 'the Claude desktop app', held_cli: 'Claude Code in a terminal (CLI)', held_vscode: 'VS Code', held_other: 'another Claude Code window',
    held_title: 'This conversation is open in {app} right now',
    held_close_desktop: 'To go on with the same conversation, quit the Claude desktop app completely first (also from its tray icon), then press Check again below.',
    held_close_cli: 'To go on with the same conversation, end Claude Code in that terminal first (/exit, or Ctrl+C twice), then press Check again below.',
    held_close_other: 'To go on with the same conversation, close it in that window first, then press Check again below.',
    held_copy: 'If you go on anyway, baton brings it in as a copy: a new conversation that carries everything said so far, goes on apart from the original and does not sync with it.',
    held_recheck: 'Check again', import_go_copy: 'Continue in a copy', held_still: 'Still open.', held_free: 'Closed now: it can go on as the same conversation.',
    conv_gone_hint: 'The folder this conversation ran in is gone, so it cannot continue.',
    conv_unusable: 'Folder not usable',
    conv_bad_hint: 'baton starts sessions only in folders on this PC (such as C:\\…) with a path shorter than 248 characters, so this conversation cannot continue from here.',
    import_more: 'Only the newest 200 conversations are shown. For older ones, run baton sessions --max 1000 on the command line and continue one with baton resume <id>.',
    copy_ok: 'Continue in a copy (leaves the original as it is)',
    accounts_title: 'Accounts', accounts_sub: 'Sign in once per account (including two-factor authentication) and it stays signed in. Switching never signs in again.',
    add_account: 'Add account', check_logins: 'Check sign-ins', checking: 'Checking…',
    no_accounts_title: 'No accounts yet',
    no_accounts_body: 'Add each Claude account you want to use. Each one signs in once in your browser.',
    st_ok: 'Ready', st_limited: 'Limited until {t}', st_needs_login: 'Sign in again', st_signed_out: 'Not signed in',
    st_blocked: 'Blocked', st_off: 'Off', st_dup: 'Duplicate account', st_signing: 'Signing in…',
    kept: 'Signed in', checked_at: 'checked {t}', not_checked: 'not checked yet',
    use_5h: '5-hour limit', use_7d: 'Weekly limit', use_resets: 'resets {t}', use_asof: 'as of {t}',
    use_unknown: 'Usage shows up once a baton session has run on this account.',
    login_again: 'Sign in again', enabled_label: 'Use for switching', use_short: 'Use', up: 'Up', down: 'Down', remove: 'Remove',
    unblock: 'Resolved — use again',
    order_hint: 'Accounts are used from the top. Putting the account you use in the Claude desktop app last saves its desktop chat limit.',
    blocked_hint: 'Blocked (on hold, billing problem or revoked sign-in). Once resolved, press Resolved — use again (no new sign-in needed). Sign in again only if the sign-in was revoked.',
    dup_hint: 'Turned off: it signed in as an account you already added. Sign it in as a different account.',
    add_title: 'Add account', add_name: 'Account name (only used inside baton)', add_name_hint: 'lowercase letters, digits and - (e.g. personal, work)',
    add_email: 'Email of this account (recommended)', add_email_hint: 'It is pre-filled on the sign-in page, which helps you sign in to the right account.',
    add_private: 'My browser is already signed in to another Claude account (sign in in an InPrivate window)',
    add_go: 'Open sign-in window',
    signin_title: 'Signing in “{a}”', signin_close_hint: 'Closing the sign-in window (the black one) cancels the sign-in; the account stays in the list as Not signed in, and Sign in again continues it.',
    signin_steps_normal: ['The black sign-in window opens and your browser shows the Claude sign-in page.', 'Sign in and finish two-factor authentication. You do this once for this account.', 'The result appears here when it is done.'],
    signin_steps_private: ['The Claude sign-in page opens in an InPrivate window. Your normal browser stays signed in as before.', 'Sign in and finish two-factor authentication there. The result appears here when it is done.', 'If your normal browser opened instead (do not use that tab; close it), or you closed the InPrivate window: in the black sign-in window, select the https://claude… address with the mouse, copy it with Ctrl+C and press the button below.'],
    open_private: 'Open in InPrivate again', private_opened: 'InPrivate window opened. Sign in there.',
    private_no_url: 'No address copied yet. Select the https://claude… address in the black window, press Ctrl+C, then press this again.',
    res_ok: 'Signed in: {e}', res_ok_plan: 'Signed in: {e} ({p})', res_still_off: 'This account is turned off, so it is not used yet. Turn on its “Use” switch at the right of the list to use it.',
    res_unfinished: 'The sign-in window for “{a}” closed without a result. Try again.',
    res_api: 'This account signed in without a Claude subscription (API key or similar). Sign it in with a subscription account.',
    res_duplicate: '“{a}” signed in as the same Claude account ({e}) as “{d}”, so it was turned off. Sign it in as a different account.',
    res_login_failed: 'Signing in “{a}” did not finish. Try again.', res_not_signed_in: '“{a}” is not signed in. Try again.',
    res_busy: 'A session is running on “{a}”, so it cannot sign in again now. Try after the session ends.',
    res_error: 'Signing in “{a}” failed: {m}', retry_private: 'Sign in again in InPrivate', retry: 'Try again', dismiss: 'Dismiss',
    in_use: 'In use', in_use_hint: 'A session is running on this account.',
    add_how: 'A sign-in window (black) opens and your browser shows the Claude sign-in page. Finish signing in and two-factor authentication; the account then appears here. If the browser does not open, paste the address shown in the black window into it.',
    add_tip_second: 'If your browser is already signed in to claude.ai as another account, it will connect that account. Sign out of claude.ai first, or paste the address shown in the black window into an InPrivate window.',
    add_started: 'Sign-in window opened. Finish signing in in your browser.',
    login_started: 'Sign-in window opened.',
    bad_name: 'Use 1-32 lowercase letters, digits or -, starting with a letter or digit.',
    remove_title: 'Remove “{a}”', remove_body: 'baton signs this account out and deletes it. Using it again means signing in (with two-factor authentication) from scratch. Conversations that started on it are not deleted: they move to your own Claude Code. Your Claude account itself is not affected.',
    remove_go: 'Remove', removed: 'Removed “{a}”.',
    notice_title: 'Please read this first',
    notice_points: [
      'Being signed in to several of your own accounts is not what this notice is about. It is about continuing on another account after a usage limit.',
      'When the account in use hits its usage limit, baton stops Claude Code and continues the same conversation on another account.',
      'Only add accounts you personally own. Sharing an account with other people is against Anthropic\'s terms.',
      'Anthropic\'s Usage Policy prohibits intentionally bypassing restrictions in its products without permission and using multiple accounts to get around its safeguards. Continuing past a usage limit on another account may be treated as exactly that, and Anthropic can suspend accounts without notice. No Anthropic source says this use is allowed. Use it at your own risk.',
      'Each switch makes the next account read the whole conversation again, which uses part of its quota.',
      'baton never reads or stores your credentials; Claude Code signs in and refreshes each account itself.',
    ],
    notice_ack: 'I have read this and want baton to switch accounts.',
    choose_mode: 'Switch mode',
    mode_auto_desc: 'Continue on the next account without asking.',
    mode_confirm_desc: 'Ask in the session window before each switch; you confirm there with Enter. (Recommended)',
    mode_off_desc: 'Never switch automatically; use the button in this window when needed.',
    ob_welcome_title: 'Welcome to baton',
    ob_welcome_points: [
      'Sign in once per account (including two-factor authentication) and it stays signed in.',
      'Pick a folder and start a session: Claude Code opens in its own window.',
      'When an account hits its limit, the same conversation continues on the next one.',
    ],
    ob_claude_title: 'Claude Code',
    ob_claude_desktop: 'baton uses Claude Code {v} from your Claude desktop app. Nothing else to install.',
    ob_claude_cli: 'baton uses your installed Claude Code {v}.',
    ob_claude_missing: 'Claude Code was not found. Install the Claude desktop app (claude.ai/download); baton uses the Claude Code inside it.',
    ob_claude_wait: 'Looking for Claude Code…',
    next: 'Next', back: 'Back', finish: 'Get started', save: 'Save', apply: 'Apply',
    ack_needed: 'Tick the box to turn on automatic or confirmed switching.',
    settings_title: 'Settings', set_mode: 'Switch mode', set_mode_hint: 'Questions appear in the session\'s own window.',
    set_exhausted: 'When every account is limited', ex_wait: 'Wait for the first account to free up, then continue', ex_stop: 'Stop and show how to continue later',
    set_advanced: 'Advanced', set_cooldown: 'Minimum seconds between switches', set_max: 'Most switches per hour', set_retry: 'Minutes to wait when the reset time is unknown',
    set_prompt: 'Message sent to the next account to continue', set_share: 'Share your usual Claude Code setup (skills, CLAUDE.md, …) with every account',
    set_claude: 'Claude Code location', set_claude_auto: 'Find automatically',
    set_claude_which: 'Claude Code to use', claude_which_auto: 'Automatic (the installed CLI if there is one and it is not too old, else the desktop app\'s)',
    claude_which_desktop: 'Always the one inside the Claude desktop app',
    claude_which_note: 'Either way, sessions open in a Claude Code window of their own (a terminal screen), not inside the desktop app.', set_claude_bundle: 'Also use the Claude Code inside the Claude desktop app',
    set_claude_custom: 'Custom (full path to claude.exe)', saved: 'Saved.', set_lang: 'Language', lang_auto: 'Same as Windows',
    help_title: 'How baton works',
    help_points: [
      'Each account has its own Claude Code profile; Claude Code manages its sign-in.',
      'A session opens as a Claude Code window. Work there as usual.',
      'When the account in use hits its limit, baton stops Claude Code safely and reopens the same conversation on the next account (about 5-10 seconds).',
      'The conversation and project folder stay the same; background commands that were running end and are not re-run.',
      'The Claude desktop app\'s own sign-in does not change; baton\'s accounts are signed in separately.',
    ],
    notice_short: 'Before you use it', diag_title: 'Diagnostics', diag_run: 'Run diagnostics', diag_running: 'Running…',
    open_data: 'Open data folder', cli_title: 'Command line', cli_body: 'baton.exe in the same folder offers everything from PowerShell too; in that folder, e.g. .\\baton.exe list, .\\baton.exe run.',
    version: 'version {v}',
    install_title: 'Install to the Start menu', install_body: 'baton runs from the extracted folder now. Installing adds it to the Start menu, and a taskbar pin keeps working. No administrator rights needed.',
    install_go: 'Install and reopen', installed_note: 'Installed in the Start menu.', install_failed: 'Could not install: {e}',
    uninstall_title: 'Uninstall baton', uninstall_body: 'baton signs out every account and deletes its data (each account\'s sign-in). Conversations that started in baton are moved to your own Claude Code (%USERPROFILE%\\.claude) first, so claude --resume opens them again. Your Claude accounts, your own Claude Code setup and the Claude desktop app\'s sessions are not affected.',
    upd_title: 'Update baton', upd_current: 'This is version {v}', upd_newest: 'This is the newest version.', upd_checked: 'checked {t}',
    upd_new: 'baton {v} is out.', upd_check: 'Check for updates', upd_checking: 'Checking…', upd_go: 'Update to {v}',
    upd_err: 'Could not check for updates: {e}', upd_notes: 'What changed', upd_page: 'Open the release page',
    upd_confirm_title: 'Update baton to {v}',
    upd_confirm_body: 'baton downloads {v} from GitHub, checks it against its checksums, installs it and opens its window again. Your accounts, settings and conversations stay as they are.',
    upd_portable: 'baton runs from the unzipped folder now, so the update is installed to the Start menu and opens from there.',
    upd_installing: 'Downloading and installing… (this can take a minute)', upd_done: 'Installed baton {v}. Opening it…',
    upd_failed: 'Could not update: {e}', upd_err_sessions: 'Sessions are running. End them all, then update (they use baton.exe).',
    upd_err_windows: 'Windows that baton opened (session or sign-in windows) are still open. Close them, then update.',
    upd_banner: 'baton {v} is out.', upd_banner_go: 'Details', set_upd: 'Check for a newer baton when it opens (GitHub)',
    uninstall_go: 'Uninstall…', uninstall_confirm: 'This window closes and the uninstall window asks once more. End running sessions first.', uninstall_ok: 'Open uninstall',
    problem: 'Could not read a file: {e}', claude_warn: '{w}', need_claude: 'Claude Code was not found, so sessions cannot start.',
    need_mode: 'You have not chosen how baton switches accounts yet.', choose_now: 'Choose now',
    error: 'Failed: {e}', details: 'Details',
    expired: 'This baton window has expired. Open baton again.',
    err_expired: 'This baton window has expired. Open baton again.',
    err_exists: 'An account with this name already exists. Use Sign in again in the list, or choose another name.',
    err_signin_open: 'A sign-in window is already open. Finish or close it, then try again.',
    err_bad_email: 'That does not look like an email address.', err_bad_name: 'Use 1-32 lowercase letters, digits or -, starting with a letter or digit.',
    err_bad_transcript: 'This conversation cannot be opened. Choose again.', err_no_account: 'There is no account with that name.',
    err_no_edge: 'Microsoft Edge was not found. Paste the address into a private window of another browser.',
    err_no_conversation: 'There is no conversation in this folder yet. Start a new one.',
    err_sessions_running: 'Sessions are running. End them all, then uninstall.',
    err_windows_open: 'Windows that baton opened (session or sign-in windows) are still open. Close them, then uninstall.',
    switch_now: 'Requested: it starts on {a} shortly.', switch_now_any: 'Requested: it starts on a free account shortly.',
    num_bad: 'Enter a whole number of 1 or more.',
    err_open_elsewhere: 'This conversation is open in another window (the desktop app or another session). Close it there and try again.',
    err_bad_folder: 'Choose an existing folder on this PC (e.g. C:\\Users\\…).', err_long_folder: 'That path is too long. Choose a folder with a shorter path.',
    ago_now: 'just now', ago_min: '{n} min ago', ago_hour: '{n} h ago', ago_day: '{n} d ago',
  },
};

const S = { state: null, lang: 'ko', tab: null, lastJSON: '', onboardingShown: false, mainDirty: false,
  ui: { doctorOut: null, doctorOk: true, doctorRunning: false, advancedOpen: false, signinPrivate: false, dismissed: {}, runOpen: {} } };

function t(key, vars) {
  let s = (T[S.lang] && T[S.lang][key]) ?? T.en[key] ?? key;
  if (vars && typeof s === 'string') for (const [k, v] of Object.entries(vars)) s = s.replaceAll('{' + k + '}', v);
  return s;
}

// h builds an element. props: class, text, title, on<event>, and attributes.
function h(tag, props, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'text') el.textContent = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k === 'checked' || k === 'disabled' || k === 'selected' || k === 'value') el[k] = v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const kid of kids.flat()) if (kid !== null && kid !== undefined && kid !== false) el.append(kid);
  return el;
}

// The session token: the window URL carries a one-time code, exchanged here
// once; the token is kept for reloads of this tab only.
let token = '';
try { token = sessionStorage.getItem('baton_token') || ''; } catch (_) { /* none */ }
// Each load of the page has an id of its own: a reload or a duplicated tab
// shares the token, and the GUI must know when every load has closed.
const pageId = Array.from(crypto.getRandomValues(new Uint8Array(12)), (b) => b.toString(16).padStart(2, '0')).join('');
let seq = 0; // numbers this load's requests (see the server's seqHeader)
function ids() { return { 'X-Baton-Token': token, 'X-Baton-Page': pageId, 'X-Baton-Seq': String(++seq) }; }

async function connect() {
  const k = new URLSearchParams(location.search).get('k');
  if (!k) return;
  history.replaceState(null, '', '/');
  const r = await fetch('/api/session', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code: k }) });
  if (!r.ok) return;
  token = (await r.json()).token || '';
  try { sessionStorage.setItem('baton_token', token); } catch (_) { /* this load only */ }
}

async function api(path, body) {
  const opts = { method: body === undefined ? 'GET' : 'POST', headers: ids(), credentials: 'omit' };
  if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
  const r = await fetch('/api/' + path, opts);
  let data = {};
  try { data = await r.json(); } catch (_) { /* empty */ }
  if (!r.ok) {
    const e = new Error(data.error || r.statusText);
    e.code = data.code; e.details = data.details; e.status = r.status;
    throw e;
  }
  return data;
}

let toastTimer;
function toast(msg, bad) {
  const el = document.getElementById('toast');
  el.textContent = msg;
  el.className = 'show' + (bad ? ' bad' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.className = ''; }, bad ? 7000 : 4500);
}

// errText explains a failure in the window's language when the server named
// it; anything else shows baton's own message.
function errText(e) {
  const k = 'err_' + e.code;
  if (e.code && ((T[S.lang] && T[S.lang][k]) || T.en[k])) return t(k);
  return t('error', { e: e.message });
}

function showError(e) {
  toast(errText(e), true);
}

// cleanPath drops the quotes Explorer's "Copy as path" adds.
function cleanPath(p) { return (p || '').trim().replace(/^"(.*)"$/, '$1').trim(); }

// modeReady: sessions start only once the user chose how baton switches.
function modeReady(st) { return !!st.config.switch_mode && (st.config.switch_mode === 'off' || st.config.risk_acknowledged); }

function startSession(pre) {
  if (modeReady(S.state)) openStart(pre);
  else openNotice(() => openStart(pre));
}

// ---- time ----------------------------------------------------------------------

function clock(iso) {
  const d = new Date(iso);
  const far = Math.abs(d - Date.now()) > 20 * 3600e3;
  const loc = S.lang === 'ko' ? 'ko-KR' : 'en-US';
  return far ? d.toLocaleString(loc, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
    : d.toLocaleTimeString(loc, { hour: '2-digit', minute: '2-digit' });
}

function ago(iso) {
  const m = Math.round((Date.now() - new Date(iso)) / 60000);
  if (m < 1) return t('ago_now');
  if (m < 60) return t('ago_min', { n: m });
  if (m < 48 * 60) return t('ago_hour', { n: Math.round(m / 60) });
  return t('ago_day', { n: Math.round(m / 1440) });
}

function baseName(p) { return (p || '').replace(/[\\/]+$/, '').split(/[\\/]/).pop() || p; }

// ---- polling ---------------------------------------------------------------------

async function refresh() {
  try {
    const st = await api('state');
    const json = JSON.stringify(st);
    S.state = st;
    if (S.lang !== st.lang) { S.lang = st.lang; S.lastJSON = ''; document.documentElement.lang = st.lang; }
    if (!S.tab) S.tab = st.accounts.length ? 'sessions' : 'accounts';
    if (json !== S.lastJSON) { S.lastJSON = json; render(); }
    maybeOnboard();
  } catch (e) {
    if (e.status === 403) { document.body.textContent = S.state ? t('expired') : T.ko.expired + ' / ' + T.en.expired; return; }
  }
}

function schedule() {
  const ms = document.visibilityState === 'visible' ? 1500 : 10000;
  setTimeout(async () => { await refresh(); schedule(); }, ms);
}

document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') refresh(); });
window.addEventListener('pagehide', () => {
  fetch('/api/bye', { method: 'POST', keepalive: true, headers: ids(), credentials: 'omit' }).catch(() => {});
});

// ---- render ------------------------------------------------------------------------

function render() {
  const st = S.state;
  if (!st) return;
  renderTop(st);
  renderTabs(st);
  renderBanners(st);
  const main = document.getElementById('main');
  const a = document.activeElement;
  if (a && main.contains(a) && /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName) && a.type !== 'checkbox' && a.type !== 'radio') {
    S.mainDirty = true; // redrawn when the field loses focus
    return;
  }
  S.mainDirty = false;
  main.replaceChildren(...({ sessions: viewSessions, accounts: viewAccounts, settings: viewSettings, help: viewHelp }[S.tab])(st).filter(Boolean));
}

// A redraw put off while typing happens once the field loses focus, but not
// when focus moved to a button in the same view: replacing it under the
// pointer would swallow the click (and the typed value with it).
document.addEventListener('focusout', () => {
  setTimeout(() => {
    const a = document.activeElement;
    const inMain = a && a !== document.body && document.getElementById('main').contains(a);
    if (S.mainDirty && !inMain) render();
  }, 0);
});
// Within the view, the redraw waits until the click has gone through.
document.addEventListener('click', () => { if (S.mainDirty) setTimeout(render, 0); });

function claudeChip(st) {
  if (st.claude_pending) return h('span', { class: 'chip', text: t('claude_checking') });
  if (!st.claude.found) return h('span', { class: 'chip bad', text: t('claude_missing'), title: st.claude.error || '' });
  const label = st.claude.source === 'desktop' ? t('claude_desktop', { v: st.claude.version }) : t('claude_cli', { v: st.claude.version });
  return h('span', { class: 'chip' + (st.claude.warning ? ' warn' : ' ok'), text: label, title: st.claude.warning || st.claude.path });
}

function modeChip(st) {
  const m = st.config.switch_mode;
  const cls = m === 'auto' || m === 'confirm' ? 'ok' : m === 'off' ? '' : 'warn';
  return h('button', { class: 'chip ' + cls, text: m ? t('mode_' + m) : t('mode_unset'), onclick: () => { S.tab = 'settings'; render(); } });
}

function renderTop(st) {
  const sel = h('select', { 'aria-label': t('lang_label'), class: 'chip', onchange: (e) => setConfig('gui_language', e.target.value, false, true) },
    h('option', { value: 'auto', text: t('lang_auto'), selected: !st.config.gui_language }),
    h('option', { value: 'ko', text: '한국어', selected: st.config.gui_language === 'ko' }),
    h('option', { value: 'en', text: 'English', selected: st.config.gui_language === 'en' }));
  sel.classList.add('auto-w');
  document.getElementById('topbar').replaceChildren(claudeChip(st), modeChip(st), sel);
}

function renderTabs(st) {
  const tabs = [['sessions', st.runs.length], ['accounts', st.accounts.length], ['settings'], ['help']];
  document.getElementById('tabs').replaceChildren(...tabs.map(([id, n]) => h('button', {
    role: 'tab', 'aria-selected': String(S.tab === id), onclick: () => { S.tab = id; render(); document.getElementById('main').focus(); },
  }, t('tab_' + id), n ? h('span', { class: 'count', text: String(n) }) : null)));
}

function renderBanners(st) {
  const out = [];
  for (const p of st.problems || []) out.push(h('div', { class: 'banner bad' }, h('div', { class: 'grow', text: t('problem', { e: p }) })));
  if (st.elevated) out.push(h('div', { class: 'banner warn' }, h('div', { class: 'grow', text: t('elevated') })));
  if (!st.claude_pending && !st.claude.found) {
    const msg = st.claude.bundle_off ? t('claude_bundle_off')
      : st.claude.too_old ? (st.claude.source === 'desktop' ? t('claude_too_old') : st.claude.source === 'config' ? t('claude_path_old')
        : t('claude_cli_old') + (st.claude.desktop_app && st.config.allow_desktop_bundle ? ' ' + t('claude_cli_old_app') : ''))
        : st.claude.desktop_app ? t('claude_app_setup') : t('ob_claude_missing');
    out.push(h('div', { class: 'banner warn' }, h('div', { class: 'grow' }, h('b', { text: t('need_claude') }), h('div', { text: msg }),
      st.claude.error ? h('div', { class: 'small muted', text: st.claude.error }) : null),
      h('button', { class: 'btn', text: t('recheck'), onclick: async () => { await api('claude/refresh', {}).catch(showError); refresh(); } })));
  }
  if (st.update && st.update.newer && !S.ui.dismissed['upd' + st.update.latest] && S.tab !== 'help') {
    out.push(h('div', { class: 'banner info' }, h('div', { class: 'grow', text: t('upd_banner', { v: st.update.latest }) }),
      h('button', { class: 'btn', text: t('upd_banner_go'), onclick: () => { S.tab = 'help'; render(); } }),
      h('button', { class: 'btn ghost', text: t('dismiss'), onclick: () => { S.ui.dismissed['upd' + st.update.latest] = true; render(); } })));
  }
  const needLogin = st.accounts.filter((a) => a.enabled && a.status === 'needs_login');
  if (needLogin.length && S.tab !== 'accounts') {
    out.push(h('div', { class: 'banner warn' }, h('div', { class: 'grow', text: t('st_needs_login') + ': ' + needLogin.map((a) => a.id).join(', ') }),
      h('button', { class: 'btn', text: t('tab_accounts'), onclick: () => { S.tab = 'accounts'; render(); } })));
  }
  if (!st.config.switch_mode || (st.config.switch_mode !== 'off' && !st.config.risk_acknowledged)) {
    out.push(h('div', { class: 'banner warn' }, h('div', { class: 'grow', text: t('need_mode') }),
      h('button', { class: 'btn primary', text: t('choose_now'), onclick: () => openNotice() })));
  }
  document.getElementById('banners').replaceChildren(...out);
}

// ---- sessions ----------------------------------------------------------------------

function usableAccounts(st) {
  return st.accounts.filter((a) => a.enabled && a.status !== 'blocked' && a.status !== 'needs_login' && a.status !== 'signed_out');
}

function viewSessions(st) {
  const canStart = usableAccounts(st).length > 0;
  const head = h('div', { class: 'card-head' },
    h('div', {}, h('h2', { text: t('sessions_title') }), h('p', { class: 'muted small', text: t('sessions_sub') })),
    h('div', { class: 'actions' },
      h('button', { class: 'btn', text: t('import_btn'), disabled: !canStart || !st.claude.found, onclick: () => openImport() }),
      h('button', { class: 'btn primary', text: t('new_session'), disabled: !canStart || !st.claude.found, onclick: () => startSession() })));
  const rows = st.runs.map((r) => runRow(st, r));
  // A session window opened from here stays open after its session ends
  // when there is a message to read: only a new one is "opening".
  const runJobs = st.jobs.filter((j) => j.kind === 'run' && j.running);
  const ages = runJobs.map((j) => Date.now() - new Date(j.started));
  const opening = ages.filter((a) => a < 90e3).length;
  const lingering = runJobs.length - opening > rows.length;
  // The state does not change when a window passes 90 s: redraw then.
  clearTimeout(S.openingTimer);
  if (opening) S.openingTimer = setTimeout(render, 90e3 - Math.max(...ages.filter((a) => a < 90e3)) + 200);
  let body;
  if (rows.length) body = h('div', {}, rows);
  else if (!st.accounts.length) body = h('div', { class: 'empty' }, h('h3', { text: t('no_accounts_title') }), h('p', { text: t('need_account_first') }),
    h('button', { class: 'btn primary', text: t('go_accounts'), onclick: () => { S.tab = 'accounts'; render(); } }));
  else if (!canStart) body = h('div', { class: 'empty' }, h('h3', { text: t('no_sessions_title') }), h('p', { text: t('no_usable') }),
    h('button', { class: 'btn primary', text: t('go_accounts_tab'), onclick: () => { S.tab = 'accounts'; render(); } }));
  else body = h('div', { class: 'empty' }, h('h3', { text: t('no_sessions_title') }), h('p', { text: t('no_sessions_body') }));
  const sessions = h('div', { class: 'card' }, head,
    opening && !rows.length ? h('p', { class: 'muted' }, h('span', { class: 'spin' }), ' ', t('job_run')) : null,
    lingering ? h('p', { class: 'muted small', text: t('ended_console') }) : null, body);
  const recentSessions = h('div', { class: 'card' }, h('h3', { text: t('recent_sessions') }),
    st.recent_sessions.length ? h('div', {}, st.recent_sessions.map((r) => h('div', { class: 'row' },
      h('div', { class: 'grow' },
        h('div', { class: 'title', text: convTitle(r) }),
        h('div', { class: 'sub', title: r.cwd, text: `${baseName(r.cwd)} · ${ago(r.ended)} · ${r.where === 'claude-code' ? t('conv_where_home') : t('conv_where', { a: r.where })}` })),
      h('button', { class: 'btn', text: t('continue_btn'), disabled: !st.claude.found || !canStart,
        onclick: () => startSession({ folder: r.cwd, mode: 'pick', session_id: r.session_id, where: r.where,
          conv: { session_id: r.session_id, where: r.where, title: r.title, first_prompt: r.first_prompt, cwd: r.cwd, modified: r.ended } }) }))))
      : h('p', { class: 'muted', text: t('recent_sessions_none') }));
  const recent = h('div', { class: 'card' }, h('h3', { text: t('recent_title') }),
    st.switches.length ? h('div', {}, st.switches.map((sw) => h('div', { class: 'row' },
      h('div', { class: 'grow' }, h('div', { text: `${sw.from} → ${sw.to}` }), h('div', { class: 'sub', text: `${clock(sw.at)} · ${t('reason_' + sw.reason)}` })))))
      : h('p', { class: 'muted', text: t('recent_none') }));
  return [cutCard(st, canStart), sessions, recentSessions, recent];
}

// cutCard offers the sessions that were cut off (a restart, a crash) for
// reopening as they were.
function cutCard(st, canStart) {
  const cut = st.unfinished || [];
  if (!cut.length) return null;
  const act = async (path, ids, done) => {
    try { await api(path, { run_ids: ids }); if (done) toast(done); S.lastJSON = ''; await refresh(); }
    catch (e) { if (e.code === 'needs_mode') { openNotice(); return; } showError(e); }
  };
  const ids = cut.map((c) => c.run_id);
  return h('div', { class: 'card warn-card' },
    h('div', { class: 'card-head' },
      h('div', {}, h('h2', { text: cut.length === 1 ? t('cut_title1') : t('cut_title', { n: cut.length }) }), h('p', { class: 'muted small', text: t('cut_body') })),
      h('div', { class: 'actions' },
        cut.length > 1 ? h('button', { class: 'btn ghost', text: t('dismiss_all'), onclick: () => act('sessions/dismiss', ids) }) : null,
        cut.length > 1 ? h('button', { class: 'btn primary', text: t('restore_all'), disabled: !canStart || !st.claude.found, onclick: () => act('sessions/restore', ids, t('restore_started')) }) : null)),
    h('div', {}, cut.map((c) => h('div', { class: 'row' },
      h('div', { class: 'grow' },
        h('div', { class: 'title' }, h('span', { text: convTitle(c) }), h('span', { class: 'pill ' + (c.shutdown ? 'info' : 'warn'), text: c.shutdown ? t('cut_restart') : t('cut_stopped') }),
          c.working ? h('span', { class: 'pill off', text: t('cut_working') }) : null),
        h('div', { class: 'sub', title: c.cwd, text: `${baseName(c.cwd)} · ${c.account ? t('run_on', { a: c.account }) : ''} · ${ago(c.ended)}` })),
      h('div', { class: 'actions' },
        h('button', { class: 'btn ghost', text: t('dismiss_btn'), onclick: () => act('sessions/dismiss', [c.run_id]) }),
        h('button', { class: cut.length > 1 ? 'btn' : 'btn primary', text: t('restore_btn'), disabled: !canStart || !st.claude.found,
          onclick: () => act('sessions/restore', [c.run_id], t('restore_started')) }))))));
}

function runState(r) {
  const cls = { running: 'ok', stopping: 'info', switching: 'info', ending: 'info', waiting: 'warn', asking: 'warn' }[r.state] || 'off';
  return h('span', { class: 'pill ' + cls, text: t('run_' + r.state) });
}

function runRow(st, r) {
  const others = usableAccounts(st).filter((a) => a.id !== r.account && (!r.allowed || r.allowed.includes(a.id)));
  const waiting = r.state === 'waiting';
  // "Next account" needs one that is free now; while a switch is already
  // pending only a named account can change its target.
  const autoOk = (r.state === 'running' || waiting) && others.some((a) => a.status === 'ok');
  const retry = waiting && usableAccounts(st).some((a) => a.id === r.account);
  // The mode this run will apply: its own, or a stricter setting chosen since
  // (a run adopts a stricter mode before it switches, never a looser one).
  const rank = { auto: 0, confirm: 1, off: 2 };
  const runMode = r.switch_mode || st.config.switch_mode;
  const cfgMode = st.config.switch_mode;
  const confirmMode = (cfgMode && rank[cfgMode] > rank[runMode] ? cfgMode : runMode) === 'confirm';
  const sel = h('select', { 'aria-label': t('switch_btn'), onchange: async (e) => {
    const v = e.target.value; e.target.value = '';
    if (v === '') return;
    try {
      await api('sessions/switch', { run_id: r.run_id, target: v === '*' ? '' : v });
      // While waiting, a retry of the same account, or the first account of
      // a run, starts at once without a question.
      if (waiting && (v === r.account || !r.account)) toast(v === '*' ? t('switch_now_any') : t('switch_now', { a: v }));
      else toast(confirmMode ? t('switch_asked_confirm') : t('switch_asked'));
    } catch (err) { showError(err); }
  } }, h('option', { value: '', text: t('switch_btn') + '…' }),
  autoOk ? h('option', { value: '*', text: t('switch_auto') }) : null,
  others.map((a) => h('option', { value: a.id, text: t('switch_to', { a: a.id }) })),
  // While waiting for a reset, the current account can be tried again now.
  retry ? h('option', { value: r.account, text: t('retry_now', { a: r.account }) }) : null);
  sel.classList.add('auto-w');
  const canSwitch = (r.state === 'running' || r.state === 'stopping' || waiting) && (others.length > 0 || retry);
  const asking = r.state === 'asking';
  // At a glance: where, what state, which account, how full the context is,
  // what changed and Remote Control. The rest waits behind "Details".
  const open = !!S.ui.runOpen[r.run_id];
  const toggle = h('button', { class: 'btn ghost details-btn', 'aria-expanded': String(open), text: open ? t('details_less') : t('details_more'),
    onclick: () => { S.ui.runOpen[r.run_id] = !open; render(); } });
  const facts = [
    h('span', { class: 'fact strong', text: r.account ? t('run_on', { a: r.account }) : t('run_on_none') }),
    r.context != null ? contextFact(r.context) : null,
    changesFact(r),
    r.remote_control ? h('span', { class: 'pill info', title: t('rc_where', { a: r.account || '' }), text: t('rc_on') }) : null,
  ].filter(Boolean);
  return h('div', { class: 'row run' + (open ? ' open' : '') },
    h('div', { class: 'grow' },
      h('div', { class: 'title' }, h('span', { title: r.cwd, text: baseName(r.cwd) }), runState(r), claudeState(r)),
      h('div', { class: 'facts' }, facts),
      r.claude === 'waiting' && r.waiting_for ? h('div', { class: 'hint', text: t('cl_waiting_hint', { w: r.waiting_for }) }) : null,
      asking ? h('div', { class: 'hint', text: t('asking_hint') + ' ' + t('asking_stop_hint') }) : null,
      waiting ? h('div', { class: 'hint', text: r.account ? t('waiting_hint') : t('waiting_first') }) : null,
      open ? runDetails(r) : null),
    h('div', { class: 'actions' },
      r.window ? h('button', { class: 'btn', title: t('show_window_tip'), text: t('show_window'),
        onclick: () => api('sessions/focus', { run_id: r.run_id }).catch(showError) }) : null,
      canSwitch ? sel : null,
      // While the session window asks, baton reads only that window.
      asking || r.state === 'ending' ? null : h('button', { class: 'btn ghost danger', text: t('stop_btn'), onclick: () => confirmStop(r) }),
      toggle));
}

// runDetails is what a session row shows once opened.
function runDetails(r) {
  const c = r.changes;
  const line = (label, ...value) => h('div', { class: 'detail' }, h('span', { class: 'detail-label', text: label }), h('span', { class: 'detail-value' }, ...value));
  return h('div', { class: 'details' },
    line(t('d_path'), h('span', { class: 'mono', text: r.cwd })),
    line(t('d_started'), clock(r.started) + (r.model ? ' · ' + r.model : '')),
    c && c.error && c.error !== 'no_git' ? line(t('d_changes'), h('span', { class: 'err-inline', text: t('ch_error', { e: c.error }) })) : null,
    c && c.repo && !c.error ? line(t('d_changes'), c.files ? changeCount(c.files, c.added, c.removed) : t('ch_none')) : null,
    r.remote_control ? line(t('rc_on'), t('rc_where', { a: r.account || '' })) : null,
    h('div', { class: 'actions start' },
      c && c.repo && c.files ? h('button', { class: 'btn small-btn', text: t('ch_btn'), onclick: () => openChanges(r) }) : null,
      h('button', { class: 'btn small-btn', text: t('open_folder'), onclick: () => api('open', { what: 'folder', run_id: r.run_id }).catch(showError) }),
      h('button', { class: 'btn small-btn', text: t('open_log'), onclick: () => api('open', { what: 'run', run_id: r.run_id }).catch(showError) })));
}

// contextFact: how full the session's context window is, as a small meter.
function contextFact(p) {
  const pct = Math.max(0, Math.min(100, p));
  const fill = h('span', { class: 'fill ' + (pct >= 90 ? 'bad' : pct >= 70 ? 'warn' : 'ok') });
  fill.style.width = pct + '%'; // CSSOM: the page's CSP allows no inline style attributes
  return h('span', { class: 'fact', title: t('ctx', { p: Math.round(pct) }) }, t('ctx_short'),
    h('span', { class: 'meter mini', role: 'meter', 'aria-valuemin': '0', 'aria-valuemax': '100', 'aria-valuenow': String(Math.round(pct)), 'aria-label': t('ctx_short') }, fill),
    h('span', { class: 'nums', text: Math.round(pct) + '%' }));
}

// changeNums: lines added in green, removed in red.
function changeNums(added, removed) {
  return h('span', { class: 'nums' }, added >= 0 ? h('span', { class: 'plus', text: '+' + added }) : null, ' ',
    removed >= 0 ? h('span', { class: 'minus', text: '−' + removed }) : null);
}

function changeCount(n, added, removed) {
  return h('span', {}, t(n === 1 ? 'ch_files1' : 'ch_files', { n }), ' ', changeNums(added, removed));
}

// changesFact: the folder's uncommitted changes; a click lists them.
function changesFact(r) {
  const c = r.changes;
  if (!c || c.error || !c.repo) return null;
  if (!c.files) return h('span', { class: 'fact', text: t('ch_none') });
  return h('button', { class: 'fact link', title: t('ch_btn'), onclick: () => openChanges(r) }, changeNums(c.added, c.removed),
    h('span', { class: 'muted', text: t(c.files === 1 ? 'ch_files1' : 'ch_files', { n: c.files }) }));
}

// claudeState: what Claude Code itself is doing in the session.
function claudeState(r) {
  if (!r.claude || r.state !== 'running' && r.state !== 'stopping') return null;
  if (r.claude === 'busy') return h('span', { class: 'pill info' }, h('span', { class: 'spin' }), ' ', t('cl_busy'));
  if (r.claude === 'waiting') return h('span', { class: 'pill warn', text: t('cl_waiting') });
  return h('span', { class: 'pill off', text: t('cl_idle') });
}

// openChanges lists a session's changed files, the way the desktop app's
// changes panel does.
async function openChanges(r) {
  const body = h('div', {}, h('p', { class: 'muted small' }, h('span', { class: 'spin' }), ' ', t('ch_loading')));
  openDialog(h('h2', { text: t('ch_title', { f: baseName(r.cwd) }) }), h('p', { class: 'muted small', text: t('ch_note') }), body,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('open_folder'), onclick: () => api('open', { what: 'folder', run_id: r.run_id }).catch(showError) }),
      h('button', { class: 'btn primary', text: t('close'), onclick: closeDialog })));
  let c;
  try { c = await api('changes?run_id=' + encodeURIComponent(r.run_id)); } catch (e) { body.replaceChildren(h('p', { class: 'err', text: errText(e) })); return; }
  if (c.error) { body.replaceChildren(h('p', { class: 'err', text: c.error === 'no_git' ? t('ch_nogit') : t('ch_error', { e: c.error }) })); return; }
  if (!c.repo) { body.replaceChildren(h('p', { class: 'muted', text: t('ch_norepo') })); return; }
  if (!c.files.length) { body.replaceChildren(h('p', { class: 'muted', text: t('ch_none') })); return; }
  const cls = { modified: 'info', added: 'ok', untracked: 'ok', deleted: 'bad', renamed: 'info', conflict: 'warn' };
  body.replaceChildren(h('p', { class: 'small' }, changeCount(c.files.length, c.added, c.removed)),
    h('div', { class: 'list tall' }, c.files.map((f) => h('div', { class: 'item change' },
      h('span', { class: 'pill ' + (cls[f.status] || 'off'), text: t('gs_' + f.status) }),
      h('span', { class: 'grow mono', text: f.path }),
      h('span', { class: 'small' }, changeNums(f.added, f.removed))))));
}

function confirmStop(r) {
  const go = h('button', { class: 'btn primary danger', text: t('stop_go'), onclick: async () => {
    go.disabled = true;
    try { await api('sessions/stop', { run_id: r.run_id }); closeDialog(); toast(t('stop_asked')); } catch (e) { showError(e); } finally { go.disabled = false; }
  } });
  openDialog(h('h2', { text: t('stop_title') }), h('p', { text: t('stop_body') }), h('p', { class: 'small muted', text: r.cwd }),
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), go));
}

// ---- start dialog ------------------------------------------------------------------

function openDialog(...content) {
  const d = document.getElementById('dialog');
  d.replaceChildren(h('div', { class: 'dlg' }, ...content));
  if (!d.open) d.showModal();
  return d;
}
function closeDialog() { const d = document.getElementById('dialog'); if (d.open) d.close(); }

function openStart(pre) {
  const st = S.state;
  pre = pre || {};
  // trusted/checked come from the server's folder check; Start waits for it.
  const form = { folder: pre.folder || st.recent_folders[0] || '', account: '', mode: pre.mode || 'new', sel: pre.session_id ? { session_id: pre.session_id, where: pre.where } : null,
    trusted: false, checked: false, problem: '', trust: false, copy: false, // copy: the user's own tick
    rc: !!st.config.remote_control };
  const err = h('div', { class: 'err' });
  const folderInput = h('input', { type: 'text', value: form.folder, placeholder: 'C:\\…', oninput: (e) => { form.folder = e.target.value; folderChanged(); } });
  const trustBox = h('div', {});
  const convBox = h('div', {});
  const copyBox = h('div', {});
  let convs = [];
  let seq = 0;
  let convSeq = 0;
  let convsLoad = Promise.resolve(); // the newest list request; Start waits for it
  let convsStale = false; // the folder changed and its list has not been asked for yet
  let timer;

  function folderChanged() { form.checked = false; convsStale = true; clearTimeout(timer); timer = setTimeout(() => { checkFolder(); loadConvs(); }, 250); }
  let trustFor = ''; // the folder the trust tick was given for

  async function checkFolder() {
    const my = ++seq;
    form.checked = false;
    if (!cleanPath(form.folder)) { trustBox.replaceChildren(); return; }
    let info;
    try { info = await api('folder/info?path=' + encodeURIComponent(cleanPath(form.folder))); } catch (_) { return; }
    if (my !== seq) return;
    form.trusted = !!info.trusted;
    form.problem = info.ok ? '' : info.problem;
    form.checked = true;
    drawAccounts(info.ok ? info.allowed : null);
    if ((info.path || '') !== trustFor) { form.trust = false; trustFor = info.path || ''; } // a tick is for one folder
    if (!info.ok) { trustBox.replaceChildren(h('div', { class: 'small err', text: info.problem === 'long_folder' ? t('folder_long') : t('folder_bad') })); return; }
    if (info.trusted) { trustBox.replaceChildren(); return; }
    trustBox.replaceChildren(h('div', { class: 'tip' }, h('b', { text: t('trust_q') }), h('div', { class: 'small', text: t('trust_body') }),
      h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: form.trust, onchange: (e) => { form.trust = e.target.checked; } }), h('span', { text: t('trust_ok') }))));
  }

  // The conversation a start would continue; one that another Claude Code
  // has open can only go on in a copy.
  function target() {
    if (form.mode === 'continue') return convs[0] || null;
    if (form.mode === 'pick' && form.sel) return convs.find((c) => c.session_id === form.sel.session_id && c.where === form.sel.where) || null;
    return null;
  }
  const copyOf = (c) => !!c && (c.open || form.copy);
  function drawCopy() {
    const c = target();
    if (!c) { copyBox.replaceChildren(); return; }
    copyBox.replaceChildren(c.open ? h('div', { class: 'tip', text: t('conv_open_hint') }) : '',
      h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: copyOf(c), disabled: c.open, onchange: (e) => { form.copy = e.target.checked; } }),
        h('span', { text: t('copy_ok') })));
  }

  function loadConvs() { convsLoad = fetchConvs(); return convsLoad; }
  async function fetchConvs() {
    const my = ++convSeq;
    convsStale = false;
    if (form.mode === 'new') { convBox.replaceChildren(); copyBox.replaceChildren(); return; }
    convBox.replaceChildren(h('p', { class: 'muted small', text: t('conv_loading') }));
    const folder = cleanPath(form.folder);
    let got = [];
    try { got = folder ? await api('conversations?folder=' + encodeURIComponent(folder)) : []; } catch (_) { got = []; }
    if (my !== convSeq || folder !== cleanPath(form.folder) || form.mode === 'new') return;
    // A conversation chosen elsewhere (bring in, recent sessions) stays even
    // when it is not among the folder's newest.
    const c0 = pre.conv;
    if (c0 && sameFolder(c0.cwd, folder) && !got.some((c) => c.session_id === c0.session_id && c.where === c0.where)) got = [...got, c0];
    convs = got;
    if (form.sel && !convs.some((c) => c.session_id === form.sel.session_id && c.where === form.sel.where)) form.sel = null;
    drawCopy();
    if (!convs.length) { convBox.replaceChildren(h('p', { class: 'muted small', text: t('conv_none') })); return; }
    if (form.mode === 'continue') {
      convBox.replaceChildren(h('p', { class: 'muted small', text: t('conv_latest', { p: convTitle(convs[0]) }) }));
      return;
    }
    const list = h('div', { class: 'list', role: 'listbox' });
    const pick = (c, key) => {
      if (!isSel(c)) form.copy = false; // a tick is for one conversation
      form.sel = { session_id: c.session_id, where: c.where }; draw(); drawCopy();
      if (key) { const el = list.querySelector('.selected'); if (el) el.focus(); } // the redraw took the focus away
    };
    const isSel = (c) => !!form.sel && form.sel.session_id === c.session_id && form.sel.where === c.where;
    const draw = () => list.replaceChildren(...convs.map((c) => h('div', {
      class: 'item' + (isSel(c) ? ' selected' : ''), role: 'option', tabindex: '0', 'aria-selected': String(isSel(c)),
      onclick: () => pick(c), onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { pick(c, true); e.preventDefault(); } },
    }, convItem(c))));
    draw();
    convBox.replaceChildren(list);
    const chosen = list.querySelector('.selected');
    if (chosen) chosen.scrollIntoView({ block: 'nearest' });
  }

  const modeChoice = (value, label) => h('label', { class: 'choice' + (form.mode === value ? ' selected' : '') },
    h('input', { type: 'radio', name: 'mode', value, checked: form.mode === value, onchange: () => { form.mode = value; redrawModes(); loadConvs(); } }),
    h('span', { text: label }));
  const modes = h('div', {});
  function redrawModes() { modes.replaceChildren(modeChoice('new', t('conv_new')), modeChoice('continue', t('conv_continue')), modeChoice('pick', t('conv_pick'))); }
  redrawModes();

  const usable = usableAccounts(st);
  const account = h('select', { onchange: (e) => { form.account = e.target.value; } });
  const ruleNote = h('div', { class: 'small muted' });
  // A folder rule leaves only its accounts to choose from.
  function drawAccounts(allowed) {
    const list = allowed ? usable.filter((a) => allowed.includes(a.id)) : usable;
    if (form.account && !list.some((a) => a.id === form.account)) form.account = '';
    account.replaceChildren(h('option', { value: '', text: t('account_auto') }),
      ...list.map((a) => h('option', { value: a.id, selected: a.id === form.account,
        text: a.id + (a.email ? ` (${a.email})` : '') + (a.status === 'limited' ? ' — ' + t('st_limited', { t: clock(a.retry_at) }) : '') })));
    ruleNote.textContent = allowed ? t('start_rule', { a: allowed.join(', ') }) : '';
  }
  drawAccounts(null);

  const start = h('button', { class: 'btn primary', text: t('start_go'), onclick: async () => {
    err.textContent = '';
    if (!cleanPath(form.folder)) { err.textContent = t('choose_folder_first'); return; }
    start.disabled = true;
    try {
      // Whether a copy is needed comes from the folder's list: let it arrive.
      // Wait for the list of the folder and mode as they are now (the user can
      // change them meanwhile); a list that was replaced is not good enough.
      for (;;) {
        if (convsStale) { clearTimeout(timer); loadConvs(); } // the folder check follows below
        const p = convsLoad;
        await p;
        if (p === convsLoad && !convsStale) break;
      }
      if (form.mode === 'pick' && !form.sel) { err.textContent = t('pick_conv_first'); return; }
      // The trust question must be answered before a start, not raced.
      if (!form.checked) { clearTimeout(timer); await checkFolder(); }
      if (!form.checked) { err.textContent = t('folder_bad'); return; }
      if (form.problem) { err.textContent = form.problem === 'long_folder' ? t('folder_long') : t('folder_bad'); return; }
      if (!form.trusted && !form.trust) { err.textContent = t('trust_needed'); return; }
      await api('sessions/start', { folder: cleanPath(form.folder), account: form.account, mode: form.mode === 'pick' ? 'resume' : form.mode,
        session_id: form.sel ? form.sel.session_id : '', where: form.sel ? form.sel.where : '', trust: form.trust,
        copy: form.mode !== 'new' && copyOf(target()), remote_control: form.rc });
      closeDialog(); toast(t('start_done') + ' ' + t('first_start'));
    } catch (e) {
      if (e.code === 'needs_mode') { closeDialog(); openNotice(() => openStart(pre)); return; }
      err.textContent = errText(e);
    } finally { start.disabled = false; }
  } });

  openDialog(h('h2', { text: t('start_title') }),
    h('label', { class: 'field' }, h('span', { text: t('start_folder') }),
      h('div', { class: 'inline' }, folderInput, h('button', { class: 'btn', text: t('browse'), onclick: async (e) => {
        e.preventDefault();
        try { const r = await api('folder/pick', {}); if (!r.cancelled) { form.folder = r.path; folderInput.value = r.path; checkFolder(); loadConvs(); } } catch (x) { showError(x); }
      } })),
      st.recent_folders.length ? h('div', { class: 'chips' }, st.recent_folders.map((f) => h('button', { type: 'button', title: f, text: baseName(f),
        onclick: () => { form.folder = f; folderInput.value = f; checkFolder(); loadConvs(); } }))) : null),
    trustBox,
    h('label', { class: 'field' }, h('span', { text: t('start_account') }), account, ruleNote),
    h('div', { class: 'field' }, h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: form.rc, onchange: (e) => { form.rc = e.target.checked; } }),
      h('span', {}, h('b', { text: t('start_rc') }), h('div', { class: 'small muted', text: t('start_rc_hint') })))),
    h('div', { class: 'field' }, h('span', { class: 'muted small', text: t('start_conv') }), modes, convBox, copyBox),
    err,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), start));
  checkFolder();
  loadConvs();
}

// ---- bring in a conversation ---------------------------------------------------------

function convTitle(c) { return c.title || c.first_prompt || t('conv_untitled'); }

function sameFolder(a, b) {
  const norm = (p) => cleanPath(p).replace(/[\\/]+$/, '').toLowerCase();
  return norm(a) === norm(b);
}

// sourceName says where a conversation is kept: on an account, or in the
// user's own Claude Code and what made it there.
function sourceName(c) {
  if (c.where && c.where !== 'claude-code') return t('conv_where', { a: c.where });
  return t({ 'claude-desktop': 'src_desktop', cli: 'src_cli', 'claude-vscode': 'src_vscode' }[c.source] || 'src_other');
}

function convItem(c, withFolder) {
  return h('div', { class: 'grow' },
    h('div', { class: 'title' }, h('span', { text: convTitle(c) }),
      c.open ? h('span', { class: 'pill warn', text: t('conv_open') }) : null,
      c.folder_ok === false ? h('span', { class: 'pill off', text: c.folder_problem === 'gone' ? t('conv_gone') : t('conv_unusable') }) : null),
    h('div', { class: 'sub small muted', title: c.cwd || '', text: [withFolder ? baseName(c.cwd) : '', ago(c.modified), sourceName(c)].filter(Boolean).join(' · ') }));
}

// openImport lists the conversations of every folder (the desktop app's
// sessions included) to continue one of them in baton.
async function openImport() {
  const CAP = 200; // what the server lists at most
  let convs = null;
  let failed = '';
  let capped = false;
  let sel = null;
  let q = '';
  const list = h('div', { class: 'list tall', role: 'listbox', 'aria-label': t('import_title') });
  const hint = h('div', {});
  const more = h('div', {});
  let note = ''; // the outcome of Check again
  const go = h('button', { class: 'btn primary', text: t('import_go'), disabled: true, onclick: () => {
    if (!sel || sel.folder_ok === false) return;
    closeDialog();
    startSession({ folder: sel.cwd, mode: 'pick', session_id: sel.session_id, where: sel.where, conv: sel });
  } });
  const search = h('input', { type: 'text', placeholder: t('import_search'), 'aria-label': t('import_search'), oninput: (e) => { q = e.target.value; draw(); } });
  const pick = (c, key) => {
    if (key && sel === c) { go.click(); return; } // Enter on the chosen one continues it
    if (sel !== c) note = '';
    sel = c; draw();
    if (key) { const el = list.querySelector('.selected'); if (el) el.focus(); } // the redraw took the focus away
  };
  function draw() {
    if (!convs) { list.replaceChildren(h('p', { class: 'muted small pad' }, h('span', { class: 'spin' }), ' ', t('conv_loading'))); return; }
    if (failed) { list.replaceChildren(h('p', { class: 'err pad', text: failed })); return; }
    const words = q.toLowerCase().split(/\s+/).filter(Boolean);
    const shown = convs.filter((c) => {
      const hay = [c.title, c.first_prompt, c.cwd].join(' ').toLowerCase();
      return words.every((w) => hay.includes(w));
    });
    if (sel && !shown.includes(sel)) sel = null;
    if (!shown.length) list.replaceChildren(h('p', { class: 'muted small pad', text: convs.length ? t('import_no_match') : t('import_none') }));
    else list.replaceChildren(...shown.map((c) => h('div', {
      class: 'item' + (sel === c ? ' selected' : ''), role: 'option', tabindex: '0', 'aria-selected': String(sel === c),
      onclick: () => pick(c), ondblclick: () => { sel = c; go.click(); },
      onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { pick(c, true); e.preventDefault(); } },
    }, convItem(c, true))));
    const st = S.state;
    go.disabled = !sel || sel.folder_ok === false || !st.claude.found || !usableAccounts(st).length;
    go.textContent = sel && sel.open && sel.folder_ok !== false ? t('import_go_copy') : t('import_go');
    if (sel && sel.folder_ok === false) {
      hint.replaceChildren(h('div', { class: 'tip', text: sel.folder_problem === 'gone' ? t('conv_gone_hint') : t('conv_bad_hint') }));
    } else if (sel && sel.open) {
      hint.replaceChildren(heldWarning(sel, recheck, note));
    } else {
      hint.replaceChildren(...(note ? [h('div', { class: 'tip ok-tip', text: note })] : []));
    }
    more.replaceChildren(...(capped ? [h('p', { class: 'muted small mt6', text: t('import_more') })] : []));
  }
  draw();
  openDialog(h('h2', { text: t('import_title') }), h('p', { class: 'muted small', text: t('import_sub') }),
    h('div', { class: 'field' }, search), list, more, hint,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), go));
  search.focus();
  async function load() {
    let got = [];
    failed = '';
    try { got = await api('conversations?all=1'); } catch (e) { failed = errText(e); }
    capped = got.length >= CAP;
    // Sessions running in baton right now are on the Sessions tab already.
    const running = new Set(S.state.runs.map((r) => r.session_id));
    convs = got.filter((c) => !running.has(c.session_id));
    if (sel) sel = convs.find((c) => c.session_id === sel.session_id && c.where === sel.where) || null;
  }
  // Check again: the user closed the other app; read the list anew and keep
  // the conversation chosen.
  async function recheck() {
    const was = sel;
    await load();
    note = !was ? '' : sel && sel.open ? t('held_still') : sel ? t('held_free') : '';
    draw();
  }
  await load();
  draw();
}

// heldWarning: the conversation is open in another app. Close that first to
// go on with the same conversation, or go on in a copy, which does not sync.
function heldWarning(c, recheck, note) {
  const app = t({ 'claude-desktop': 'held_desktop', cli: 'held_cli', 'claude-vscode': 'held_vscode' }[c.holder] || 'held_other');
  const close = t({ 'claude-desktop': 'held_close_desktop', cli: 'held_close_cli' }[c.holder] || 'held_close_other');
  const btn = h('button', { class: 'btn', text: t('held_recheck'), onclick: async () => {
    btn.disabled = true;
    try { await recheck(); } finally { btn.disabled = false; }
  } });
  return h('div', { class: 'tip warn-tip' },
    h('b', { text: '⚠ ' + t('held_title', { app }) }),
    h('div', { class: 'mt6', text: close }),
    h('div', { class: 'mt6', text: t('held_copy') }),
    h('div', { class: 'actions start mt6' }, btn, note ? h('span', { class: 'small', text: note }) : null));
}

// ---- accounts ------------------------------------------------------------------------

function accountStatus(a) {
  if (a.signing_in) return h('span', { class: 'pill info' }, h('span', { class: 'spin' }), ' ', t('st_signing'));
  if (!a.enabled) return h('span', { class: 'pill off', text: a.disabled_as_duplicate ? t('st_dup') : t('st_off') });
  const map = { ok: ['ok', t('st_ok')], limited: ['warn', t('st_limited', { t: a.retry_at ? clock(a.retry_at) : '' })],
    needs_login: ['bad', t('st_needs_login')], signed_out: ['bad', t('st_signed_out')], blocked: ['bad', t('st_blocked')] };
  const [cls, label] = map[a.status] || ['off', a.status];
  return h('span', { class: 'pill ' + cls, text: label, title: a.detail || '' });
}

function viewAccounts(st) {
  const head = h('div', { class: 'card-head' },
    h('div', {}, h('h2', { text: t('accounts_title') }), h('p', { class: 'muted small', text: t('accounts_sub') })),
    h('div', { class: 'actions' },
      st.accounts.length ? h('button', { class: 'btn', disabled: st.checking, onclick: checkLogins },
        st.checking ? h('span', { class: 'spin' }) : null, st.checking ? ' ' + t('checking') : t('check_logins')) : null,
      h('button', { class: 'btn primary', text: t('add_account'), onclick: () => openAdd() })));
  if (!st.accounts.length) {
    return [...signinCards(st), h('div', { class: 'card' }, head, h('div', { class: 'empty' }, h('h3', { text: t('no_accounts_title') }), h('p', { text: t('no_accounts_body') }),
      h('button', { class: 'btn primary', text: t('add_account'), onclick: () => openAdd() })))];
  }
  const rows = st.accounts.map((a, i) => accountRow(st, a, i));
  return [...signinCards(st), h('div', { class: 'card' }, head, h('div', {}, rows), st.accounts.length > 1 ? h('p', { class: 'muted small', text: t('order_hint') }) : null)];
}

function accountRow(st, a, i) {
  const needsLogin = a.status === 'needs_login' || a.status === 'signed_out' || a.status === 'blocked' || a.disabled_as_duplicate;
  const loginLine = a.status === 'ok' || a.status === 'limited'
    ? `${t('kept')} · ${a.checked_at ? t('checked_at', { t: ago(a.checked_at) }) : t('not_checked')}`
    : (a.checked_at ? t('checked_at', { t: ago(a.checked_at) }) : '');
  const toggle = h('label', { class: 'switch-wrap', title: t('enabled_label') }, h('span', { class: 'small muted', text: t('use_short') }),
    h('span', { class: 'switch' },
      h('input', { type: 'checkbox', checked: a.enabled, 'aria-label': t('enabled_label') + ' ' + a.id,
        onchange: (e) => accountCmd(e.target.checked ? 'enable' : 'disable', a.id) }), h('span')));
  const otherSignedIn = st.accounts.some((o) => o.id !== a.id && o.logged_in);
  return h('div', { class: 'row' },
    h('div', { class: 'grow' },
      h('div', { class: 'title' }, a.id, accountStatus(a), a.in_use ? h('span', { class: 'pill info', text: t('in_use'), title: t('in_use_hint') }) : null),
      h('div', { class: 'sub', text: [a.email, a.plan, a.org].filter(Boolean).join(' · ') || '—' }),
      loginLine ? h('div', { class: 'sub', text: loginLine }) : null,
      usageBlock(a),
      a.status === 'blocked' && !a.disabled_as_duplicate ? h('div', { class: 'small', text: t('blocked_hint') }) : null,
      a.disabled_as_duplicate ? h('div', { class: 'small', text: t('dup_hint') }) : null),
    h('div', { class: 'actions' },
      // A duplicate cannot be turned on: only a correct sign-in clears it (and the block).
      a.status === 'blocked' && !a.disabled_as_duplicate ? h('button', { class: 'btn', text: t('unblock'), onclick: () => accountCmd('enable', a.id) }) : null,
      needsLogin && !a.signing_in ? h('button', { class: 'btn primary', text: t('login_again'), disabled: a.in_use, title: a.in_use ? t('in_use_hint') : '',
        onclick: () => openAdd({ name: a.id, email: retryEmail(st, a), login: true, private: otherSignedIn }) }) : null,
      h('button', { class: 'btn ghost', text: '▲', title: t('up'), 'aria-label': t('up'), disabled: i === 0, onclick: () => move(a.id, 'up') }),
      h('button', { class: 'btn ghost', text: '▼', title: t('down'), 'aria-label': t('down'), disabled: i === st.accounts.length - 1, onclick: () => move(a.id, 'down') }),
      toggle,
      h('button', { class: 'btn ghost danger', text: t('remove'), onclick: () => openRemove(a.id) })));
}

// usageBlock shows an account's 5-hour and weekly limits as a baton session
// on it last saw them.
function usageBlock(a) {
  const u = a.usage;
  if (!u) return a.logged_in && a.enabled ? h('div', { class: 'sub small', text: t('use_unknown') }) : null;
  const one = (label, w) => {
    if (!w) return null;
    const pct = Math.max(0, Math.min(100, w.used));
    const fill = h('span', { class: 'fill ' + (pct >= 90 ? 'bad' : pct >= 70 ? 'warn' : 'ok') });
    fill.style.width = pct + '%'; // CSSOM: the page's CSP allows no inline style attributes
    return h('div', { class: 'usage-row' }, h('span', { class: 'usage-label', text: label }),
      h('span', { class: 'meter', role: 'meter', 'aria-valuemin': '0', 'aria-valuemax': '100', 'aria-valuenow': String(Math.round(pct)), 'aria-label': label }, fill),
      h('span', { class: 'usage-num', text: Math.round(pct) + '%' }),
      h('span', { class: 'usage-reset', text: t('use_resets', { t: clock(w.resets_at) }) }));
  };
  return h('div', { class: 'usage' }, one(t('use_5h'), u.five_hour), one(t('use_7d'), u.seven_day),
    h('div', { class: 'sub small', text: t('use_asof', { t: ago(u.updated) }) }));
}

// retryEmail is the address a new sign-in of a pre-fills: its own, but not
// when it is a duplicate, whose stored address is the other account's and
// would lead right back to it.
function retryEmail(st, a) {
  if (!a || !a.email || a.disabled_as_duplicate) return '';
  if (a.enabled) return a.email; // a duplicate can never be on: this is the original
  const e = a.email.toLowerCase();
  return st.accounts.some((o) => o.id !== a.id && o.logged_in && (o.email || '').toLowerCase() === e) ? '' : a.email;
}

async function checkLogins() {
  try { await api('accounts/check', {}); await refresh(); } catch (e) { showError(e); }
}

async function accountCmd(cmd, name) {
  try { await api('accounts/' + cmd, { name }); } catch (e) { showError(e); }
  refresh();
}

async function move(name, dir) {
  try { await api('accounts/move', { name, dir }); } catch (e) { showError(e); }
  refresh();
}


function openAdd(prefill) {
  const st = S.state;
  const err = h('div', { class: 'err' });
  let nameTouched = !!(prefill && prefill.name);
  const name = h('input', { type: 'text', value: (prefill && prefill.name) || (st.accounts.length ? '' : 'personal'), maxlength: '32',
    autocomplete: 'off', spellcheck: 'false', oninput: (e) => { e.target.value = e.target.value.toLowerCase(); nameTouched = true; } });
  const email = h('input', { type: 'text', value: (prefill && prefill.email) || '', placeholder: 'name@example.com', autocomplete: 'off', spellcheck: 'false',
    oninput: (e) => {
      if (nameTouched || prefill) return;
      const local = e.target.value.split('@')[0].toLowerCase().replace(/[^a-z0-9-]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 32);
      if (local) name.value = local;
    } });
  // InPrivate by default once the browser is likely signed in to another account.
  const priv = h('input', { type: 'checkbox', checked: prefill ? !!prefill.private : st.accounts.some((a) => a.logged_in) });
  const go = h('button', { class: 'btn primary', text: t('add_go'), onclick: async () => {
    const v = name.value.trim();
    if (!/^[a-z0-9][a-z0-9-]{0,31}$/.test(v)) { err.textContent = t('bad_name'); return; }
    go.disabled = true;
    try {
      const login = prefill && prefill.login;
      await api(login ? 'accounts/login' : 'accounts/add', { name: v, email: email.value.trim(), private: priv.checked });
      S.ui.signinPrivate = priv.checked;
      closeDialog(); S.tab = 'accounts'; S.lastJSON = ''; refresh();
    } catch (e) { err.textContent = errText(e); }
    finally { go.disabled = false; }
  } });
  openDialog(h('h2', { text: prefill && prefill.login ? t('login_again') + ': ' + prefill.name : t('add_title') }),
    h('label', { class: 'field' }, h('span', { text: t('add_email') }), email, h('div', { class: 'small muted', text: t('add_email_hint') })),
    prefill && prefill.login ? null : h('label', { class: 'field' }, h('span', { text: t('add_name') }), name, h('div', { class: 'small muted', text: t('add_name_hint') })),
    h('label', { class: 'check' }, priv, h('span', { text: t('add_private') })),
    h('p', { class: 'small', text: t('add_how') }),
    err,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), go));
  (prefill && prefill.login ? email : (st.accounts.length ? email : name)).focus();
}

// The sign-in in progress (steps, InPrivate button) and finished sign-ins.
function signinCards(st) {
  const out = [];
  for (const j of st.jobs) {
    if (j.kind !== 'add' && j.kind !== 'login') continue;
    if (j.running) {
      const steps = S.ui.signinPrivate ? t('signin_steps_private') : t('signin_steps_normal');
      out.push(h('div', { class: 'card signin' },
        h('h3', {}, h('span', { class: 'spin' }), ' ', t('signin_title', { a: j.target })),
        h('ol', { class: 'steps' }, steps.map((x) => h('li', { text: x }))),
        S.ui.signinPrivate ? h('div', { class: 'actions start' }, h('button', { class: 'btn primary', text: t('open_private'), onclick: openPrivate })) : null,
        h('p', { class: 'small muted', text: t('signin_close_hint') })));
      continue;
    }
    if (S.ui.dismissed[j.id]) continue;
    out.push(j.result ? resultCard(st, j) : unfinishedCard(st, j));
  }
  return out;
}

// A sign-in window that ended without writing its outcome (closed, crashed).
function unfinishedCard(st, j) {
  const acct = st.accounts.find((a) => a.id === j.target);
  const priv = st.accounts.some((o) => o.id !== j.target && o.logged_in);
  return h('div', { class: 'banner warn' }, h('div', { class: 'grow', text: t('res_unfinished', { a: j.target }) }),
    h('button', { class: 'btn primary', text: t('retry'), onclick: () => {
      S.ui.dismissed[j.id] = true;
      openAdd({ name: j.target, email: retryEmail(st, acct), login: !!acct, private: priv });
    } }),
    h('button', { class: 'btn ghost', text: t('dismiss'), onclick: () => { S.ui.dismissed[j.id] = true; render(); } }));
}

function resultCard(st, j) {
  const r = j.result;
  const acct = st.accounts.find((a) => a.id === r.account);
  const lines = [];
  let cls = 'ok';
  const again = (privateMode) => h('button', { class: 'btn primary', text: privateMode ? t('retry_private') : t('retry'),
    onclick: () => {
      S.ui.dismissed[j.id] = true;
      // A duplicate's address is the account it wrongly signed in as.
      const email = r.code === 'duplicate' ? '' : acct ? retryEmail(st, acct) : (r.email || '');
      openAdd({ name: r.account, email, login: !!acct, private: privateMode });
    } });
  const priv = st.accounts.some((o) => o.id !== r.account && o.logged_in);
  let action = null;
  switch (r.code) {
    case 'ok':
      lines.push(r.plan ? t('res_ok_plan', { e: r.email || r.account, p: r.plan }) : t('res_ok', { e: r.email || r.account }));
      if (r.still_disabled) lines.push(t('res_still_off'));
      if (r.not_subscribed) { lines.push(t('res_api')); cls = 'warn'; }
      break;
    case 'duplicate':
      cls = 'bad'; lines.push(t('res_duplicate', { a: r.account, d: r.duplicate_of, e: r.email || '' })); action = again(true); break;
    case 'busy':
      cls = 'warn'; lines.push(t('res_busy', { a: r.account })); break;
    case 'login_failed':
      cls = 'bad'; lines.push(t('res_login_failed', { a: r.account })); action = again(priv); break;
    case 'not_signed_in':
      cls = 'bad'; lines.push(t('res_not_signed_in', { a: r.account })); action = again(priv); break;
    default:
      cls = 'bad'; lines.push(t('res_error', { a: r.account, m: r.message || '' })); action = again(priv);
  }
  return h('div', { class: 'banner ' + (cls === 'ok' ? 'info' : cls) },
    h('div', { class: 'grow' }, lines.map((x) => h('div', { text: x }))),
    action, h('button', { class: 'btn ghost', text: t('dismiss'), onclick: () => { S.ui.dismissed[j.id] = true; render(); } }));
}

async function openPrivate() {
  try { await api('signin/private', {}); toast(t('private_opened')); }
  catch (e) { if (e.code === 'no_url') toast(t('private_no_url'), true); else showError(e); }
}

function openRemove(id) {
  const err = h('div', { class: 'err' });
  const go = h('button', { class: 'btn primary danger', text: t('remove_go'), onclick: () => doRemove() });
  async function doRemove() {
    go.disabled = true;
    try {
      await api('accounts/remove', { name: id });
      closeDialog(); toast(t('removed', { a: id })); refresh();
    } catch (e) {
      err.textContent = errText(e);
    } finally { go.disabled = false; }
  }
  openDialog(h('h2', { text: t('remove_title', { a: id }) }), h('p', { text: t('remove_body') }), err,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), go));
}

// ---- notice / onboarding ---------------------------------------------------------------

function noticeBlock() {
  return h('div', { class: 'notice' }, h('ul', {}, t('notice_points').map((p) => h('li', { text: p }))));
}

function modePicker(form, onChange) {
  const box = h('div', {});
  const draw = () => box.replaceChildren(...['confirm', 'auto', 'off'].map((m) => h('label', { class: 'choice' + (form.mode === m ? ' selected' : '') },
    h('input', { type: 'radio', name: 'switchmode', value: m, checked: form.mode === m, onchange: () => { form.mode = m; draw(); if (onChange) onChange(); } }),
    h('span', {}, h('b', { text: t('mode_' + m) }), h('span', { class: 'small muted', text: t('mode_' + m + '_desc') })))));
  draw();
  return box;
}

// openNotice shows the risk notice and the mode choice (also the last step of
// onboarding). Switching on needs the box ticked; the server checks it too.
function openNotice(after, mode) {
  const st = S.state;
  const form = { mode: mode || st.config.switch_mode || 'confirm', ack: st.config.risk_acknowledged };
  const err = h('div', { class: 'err' });
  const ack = h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: form.ack, onchange: (e) => { form.ack = e.target.checked; } }),
    h('span', { text: t('notice_ack') }));
  const save = h('button', { class: 'btn primary', text: after ? t('finish') : t('save'), onclick: async () => {
    err.textContent = '';
    if (form.mode !== 'off' && !form.ack) { err.textContent = t('ack_needed'); return; }
    try { await api('config', { key: 'switch_mode', value: form.mode, acknowledge: form.ack }); closeDialog(); await refresh(); if (after) after(); }
    catch (e) { err.textContent = errText(e); }
  } });
  openDialog(h('h2', { text: t('notice_title') }), noticeBlock(), ack, h('h3', { text: t('choose_mode') }), modePicker(form), err,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), save));
}

function maybeOnboard() {
  const st = S.state;
  if (S.onboardingShown || document.getElementById('dialog').open) return;
  if (st.config.switch_mode) return;
  S.onboardingShown = true;
  let step = 0;
  const steps = [
    () => [h('h2', { text: t('ob_welcome_title') }), h('ul', { class: 'steps' }, t('ob_welcome_points').map((p) => h('li', { text: p })))],
    () => {
      const s = S.state;
      let msg = t('ob_claude_wait');
      if (!s.claude_pending) msg = !s.claude.found ? t('ob_claude_missing') : s.claude.source === 'desktop'
        ? t('ob_claude_desktop', { v: s.claude.version }) : t('ob_claude_cli', { v: s.claude.version });
      return [h('h2', { text: t('ob_claude_title') }), h('p', { text: msg }), s.claude.error && !s.claude.found ? h('p', { class: 'small muted', text: s.claude.error }) : null];
    },
  ];
  const draw = () => {
    const bar = h('div', { class: 'progress' }, [0, 1, 2].map((i) => h('i', { class: i <= step ? 'on' : '' })));
    openDialog(bar, ...steps[step](), h('div', { class: 'foot' },
      step > 0 ? h('button', { class: 'btn left', text: t('back'), onclick: () => { step--; draw(); } }) : null,
      h('button', { class: 'btn primary', text: t('next'), onclick: () => {
        if (step < steps.length - 1) { step++; draw(); return; }
        openNotice(() => { if (!S.state.accounts.length) { S.tab = 'accounts'; render(); openAdd(); } });
      } })));
  };
  draw();
}

// ---- settings ----------------------------------------------------------------------------

async function setConfig(key, value, acknowledge, silent) {
  try {
    await api('config', { key, value: String(value), acknowledge: !!acknowledge });
    if (!silent) toast(t('saved'));
    S.lastJSON = '';
    await refresh();
    return true;
  } catch (e) {
    if (e.code === 'needs_ack') { openNotice(); return false; }
    showError(e);
    return false;
  }
}

function viewSettings(st) {
  const c = st.config;
  const modeForm = { mode: c.switch_mode || '' }; // nothing is drawn as chosen until it is
  const modes = modePicker(modeForm, async () => {
    if (modeForm.mode !== 'off' && !c.risk_acknowledged) { openNotice(undefined, modeForm.mode); return; }
    await setConfig('switch_mode', modeForm.mode);
  });
  const exhausted = ['wait', 'stop'].map((v) => h('label', { class: 'choice' + (c.on_exhausted === v ? ' selected' : '') },
    h('input', { type: 'radio', name: 'exhausted', checked: c.on_exhausted === v, onchange: () => setConfig('on_exhausted', v) }),
    h('span', { text: t('ex_' + v) })));
  const notifyChoices = ['all', 'important', 'off'].map((v) => h('label', { class: 'choice' + (c.notifications === v ? ' selected' : '') },
    h('input', { type: 'radio', name: 'notify', checked: c.notifications === v, onchange: () => setConfig('notifications', v) }),
    h('span', { text: t('notify_' + v) })));
  const testNotify = h('button', { class: 'btn', text: t('notify_test'), onclick: async () => {
    testNotify.disabled = true;
    try { await api('notify/test', {}); toast(t('notify_sent')); } catch (e) { showError(e); } finally { testNotify.disabled = false; }
  } });
  const num = (key, label) => {
    const input = h('input', { type: 'number', min: '1', value: String(c[key]) });
    return h('label', { class: 'field' }, h('span', { text: label }),
      h('div', { class: 'inline' }, input, h('button', { class: 'btn', text: t('apply'), onclick: () => {
        if (!/^[1-9][0-9]*$/.test(input.value.trim())) { toast(t('num_bad'), true); return; }
        setConfig(key, input.value.trim());
      } })));
  };
  const prompt = h('textarea', { text: c.continue_prompt });
  const share = h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: c.share_user_config, onchange: (e) => setConfig('share_user_config', e.target.checked) }),
    h('span', { text: t('set_share') }));
  const bundle = h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: c.allow_desktop_bundle, onchange: (e) => setConfig('allow_desktop_bundle', e.target.checked) }),
    h('span', { text: t('set_claude_bundle') }));

  const advanced = h('details', { open: S.ui.advancedOpen, ontoggle: (e) => { S.ui.advancedOpen = e.target.open; } }, h('summary', { text: t('set_advanced') }), h('div', { class: 'mt12' },
    num('cooldown_seconds', t('set_cooldown')), num('max_switches_per_hour', t('set_max')), num('exhausted_retry_minutes', t('set_retry')),
    h('label', { class: 'field' }, h('span', { text: t('set_prompt') }), prompt,
      h('div', { class: 'mt6' }, h('button', { class: 'btn', text: t('apply'), onclick: () => setConfig('continue_prompt', prompt.value) }))),
    share,
    h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: c.remote_control, onchange: (e) => setConfig('remote_control', e.target.checked) }),
      h('span', { text: t('set_rc') })),
    h('h3', { text: t('set_claude'), class: 'mt14' }),
    h('p', { class: 'small muted', text: st.claude.found ? st.claude.path : (st.claude.error || '') }),
    st.claude.found && st.claude.old_cli ? h('p', { class: 'small muted', text: t('claude_old_cli_skipped', { v: st.claude.old_cli }) }) : null,
    h('label', { class: 'field' }, h('span', { text: t('set_claude_which') }),
      h('select', { onchange: (e) => setConfig('claude_path', e.target.value) },
        h('option', { value: 'auto', text: t('claude_which_auto'), selected: !c.claude_path }),
        h('option', { value: 'desktop', text: t('claude_which_desktop'), selected: c.claude_path === 'desktop' }),
        c.claude_path && c.claude_path !== 'desktop' ? h('option', { value: c.claude_path, text: c.claude_path, selected: true }) : null),
      h('div', { class: 'small muted', text: t('claude_which_note') })),
    c.claude_path !== 'desktop' ? bundle : null,
    c.claude_path && c.claude_path !== 'desktop' ? h('div', { class: 'actions start' }, h('span', { class: 'small mono', text: c.claude_path }),
      h('button', { class: 'btn', text: t('set_claude_auto'), onclick: () => setConfig('claude_path', 'auto') })) : null,
    h('p', { class: 'small muted', text: t('claude_custom_cli') })));
  const rules = h('div', { class: 'card' },
    h('div', { class: 'card-head' }, h('div', {}, h('h3', { text: t('set_rules') }), h('p', { class: 'small muted', text: t('set_rules_hint') })),
      h('button', { class: 'btn', text: t('rule_add'), disabled: !st.accounts.length, onclick: () => openRule(st) })),
    st.rules.length ? h('div', {}, st.rules.map((r) => h('div', { class: 'row' },
      h('div', { class: 'grow' }, h('div', { class: 'title mono', text: r.folder }),
        h('div', { class: 'facts' }, r.accounts.map((a) => h('span', { class: 'pill ' + ((r.missing || []).includes(a) ? 'bad' : 'info'), text: a })),
          r.missing && r.missing.length ? h('span', { class: 'small err-inline', text: t('rule_missing', { a: r.missing.join(', ') }) }) : null)),
      h('div', { class: 'actions' }, h('button', { class: 'btn', text: t('rule_edit'), onclick: () => openRule(st, r) }),
        h('button', { class: 'btn ghost danger', text: t('rule_remove'), onclick: async () => {
          try { await api('rules/remove', { folder: r.folder }); toast(t('rule_removed')); S.lastJSON = ''; await refresh(); } catch (e) { showError(e); }
        } })))))
      : h('p', { class: 'muted', text: t('rules_none') }));
  return [h('div', { class: 'card' }, h('h2', { text: t('settings_title') }),
    h('h3', { text: t('set_mode'), class: 'mt12' }), h('p', { class: 'small muted', text: t('set_mode_hint') }), modes,
    h('h3', { text: t('set_exhausted'), class: 'mt16' }), exhausted,
    h('h3', { text: t('set_notify'), class: 'mt16' }), h('p', { class: 'small muted', text: t('set_notify_hint') }), notifyChoices,
    h('div', { class: 'actions start mt6' }, testNotify),
    h('label', { class: 'check mt14' }, h('input', { type: 'checkbox', checked: c.keep_awake, onchange: (e) => setConfig('keep_awake', e.target.checked) }),
      h('span', { text: t('set_awake') })),
    h('p', { class: 'small muted', text: t('set_awake_hint') }),
    h('div', { class: 'mt16' }, advanced)), rules];
}

// openRule adds a folder rule, or changes one.
function openRule(st, rule) {
  const form = { folder: rule ? rule.folder : '', accounts: new Set(rule ? rule.accounts : []) };
  const err = h('div', { class: 'err' });
  const folderInput = h('input', { type: 'text', value: form.folder, placeholder: 'C:\\…', disabled: !!rule, oninput: (e) => { form.folder = e.target.value; } });
  const boxes = st.accounts.map((a) => h('label', { class: 'check' },
    h('input', { type: 'checkbox', checked: form.accounts.has(a.id), onchange: (e) => { if (e.target.checked) form.accounts.add(a.id); else form.accounts.delete(a.id); } }),
    h('span', { text: a.id + (a.email ? ` (${a.email})` : '') })));
  const save = h('button', { class: 'btn primary', text: t('rule_save'), onclick: async () => {
    err.textContent = '';
    if (!cleanPath(form.folder)) { err.textContent = t('choose_folder_first'); return; }
    if (!form.accounts.size) { err.textContent = t('err_rule_empty'); return; }
    save.disabled = true;
    try {
      const keep = st.accounts.map((a) => a.id).filter((id) => form.accounts.has(id));
      await api('rules/set', { folder: cleanPath(form.folder), accounts: keep });
      closeDialog(); toast(t('rule_saved')); S.lastJSON = ''; await refresh();
    } catch (e) { err.textContent = e.code === 'bad_folder' ? t('folder_bad') : errText(e); } finally { save.disabled = false; }
  } });
  openDialog(h('h2', { text: t('rule_title') }),
    h('label', { class: 'field' }, h('span', { text: t('rule_folder') }),
      h('div', { class: 'inline' }, folderInput, rule ? null : h('button', { class: 'btn', text: t('browse'), onclick: async (e) => {
        e.preventDefault();
        try { const r = await api('folder/pick', {}); if (!r.cancelled) { form.folder = r.path; folderInput.value = r.path; } } catch (x) { showError(x); }
      } }))),
    h('div', { class: 'field' }, h('span', { text: t('rule_accounts') }), ...boxes),
    h('p', { class: 'small muted', text: t('rule_note') }),
    err,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), save));
}

// ---- help --------------------------------------------------------------------------------

function viewHelp(st) {
  const out = S.ui.doctorOut === null ? null : h('pre', { class: 'out', text: S.ui.doctorOut });
  const run = h('button', { class: 'btn', disabled: S.ui.doctorRunning, onclick: async () => {
    S.ui.doctorRunning = true; render();
    try { const r = await api('doctor', {}); S.ui.doctorOut = r.output; S.ui.doctorOk = r.ok; } catch (e) { showError(e); }
    finally { S.ui.doctorRunning = false; render(); }
  } }, S.ui.doctorRunning ? h('span', { class: 'spin' }) : null, S.ui.doctorRunning ? ' ' + t('diag_running') : t('diag_run'));
  return [
    h('div', { class: 'card' }, h('h2', { text: t('help_title') }), h('ul', { class: 'steps' }, t('help_points').map((p) => h('li', { text: p })))),
    h('div', { class: 'card' }, h('h3', { text: t('notice_short') }), noticeBlock()),
    h('div', { class: 'grid2' },
      h('div', { class: 'card' }, h('h3', { text: t('diag_title') }), h('div', { class: 'actions start' }, run,
        h('button', { class: 'btn', text: t('open_data'), onclick: () => api('open', { what: 'data' }).catch(showError) })), out),
      h('div', { class: 'card' }, h('h3', { text: t('cli_title') }), h('p', { class: 'small', text: t('cli_body') }),
        h('p', { class: 'small muted', text: t('version', { v: st.version }) }), h('p', { class: 'small muted mono', text: st.home }))),
    updateCard(st),
    h('div', { class: 'grid2' },
      st.can_install ? h('div', { class: 'card' }, h('h3', { text: t('install_title') }), h('p', { class: 'small', text: t('install_body') }),
        h('button', { class: 'btn primary', text: t('install_go'), onclick: async (e) => {
          e.target.disabled = true;
          try { await api('install', { reopen: true }); } catch (x) { toast(t('install_failed', { e: x.message }), true); e.target.disabled = false; }
        } }))
        : h('div', { class: 'card' }, h('h3', { text: t('install_title') }), h('p', { class: 'small muted', text: st.installed ? t('installed_note') : '' })),
      h('div', { class: 'card' }, h('h3', { text: t('uninstall_title') }), h('p', { class: 'small', text: t('uninstall_body') }),
        h('button', { class: 'btn danger', text: t('uninstall_go'), onclick: () => openDialog(h('h2', { text: t('uninstall_title') }),
          h('p', { text: t('uninstall_body') }), h('p', { class: 'small muted', text: t('uninstall_confirm') }),
          h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }),
            h('button', { class: 'btn primary danger', text: t('uninstall_ok'), onclick: async () => { closeDialog(); await api('uninstall', {}).catch(showError); } }))) }))),
  ];
}

// ---- updates -------------------------------------------------------------------------------

function updateCard(st) {
  const u = st.update;
  if (!u) return null;
  const status = u.checking || S.ui.updChecking ? h('span', {}, h('span', { class: 'spin' }), ' ', t('upd_checking'))
    : u.error ? h('span', { class: 'err', text: t('upd_err', { e: u.error }) })
      : u.newer ? h('b', { text: t('upd_new', { v: u.latest }) })
        : u.checked ? h('span', { text: t('upd_newest') + ' (' + t('upd_checked', { t: ago(u.checked) }) + ')' }) : null;
  const check = h('button', { class: 'btn', text: t('upd_check'), disabled: u.checking || S.ui.updChecking, onclick: async () => {
    S.ui.updChecking = true; render();
    try { await api('update/check', {}); } catch (e) { showError(e); } finally { S.ui.updChecking = false; refresh(); }
  } });
  return h('div', { class: 'card' }, h('h3', { text: t('upd_title') }),
    h('p', { class: 'small muted', text: t('upd_current', { v: u.current }) }),
    status ? h('p', { class: 'small' }, status) : null,
    u.newer && u.notes ? h('details', { class: 'mt6' }, h('summary', { text: t('upd_notes') }), h('pre', { class: 'out', text: u.notes })) : null,
    h('div', { class: 'actions start mt12' },
      u.newer ? h('button', { class: 'btn primary', text: t('upd_go', { v: u.latest }), disabled: u.installing, onclick: () => confirmUpdate(st) }) : null,
      check,
      u.page && u.newer ? h('button', { class: 'btn ghost', text: t('upd_page'), onclick: () => api('open', { what: 'release' }).catch(showError) }) : null),
    h('label', { class: 'check mt12' }, h('input', { type: 'checkbox', checked: st.config.check_updates, onchange: (e) => setConfig('check_updates', e.target.checked) }),
      h('span', { class: 'small', text: t('set_upd') })));
}

function confirmUpdate(st) {
  const u = st.update;
  const err = h('div', { class: 'err' });
  const go = h('button', { class: 'btn primary', text: t('upd_go', { v: u.latest }), onclick: async () => {
    go.disabled = true;
    err.replaceChildren(h('span', { class: 'muted' }, h('span', { class: 'spin' }), ' ', t('upd_installing')));
    try {
      await api('update/install', {});
      err.replaceChildren(h('span', { text: t('upd_done', { v: u.latest }) }));
    } catch (e) {
      err.textContent = e.code === 'sessions_running' ? t('upd_err_sessions') : e.code === 'windows_open' ? t('upd_err_windows') : t('upd_failed', { e: errText(e) });
      go.disabled = false;
    }
  } });
  openDialog(h('h2', { text: t('upd_confirm_title', { v: u.latest }) }), h('p', { text: t('upd_confirm_body', { v: u.latest }) }),
    st.installed ? null : h('p', { class: 'small muted', text: t('upd_portable') }),
    u.notes ? h('details', {}, h('summary', { text: t('upd_notes') }), h('pre', { class: 'out', text: u.notes })) : null,
    err, h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), go));
}

// ---- start ---------------------------------------------------------------------------------

connect().catch(() => {}).then(refresh).then(schedule);
