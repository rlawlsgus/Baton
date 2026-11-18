'use strict';
// baton GUI. Plain DOM, no framework; every string from the server is inserted
// as text, never as HTML.

const T = {
  ko: {
    tab_sessions: '세션', tab_accounts: '계정', tab_settings: '설정', tab_help: '도움말', tab_office: '사무실',
    claude_desktop: 'Claude Code {v} (데스크톱 앱)', claude_cli: 'Claude Code {v} (CLI)', claude_checking: 'Claude Code 확인 중…',
    claude_missing: 'Claude Code 없음',
    mode_auto: '자동 전환', mode_confirm: '전환 전 확인', mode_off: '자동 전환 꺼짐', mode_unset: '전환 방식 미설정',
    lang_label: '언어',
    theme_label: '화면 테마', theme_title: '창 색: Windows 설정을 따르거나, 늘 밝게 또는 어둡게',
    theme_auto: '◐ 자동', theme_light: '☀ 라이트', theme_dark: '☾ 다크',
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
    switch_btn: '다른 계정으로 전환', switch_auto: '다음 계정으로 (자동 선택)', switch_to: '{a}(으)로', open_folder: '폴더 열기', open_log: '기록',
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
    also_open_hint: '이 대화가 {w}에서도 열려 있습니다. Claude가 지금 응답을 마치면 baton이 이 세션을 사본으로 옮깁니다.',
    ao_desktop: 'Claude 데스크톱 앱', ao_terminal: '터미널', ao_vscode: 'VS Code', ao_account: '{a} 계정의 다른 세션', ao_other: '다른 Claude Code 창',
    rc_on: '리모트 컨트롤', details_more: '자세히', details_less: '접기', d_path: '폴더', d_started: '시작', d_changes: '변경', ctx_short: '컨텍스트', rc_where: '휴대폰의 Claude 앱이나 claude.ai/code에서 {a} 계정으로 로그인하면 이 세션이 보입니다. 계정이 바뀌면 바뀐 계정에서 보입니다.',
    start_rc: '리모트 컨트롤 켜기', start_rc_hint: '휴대폰의 Claude 앱이나 claude.ai/code에서 이 세션을 보고 이어서 쓸 수 있습니다. 세션이 실행 중인 계정으로 로그인해야 보이고, 계정이 바뀌면 그 계정에서 다시 켜집니다.',
    set_rc: '새 세션에서 리모트 컨트롤 켜기를 기본으로',
    show_window: '창 보기', show_window_tip: '이 세션의 Claude Code 창을 앞으로 가져옵니다',
    cut_title: '끊긴 세션 {n}개', cut_title1: '끊긴 세션 1개',
    cut_body: 'PC가 다시 시작되었거나 세션이 예기치 않게 멈췄습니다. 다시 열면 같은 대화를 같은 설정으로 이어 가고, 작업 중이던 세션은 하던 일을 계속합니다.',
    cut_restart: 'PC 다시 시작', cut_stopped: '예기치 않게 멈춤', cut_working: '작업 중이었음',
    restore_btn: '다시 열기', restore_all: '모두 다시 열기', dismiss_btn: '다시 열지 않기', dismiss_all: '모두 다시 열지 않기',
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
    claude_app_setup: 'Claude 데스크톱 앱이 아직 Claude Code를 준비하지 않았습니다. 데스크톱 앱에서 Code 탭을 한 번 열면 준비됩니다.',
    claude_cli_old: '이 PC의 Claude Code CLI({v})는 baton에 쓰기에는 버전이 낮습니다 ({min} 이상 필요).',
    claude_bundle_off: 'Claude 데스크톱 앱에 들어 있는 Claude Code 사용이 꺼져 있습니다. 설정 → 고급 설정에서 다시 켜세요.',
    // choosing which Claude Code to use (Settings → Advanced)
    claude_app_old: 'Claude 데스크톱 앱의 Claude Code({v}) 버전이 너무 낮습니다. 데스크톱 앱을 업데이트하고(도움말 → 업데이트 확인) Code 탭을 한 번 여세요.',
    claude_none_old: 'Claude Code는 있지만 모두 baton에 쓰기에는 버전이 낮습니다 ({min} 이상 필요). 설정 → 고급 설정 → 사용할 Claude Code에서 업데이트하거나 Claude 데스크톱 앱을 업데이트하세요.',
    claude_app_installing: 'Claude 데스크톱 앱이 Claude Code를 설치하는 중입니다. 잠시 뒤 다시 확인하세요.',
    cc_auto: '자동 (권장)', cc_auto_now: '이 PC의 Claude Code 중 baton과 쓸 수 있는 가장 새 버전 (지금: {src}의 {v})', cc_auto_none: '이 PC의 Claude Code 중 baton과 쓸 수 있는 가장 새 버전 (지금은 없음)',
    cc_desktop: 'Claude 데스크톱 앱에 들어 있는 것', cc_desktop_now: '앱 업데이트를 따라감 (지금: {v})', cc_desktop_none: '앱 업데이트를 따라감 (지금은 쓸 수 있는 것이 없음)',
    cc_other: '다른 위치 (claude.exe 전체 경로)', cc_other_hint: '예: C:\\Users\\…\\claude.exe', cc_in_use: '사용 중', cc_update: '업데이트', cc_updating: '업데이트 중…',
    cc_update_started: 'Claude Code를 업데이트하는 창이 열렸습니다. 끝나면 여기에 새 버전이 나옵니다.',
    cc_why_too_old: 'baton에 쓰기에는 오래됨 ({min} 이상 필요)', cc_why_too_old_app: 'baton에 쓰기에는 오래됨 ({min} 이상 필요). Claude 데스크톱 앱을 업데이트하세요', cc_why_not_runnable: '실행되지 않음',
    cc_why_installing: 'Claude 앱이 설치하는 중', cc_why_wrapper: '.cmd 래퍼라 쓸 수 없음 (claude.exe 필요)', cc_why_known_bad: 'baton과 맞지 않는 알려진 문제가 있음',
    cc_why_missing: '더 이상 없음', cc_why_old: '버전이 너무 낮음',
    cc_src_desktop: 'Claude 데스크톱 앱', cc_src_native: '기본 설치 프로그램', cc_src_npm: 'npm', cc_src_winget: 'WinGet', cc_src_scoop: 'Scoop', cc_src_path: 'PATH', cc_src_config: '직접 지정한 경로',
    cc_fallback: '선택한 Claude Code를 지금 쓸 수 없습니다: {why}. 다시 쓸 수 있게 되면 다시 그것을 쓰고, 그동안은 {src}의 Claude Code {v} 버전을 씁니다.',
    need_claude_old: '이 PC의 Claude Code 중 baton과 쓸 수 있는 것이 없어 세션을 시작할 수 없습니다.', claude_unusable: '쓸 수 있는 Claude Code 없음', cc_update_cli: 'CLI 업데이트',
    cc_note: '이미 실행 중인 세션은 쓰던 Claude Code를 그대로 씁니다. 앞으로 시작하는 세션부터 적용됩니다.',
    err_not_updatable: '이 Claude Code는 여기서 업데이트할 수 없습니다. 다시 확인을 누른 뒤 다시 시도하세요.', err_claude_updating: 'Claude Code를 이미 업데이트하고 있습니다. 그 창이 끝날 때까지 기다리세요.',
    // what to do about a Claude Code CLI too old for baton (the banner, the first-run screen)
    claude_cli_old_btn: 'CLI 업데이트를 누르면 그 CLI가 스스로 업데이트합니다 (claude update).',
    claude_cli_old_later: '이 안내를 마친 뒤 창 위쪽의 CLI 업데이트를 누르거나, PowerShell에서 claude update를 실행하세요.',
    claude_cli_old_app: 'CLI를 따로 쓰지 않는다면 Claude 데스크톱 앱에서 Code 탭을 한 번 여는 것으로도 됩니다. 그러면 baton이 앱에 들어 있는 Claude Code를 씁니다.',
    cc_why_not_set_up: 'Claude 데스크톱 앱이 아직 Claude Code를 준비하지 않음 (Code 탭을 한 번 여세요)',
    recheck: '다시 확인', elevated: 'baton이 관리자 권한으로 실행 중입니다. 세션도 관리자 권한으로 실행되니, 창을 닫고 일반 권한으로 다시 여세요.',
    // start dialog
    start_title: '새 세션 시작', start_folder: '작업할 폴더', browse: '찾아보기', recent_folders: '최근 폴더',
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
    held_desktop: 'Claude 데스크톱 앱', held_cli: '터미널의 Claude Code(CLI)', held_vscode: 'VS Code', held_other: '다른 Claude Code 창',
    held_recheck: '다시 확인', import_go_copy: '사본으로 이어 가기', held_still: '아직 열려 있습니다.', held_free: '이제 닫혔습니다. 같은 대화로 이어 갈 수 있습니다.',
    // a conversation open elsewhere goes on in a copy; one baton runs comes to the front
    copy_auto: '{app}에서 열려 있어 사본으로 이어 갑니다. 지금까지의 내용이 모두 들어 있고, 원래 대화와는 따로 진행됩니다.',
    held_recheck_hint: '{app} 쪽을 먼저 종료하면 같은 대화로 이어 갈 수 있습니다.',
    conv_in_baton: '이 대화는 이미 baton에서 실행 중입니다. 시작을 누르면 그 창을 앞으로 가져옵니다.',
    in_baton_front: '이 대화는 이미 baton에서 실행 중입니다. 그 창을 앞으로 가져왔습니다.',
    err_in_baton: '이 대화는 이미 baton에서 실행 중입니다. 세션 목록에서 확인하세요.',
    err_deleting: '이 대화를 삭제하는 중입니다.',
    // deleting a conversation (to the Recycle Bin), and what keeps one from going now
    del_btn: '대화 삭제', del_title: '이 대화를 삭제할까요?', del_loading: '삭제할 내용을 확인하는 중…',
    del_leftover: '삭제하다 남은 대화 파일',
    del_body: '대화와 Claude Code가 함께 저장한 것(서브에이전트, 저장된 도구 출력, /rewind 체크포인트)을 휴지통으로 옮깁니다. baton, Claude Code의 이어 가기 목록, 사무실에서 사라집니다. 휴지통에서 되살릴 수 있습니다.',
    del_outside: 'baton 밖({src})에서 만든 대화입니다. 그곳 목록에서도 사라집니다.',
    del_desktop_off: 'Claude 데스크톱 앱의 다음 계정 목록에서도 뺍니다: {a}.', del_app_account: '데스크톱 앱 자체 계정',
    del_desktop_off_done: 'Claude 데스크톱 앱의 다음 계정 목록에서는 이미 뺐습니다: {a}. ‘{b}’ 버튼으로 다시 표시할 수 있습니다.',
    del_copies: '이 대화에서 이어 간 대화가 {n}개 있습니다. 내용은 그대로지만, 이 대화와 함께 저장된 큰 도구 출력은 더 이상 열 수 없습니다.',
    del_copies_one: '이 대화에서 이어 간 대화가 1개 있습니다. 내용은 그대로지만, 이 대화와 함께 저장된 큰 도구 출력은 더 이상 열 수 없습니다.',
    del_copies_more: '같은 폴더의 다른 대화를 모두 읽지는 못했습니다. 이 대화의 저장된 도구 출력을 쓰는 대화가 더 있을 수 있습니다.',
    del_kept: '프로젝트 파일, 폴더 메모리, Claude Code 입력 기록, 다른 대화는 그대로 둡니다.',
    del_go: '휴지통으로 옮기기', del_done: '대화를 휴지통으로 옮겼습니다.',
    del_reopened: '그사이 Claude Code가 이 대화를 다시 열어서, 그쪽에서 쓰는 내용으로 대화가 다시 생길 수 있습니다.',
    del_still: '다시 확인했지만 그대로입니다.', del_free: '이제 삭제할 수 있습니다.',
    del_in_baton: 'baton에서 실행 중입니다. 먼저 그 세션을 끝내세요.',
    err_gone: '이미 없는 대화입니다.',
    err_bad_session: 'baton이 이 대화를 목록에 있던 곳에서 더 이상 찾지 못합니다. 닫고 다시 확인하세요.',
    err_copy_starting: '이 대화의 사본이 baton에서 시작하는 중입니다. 사본에 첫 메시지를 보낸 뒤 다시 시도하세요.',
    err_open_elsewhere: '{app}에서 열려 있습니다. 먼저 닫으세요.',
    err_open_elsewhere_desktop: 'Claude 데스크톱 앱에서 열려 있습니다. 먼저 데스크톱 앱을 완전히 종료(트레이 아이콘에서도)하세요.',
    err_desktop_app: 'Claude 데스크톱 앱이 자기 세션으로 갖고 있습니다. 데스크톱 앱에서 삭제하세요.',
    err_desktop_app_prior: '데스크톱 앱이 자기 세션의 앞부분으로 갖고 있습니다. 데스크톱 앱에서 그 세션을 먼저 삭제하세요.',
    err_desktop_busy: '이 대화를 표시하는 목록을 Claude 데스크톱 앱이 열어 두고 있습니다. 데스크톱 앱을 완전히 종료(트레이 아이콘에서도)한 뒤 다시 시도하세요.',
    err_desktop_waiting: '이 대화를 표시하는 목록을 Claude 데스크톱 앱이 열어 두고 있습니다. 데스크톱 앱을 완전히 종료(트레이 아이콘에서도)한 뒤 다시 시도하세요.',
    err_two_files: '이 대화의 파일이 서로 다른 두 개라 어느 쪽을 지울지 알 수 없습니다.',
    err_other_name: '이 대화 파일에 Claude Code 폴더 밖의 다른 이름이 있어서, baton은 손대지 않습니다.',
    err_cannot_check: '지금 열려 있는지 확인하지 못했습니다. 잠시 뒤 다시 시도하세요.',
    err_no_recycle_bin: 'Windows가 휴지통에 넣을 수 없어({d}) 아무것도 지우지 않았습니다.',
    err_path_too_long: '휴지통에 넣기에는 경로가 너무 긴 파일이 있어 아무것도 지우지 않았습니다.',
    err_in_use: '다른 프로그램이 파일을 쓰고 있습니다. 잠시 뒤 다시 시도하세요.',
    err_partly: '일부만 휴지통으로 옮겼습니다. 다시 시도하세요.',
    err_linked_folder: '대화 파일이 링크(정션)로 연결된 폴더나 baton이 읽을 수 없는 폴더에 있어서 그대로 둡니다.',
    err_session_window: '이 대화를 쓰던 baton 세션은 끝났지만 그 창이 아직 열려 있습니다. 그 창을 닫은 뒤 다시 시도하세요.',
    err_recycle_failed: 'Windows가 휴지통으로 옮기지 못해 아무것도 지우지 않았습니다. 다시 시도하세요.',
    err_changed: '삭제하는 동안 파일이 옮겨져 대화가 아직 남아 있습니다. 다시 시도하세요.',
    err_delete: '지금은 이 대화를 삭제하지 못했습니다. 다시 시도하세요.',
    // a session moving to a copy, a conversation running in baton, quitting the desktop app to go on in it
    run_moving: '사본으로 옮기는 중', conv_running: 'baton에서 실행 중',
    held_recheck_hint_desktop: 'Claude 데스크톱 앱을 먼저 완전히 종료하면(트레이 아이콘에서도 종료) 같은 대화로 이어 갈 수 있습니다.',
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
      'baton은 baton 계정의 로그인 정보를 읽거나 저장하지 않습니다. 로그인과 갱신은 Claude Code가 직접 합니다. 데스크톱 앱 로그인 전환을 켜면, baton은 앱의 로그인 파일을 있는 그대로 복사해 보관하며 복호화하거나 어디로도 보내지 않습니다.',
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
    set_claude_which: '사용할 Claude Code',
    claude_which_note: '어느 쪽이든 세션은 별도의 Claude Code 창(터미널 화면)에서 열립니다. 데스크톱 앱 화면 안에서 열 수는 없습니다.', set_claude_bundle: 'Claude 데스크톱 앱에 들어 있는 Claude Code도 사용',
    saved: '저장했습니다.', set_lang: '화면 언어', lang_auto: 'Windows 설정 따르기',
    // help
    help_title: 'baton은 이렇게 동작합니다',
    help_points: [
      '계정마다 별도의 Claude Code 프로필이 있고, 로그인은 Claude Code가 직접 관리합니다.',
      '세션은 Claude Code 창으로 열립니다. 평소처럼 그 창에서 작업하세요.',
      '사용 중인 계정이 한도에 걸리면 baton이 Claude Code를 안전하게 멈추고, 같은 대화를 다음 계정에서 다시 엽니다(약 5~10초).',
      '전환되어도 대화 내용과 작업 폴더는 그대로입니다. 실행 중이던 백그라운드 명령은 끝나고, 다시 실행되지 않습니다.',
      'Claude 데스크톱 앱의 로그인은 계정 탭에서 직접 바꾸지 않는 한 바뀌지 않습니다(비공식 기능이며, 설정에서 켜기 전에는 꺼져 있습니다). baton 계정들은 데스크톱 앱과 따로 로그인되어 있습니다.',
      '세션 창에서 그림(스크린샷 등)을 붙여 넣으려면 왼쪽 Alt+V를 누르세요. Ctrl+V 등 터미널의 붙여 넣기는 글자만 붙여 넣습니다. 이미지 파일은 창으로 끌어다 놓거나 전체 경로를 붙여 넣으면 첨부되고, 폴더 안의 파일은 @와 이름 일부를 입력해 가리킬 수 있습니다.',
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
    uninstall_go: '제거', uninstall_confirm: '이 창이 닫히고, 제거 창에서 한 번 더 확인합니다. 실행 중인 세션이 있으면 먼저 끝내세요.', uninstall_ok: '제거 창 열기',
    // office
    office_set: '사무실 보기 (재미 기능)',
    office_set_hint: '이 PC의 Claude Code 대화를 사무실에서 일하는 사람들로 보여 줍니다. 사무실 탭이 생기고, 따로 띄워 다른 모니터에 둘 수도 있습니다. 대화 기록을 읽기만 하고 아무것도 바꾸지 않습니다.',
    office_window_title: 'baton 사무실', office_floor_guide: '층별 안내', office_follow: '최근 세션 자동으로 따라가기', office_no_floors: '아직 층이 없습니다.',
    office_floor_sessions: '세션 {n}개', office_floor_session1: '세션 1개', office_active_now: '지금 활동 중', office_quiet_more: '오래 쉰 층 {n}개 더 보기', office_quiet_more1: '오래 쉰 층 1개 더 보기', office_quiet_less: '오래 쉰 층 접기',
    office_popout: '따로 띄우기', office_popout_tip: '사무실만 별도 창으로 띄웁니다. 다른 모니터에 두고 보기 좋습니다.',
    office_history: '지난 기록', office_untitled: '제목 없는 세션 · {id}',
    office_zoom_in: '확대', office_zoom_out: '축소', office_zoom_reset: '원래대로', office_hint: '드래그로 이동 · 휠로 확대 · 더블클릭으로 원위치', office_lift: '{dir} {n}F',
    office_empty_title: '아직 출근한 Claude가 없어요', office_empty_body: 'Claude Code로 대화를 시작하면 여기서 일하는 모습이 보입니다.',
    office_off_title: '사무실이 닫혀 있습니다', office_off: '설정에서 ‘사무실 보기’를 켜면 다시 열립니다.',
    office_gone_title: '퇴근 시간입니다', office_gone: 'baton 창이 닫혀 사무실도 문을 닫았습니다.', office_reconnect: '연결이 끊겨 다시 연결하는 중…',
    office_f_project: '프로젝트', office_f_session: '세션', office_f_account: '계정', office_f_state: '상태', office_f_source: '어디서',
    office_src_baton: 'baton', office_src_cli: 'Claude Code (터미널)', office_src_desktop: 'Claude 데스크톱 앱', office_no_account: 'baton 밖에서 실행',
    office_todos: '회의실 화이트보드', office_people: '사무실 사람들', office_people_n: '{n}명 근무 중', office_people_empty: '아무도 없습니다.',
    office_log: '활동 로그', office_log_empty: '아직 기록이 없습니다.', office_legend: '어디서 무엇을 하나',
    office_me: '{name} (나)', office_agent: '서브에이전트', office_more_agents: '+{n}명 더', office_tools_n: '도구 {n}회', office_tools_n1: '도구 1회',
    office_click_focus: '누르면 세션 창이 앞으로 나옵니다', office_secs: '{n}초', office_mins: '{n}분', office_hours: '{n}시간',
    office_st_arriving: '출근 중', office_st_working: '작업 중', office_st_thinking: '고민 중', office_st_talking: '말하는 중', office_st_asking: '질문 중',
    office_st_error: '문제 생김', office_st_idle: '쉬는 중', office_st_waiting: '기다리는 중', office_st_reporting: '보고 중', office_st_leaving: '퇴근 중',
    office_b_think: '고민 중', office_b_secs: '{n}초째', office_b_ask: '확인이 필요해요', office_b_report: '보고합니다',
    office_b_leave: '👋 퇴근합니다!', office_b_hello: '👋 출근했습니다!', office_b_shift_out: '👋 교대합니다!', office_b_shift_in: '🔁 교대 근무 시작!',
    office_b_shift: '교대 근무: {d}', office_b_compact: '대화를 정리했어요', office_b_denied: '중단됐어요', office_b_clear: '새 대화 시작!', office_b_end: '오늘 업무 끝!',
    office_l_think: '고민 중', office_l_fail: '실패: {d}', office_l_deny: '거절되거나 중단됨', office_l_compact: '대화가 길어져 정리(압축)했습니다',
    office_l_idle: '차례를 마치고 쉬는 중', office_l_wait: '차례를 마치고 서브에이전트를 기다리는 중', office_wait: '서브에이전트를 기다리는 중', office_l_hire: '{type} 고용', office_l_report: '결과를 보고했습니다', office_l_leave: '퇴근했습니다',
    office_l_ask: '질문: {d}', office_l_ask_plain: '답을 기다리는 중', office_l_shift: '교대 근무: {d}', office_l_clear: '/clear로 새 대화를 시작했습니다', office_l_end: '세션이 끝났습니다',
    office_room_library: '자료실', office_room_dev: '개발실', office_room_server: '서버실', office_room_search: '정보검색실', office_room_meeting: '회의실',
    office_room_desk: '내 자리', office_room_seat: 'Claude 자리', office_room_lounge: '라운지', office_room_elevator: '엘리베이터',
    office_room_library_d: '파일 읽기 · 검색', office_room_dev_d: '코드 작성 · 수정', office_room_server_d: '명령 실행 · git', office_room_search_d: '웹 · 외부 도구',
    office_room_meeting_d: '계획 · 할 일 · 스킬', office_room_desk_d: '질문 · 확인 요청', office_room_seat_d: '생각 · 대화 · 에이전트 호출과 보고',
    office_room_lounge_d: '차례가 끝나고 에이전트도 퇴근하면 커피 한잔', office_room_elevator_d: '출근 · 퇴근 · 교대',
    // office: side columns, and what is hidden from it
    office_resize_side: '층별 안내 너비', office_resize_panel: '오른쪽 패널 너비', office_resize_tip: '드래그해서 너비 조절 · 더블클릭하면 원래대로 · Enter로 접기/펼치기', office_folded: '접힘',
    office_guide_btn: '층별 안내', office_guide_tip: '층별 안내를 사무실 위에 펼칩니다', office_panel_btn: '패널', office_panel_tip: '세션 정보, 사람들, 활동 로그를 펼칩니다',
    office_show_s: '사무실에 표시: {name}', office_show_floor: '이 층(프로젝트)을 사무실에 표시: {name}', office_show_tip: '체크를 풀면 사무실에서만 숨깁니다. 세션 자체는 그대로입니다.',
    office_hidden_more: '숨긴 항목 {n}개 보기', office_hidden_more1: '숨긴 항목 1개 보기', office_hidden_less: '숨긴 항목 접기', office_unhide_all: '모두 다시 보이기',
    office_hidden_n: '{n}개 숨김', office_floor_off: '층 숨김', office_follow_tip: '숨긴 세션은 따라가지 않습니다', office_quiet_tip: '최근 3일 동안 활동이 없는 층',
    office_all_hidden_title: '모든 세션을 숨겼습니다', office_all_hidden: '층별 안내에서 다시 볼 층이나 세션에 체크하세요.',
    err_office_off: '사무실 보기가 꺼져 있습니다. 설정에서 켜세요.', err_no_popout: '이 baton에서는 사무실을 따로 띄울 수 없습니다.',
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
    err_deleting_any: '대화를 삭제하는 중입니다. 끝날 때까지 기다린 뒤 다시 시도하세요.',
    switch_now: '요청했습니다. 곧 {a} 계정에서 시작합니다.', switch_now_any: '요청했습니다. 쓸 수 있는 계정에서 곧 시작합니다.',
    num_bad: '1 이상의 정수를 입력하세요.',
    err_bad_folder: '있는 폴더를 고르세요 (내 PC의 폴더, 예: C:\\Users\\…).', err_long_folder: '경로가 너무 깁니다. 더 짧은 경로의 폴더를 고르세요.',
    ago_now: '방금', ago_min: '{n}분 전', ago_hour: '{n}시간 전', ago_day: '{n}일 전',
    // showing a session in the Claude desktop app
    dk_btn: '데스크톱 앱에 표시할 계정 선택', dk_btn_wait: '첫 메시지를 보낸 뒤에 데스크톱 앱에 표시할 수 있습니다.',
    dk_title: 'Claude 데스크톱 앱에 표시', dk_loading: '데스크톱 앱을 살펴보는 중…', dk_apply: '적용',
    dk_intro: '이 세션을 Code 탭 목록에 보여 줄 Claude 데스크톱 앱 계정을 고르세요. 체크하지 않은 계정에서는 보이지 않습니다. 아무것도 업로드되지 않으며, 데스크톱 앱이 이 컴퓨터에 있는 같은 대화를 엽니다.',
    dk_active: '지금 데스크톱 앱에 로그인된 계정', dk_used: '데스크톱 앱에서 이 계정으로 바꾸면 보입니다',
    dk_unused: '아직 데스크톱 앱에서 쓴 적이 없는 계정입니다. 데스크톱 앱에서 이 계정으로 로그인하면 보입니다',
    dk_same: '{a}, {b}은(는) 같은 Claude 계정입니다.', dk_app_only: 'Claude 데스크톱 앱에 지금 로그인된 계정', dk_app_only_note: 'baton 계정이 아닙니다.',
    dk_former: '더 이상 baton 계정이 아닙니다. 체크를 해제하면 이 목록에서 빠집니다.', dk_app_former: '데스크톱 앱의 다른 계정',
    dk_why_api_key: 'Claude 로그인이 아닌 API 키를 쓰는 계정이라 데스크톱 앱 목록이 없습니다.',
    dk_why_cloud: 'Claude 로그인이 아닌 {p}을(를) 쓰는 계정이라 데스크톱 앱 목록이 없습니다.',
    dk_why_unknown: 'baton이 아직 이 계정이 어떤 Claude 계정인지 모릅니다. 계정 탭에서 로그인 상태 확인을 누르세요.',
    dk_why_token: '장기 토큰으로 로그인하는 계정이라 baton이 어떤 Claude 계정인지 알 수 없습니다.',
    dk_why_gateway: 'Claude 로그인이 아닌 게이트웨이로 로그인하는 계정이라 데스크톱 앱 목록이 없습니다.',
    dk_cloud_other: '클라우드 제공자',
    dk_why_own: '이 계정의 데스크톱 앱에서 만든 대화라 그대로 둡니다. 숨기려면 데스크톱 앱에서 보관하세요.',
    dk_why_archived: '이 계정의 데스크톱 앱에서 보관한 대화입니다. 다시 보이게 하려면 데스크톱 앱에서 보관을 해제하세요.',
    dk_why_released: '이 계정의 데스크톱 앱에서 이어 가서 이제 데스크톱 앱의 대화입니다. 숨기려면 데스크톱 앱에서 보관하세요.',
    dk_why_deleted: '데스크톱 앱에서 삭제했습니다. 다시 보이게 하려면 체크하세요.',
    dk_copy: '데스크톱 앱에서 연 원래 대화는 그쪽에 남습니다. 이 체크는 baton의 사본(‘{t} (baton)’)에 대한 것입니다.',
    dk_archived: '데스크톱 앱에서 보관했습니다. 체크를 해제하면 목록에서 빠집니다.',
    dk_split: 'baton의 대화 파일과 데스크톱 앱의 대화 파일이 더 이상 같은 파일이 아니라서, 한쪽에서 나눈 대화가 다른 쪽에 보이지 않습니다. baton은 데스크톱 앱에서 이 세션을 더는 갱신하지 않습니다. 체크 해제는 할 수 있습니다.',
    dk_problem_conflict: '평소 Claude Code 폴더에 같은 id의 다른 대화 파일이 이미 있어 표시할 수 없습니다 (baton은 그 파일을 덮어쓰지 않습니다).',
    dk_problem_other_drive: '이 대화가 평소 Claude Code 폴더와 다른 드라이브에 있어 데스크톱 앱이 열 수 없습니다.',
    dk_problem_link_failed: 'baton이 데스크톱 앱에서 이 대화를 열 수 있게 만들지 못했습니다.',
    dk_problem_no_file: '첫 메시지를 보낸 뒤에 데스크톱 앱에 표시할 수 있습니다.',
    dk_running: '데스크톱 앱이 지금 열려 있습니다. 로그인된 계정에서는 앱을 완전히 종료(트레이 아이콘에서도)했다가 다시 열어야 바뀐 내용이 보입니다.',
    dk_tip_copy: 'baton이 실행 중일 때 데스크톱 앱에서 열면, 서로 덮어쓰지 않도록 baton은 사본으로 이어 갑니다. 데스크톱 앱에는 원래 대화가 남고 사본이 옆에 보입니다.',
    dk_tip_hide: '숨기려면 데스크톱 앱에서 삭제하지 말고 여기서 체크를 해제하세요. 그쪽에서 삭제하면 대화 파일이 나중에 정리될 수 있습니다.',
    dk_tip_limits: '데스크톱 앱에서 이어 가면 데스크톱 앱에 로그인된 계정의 사용 한도를 씁니다.',
    dk_done: '완료했습니다.', dk_next_open: '다음에 Claude 데스크톱 앱을 열면 보입니다.',
    dk_restart: '바뀐 내용을 보려면 Claude 데스크톱 앱을 완전히 종료(트레이 아이콘에서도)했다가 다시 여세요.',
    dk_switch: '데스크톱 앱에서 그 계정으로 바꾸면 보입니다.', dk_gone_quit: '데스크톱 앱을 종료하면 목록에서 사라집니다. 그때까지 이 창을 열어 두세요.',
    dk_gone: '체크를 해제한 계정에서는 더 이상 보이지 않습니다.', dk_none: '바뀐 것이 없습니다.',
    dk_pill: '데스크톱: {a}', dk_pill_split: '⚠ 데스크톱: 파일 갈라짐', dk_app_account: '데스크톱 앱 계정',
    err_no_file: '첫 메시지를 보낸 뒤에 데스크톱 앱에 표시할 수 있습니다.', err_unknown_place: '지금은 고를 수 없는 계정입니다. 창을 다시 여세요.',
    err_split: 'baton의 대화 파일과 데스크톱 앱의 대화 파일이 갈라져 새로 체크할 수 없습니다.', err_no_desktop: '이 PC에서 Claude 데스크톱 앱을 찾지 못했습니다.',
    err_conflict: '평소 Claude Code 폴더에 같은 id의 다른 대화 파일이 이미 있어 표시할 수 없습니다.',
    err_other_drive: '이 대화가 평소 Claude Code 폴더와 다른 드라이브에 있어 데스크톱 앱이 열 수 없습니다.',
    err_link_failed: 'baton이 데스크톱 앱에서 이 대화를 열 수 있게 만들지 못했습니다.',
    dk_kept: 'baton이 이 계정의 로그인을 보관하고 있습니다. 계정 탭에서 데스크톱 앱을 이 계정으로 바꿀 수 있습니다.',
    // switching the Claude desktop app's sign-in between your own accounts (unofficial; off unless turned on)
    dl_set: 'Claude 데스크톱 앱의 로그인 계정 바꾸기 (비공식)',
    dl_set_hint: '계정마다 데스크톱 앱의 로그인을 이 PC에 보관해 두었다가, 앱이 종료된 동안 고른 계정의 것을 되돌려 놓습니다. 이메일 코드 없이 계정을 바꿀 수 있습니다. 기본은 꺼짐입니다.',
    dl_set_where: '계정 탭에서 바꿉니다.',
    dl_leftover: '예전에 보관한 데스크톱 앱 로그인 {n}개가 아직 남아 있습니다.', dl_leftover_go: '지우기',
    dl_notice_title: '데스크톱 앱 로그인을 바꾸기 전에 꼭 읽어 주세요',
    dl_notice_points: [
      '이 기능을 켜면 baton이 Claude 데스크톱 앱의 로그인을 내 Claude 계정들 사이에서 다시 로그인하지 않고 바꿉니다. 직접 켜기 전에는 꺼져 있습니다.',
      '방법: 데스크톱 앱이 완전히 종료된 동안 baton이 앱의 로그인(쿠키, 사이트 저장소, 설정 파일의 항목 6개 — 그중 2개는 암호화된 토큰)을 {path} 폴더로 옮겨 보관하고, 고른 계정의 것을 되돌려 놓습니다. baton은 이 파일들을 있는 그대로 복사하고 옮기기만 하며, 복호화하거나 어디로도 보내지 않습니다. 쿠키와 토큰은 앱이 저장한 그대로 암호화되어 이 PC의 내 Windows 계정에서만 쓸 수 있습니다. 사이트 저장소는 앱에서도 암호화되어 있지 않습니다. 사이트 저장소에 있는 앱 설정 일부도 계정을 따라갑니다.',
      '이것은 Anthropic의 기능이 아닙니다. 이를 허용한다는 Anthropic의 공식 입장은 없고, 앱이 의도한 사용 방식도 아닙니다. 특히 사용 한도에 걸린 뒤 계정을 바꾸는 것은 제품이나 한도를 우회하는 것으로 볼 수 있으며, Anthropic은 계정을 정지할 수 있습니다. baton은 한 PC에서 여러 계정을 쓴다는 사실을 숨기지 않습니다. 계정에 대한 위험은 본인이 집니다.',
      '비공식 방식이라 데스크톱 앱이 업데이트되면 언제든 작동하지 않을 수 있고, 그러면 앱이 다시 로그인(이메일 코드)을 요청할 수 있습니다. 앱이 baton이 아는 모습과 다르면 baton은 아무것도 바꾸지 않습니다.',
      '내가 요청할 때만, 그리고 데스크톱 앱이 완전히 종료된 동안에만 바꿉니다. 앱이 실행 중이면, 그 안에서 돌고 있는 작업이 없을 때만 baton이 앱을 종료했다가 바꾼 뒤 다시 엽니다. 스스로 바꾸거나 사용 한도 때문에 바꾸는 일은 없습니다.',
      '본인 소유의 계정만 쓰세요. 이 기능을 켠 동안에는, 더 쓰지 않을 계정이 아니라면 Claude 데스크톱 앱에서 로그아웃하지 마세요. 로그아웃하면 Claude 서버에서도 로그인이 끝나 baton이 보관한 로그인도 작동하지 않습니다(다시 로그인하려면 이메일 코드가 필요합니다). 계정을 추가하려면 baton 계정 탭의 ‘데스크톱 앱에 계정 추가’를 쓰세요.',
      '이 기능을 끄거나 baton을 제거하면 baton이 보관한 로그인은 모두 지워집니다. 데스크톱 앱은 그때 로그인된 계정에 그대로 남습니다. 보관 삭제는 Claude 서버의 로그인을 끝내지 않습니다. 로그인까지 끝내려면 앱을 그 계정으로 바꾼 뒤 앱에서 로그아웃하고, baton에서 보관을 삭제하세요.',
    ],
    dl_notice_ack: '읽었으며, 계정에 대한 위험을 본인이 지는 것에 동의하고, 내가 요청할 때 baton이 데스크톱 앱의 로그인을 바꾸기를 원합니다.',
    dl_on: '켜기', dl_on_done: '데스크톱 앱 로그인 전환을 켰습니다. 계정 탭에서 바꿀 수 있습니다.',
    dl_off_title: '데스크톱 앱 로그인 전환을 끌까요?',
    dl_off_body: 'baton이 데스크톱 앱용으로 보관한 로그인 {n}개를 지웁니다.',
    dl_off_none: '아직 보관한 데스크톱 앱 로그인이 없어 지울 것이 없습니다.',
    dl_off_stays: '데스크톱 앱은 {a} 계정에 그대로 로그인되어 있습니다. 다른 계정을 다시 쓰려면 앱에서 그 계정으로 로그인(이메일 코드)하세요.',
    dl_off_blank: '데스크톱 앱이 지금 로그아웃 상태라 다음에 열면 로그인을 요청합니다.',
    dl_off_go: '끄고 보관한 로그인 지우기', dl_off_go_none: '끄기', dl_off_done: '데스크톱 앱 로그인 전환을 껐습니다.',
    dl_not_revoked: '지워도 Claude 서버의 로그인은 끝나지 않습니다.',
    dl_title: 'Claude 데스크톱 앱 로그인',
    dl_sub: 'Claude 데스크톱 앱이 로그인할 계정을 고르세요. 앱이 실행 중이면 baton이 앱에서 돌고 있는 작업이 없는지 확인한 뒤 종료했다가, 바꾸고 다시 엽니다.',
    dl_pill_running: '실행 중', dl_pill_quit: '종료됨',
    dl_running: '데스크톱 앱이 실행 중입니다. 바꿀 때 baton이 앱에서 돌고 있는 작업이 없는지 확인한 뒤 앱을 종료했다가 다시 엽니다.',
    dl_running_quit_it: '데스크톱 앱이 실행 중입니다. 먼저 완전히 종료(트레이 아이콘에서도)하세요.',
    dl_loading: '데스크톱 앱 로그인을 살펴보는 중…', dl_switching: '데스크톱 앱 로그인을 바꾸는 중…',
    dl_now: '지금 로그인됨', dl_baton: 'baton: {a}', dl_not_baton: 'baton 계정 아님',
    dl_kept: '{t} 보관', dl_not_kept_pill: '아직 보관 안 함', dl_not_kept: '아직 보관하지 않았습니다. 다른 계정으로 바꿀 때 보관합니다.',
    dl_damaged_pill: '온전하지 않음',
    dl_signed_out: '데스크톱 앱이 이 PC에서 로그아웃된 상태입니다. 앱을 열어 추가할 계정으로 로그인하세요.',
    dl_none: '아직 보관한 데스크톱 앱 로그인이 없습니다.',
    dl_use: '데스크톱 앱을 이 계정으로 바꾸기', dl_use_now: '데스크톱 앱이 지금 이 계정입니다.', dl_use_not_kept: '아직 이 계정의 로그인을 보관하지 않았습니다.',
    dl_new: '데스크톱 앱에 계정 추가', dl_new_title: '데스크톱 앱에 계정 추가',
    dl_new_body: '{a}의 로그인을 보관한 뒤 이 PC에 있는 데스크톱 앱의 로그인만 비웁니다. Claude에는 아무것도 보내지 않으므로 보관한 {a}의 로그인은 그대로 유지됩니다. 그다음 데스크톱 앱에서 다른 계정으로 로그인(이메일 코드)하면, 여기서 두 계정 사이를 바꿀 수 있습니다.',
    dl_new_blank: '데스크톱 앱에는 보관할 로그인이 없습니다. 이 PC에 남은 것만 따로 보관하고 비웁니다. 그다음 데스크톱 앱에서 추가할 계정으로 로그인(이메일 코드)하면, 여기서 계정 사이를 바꿀 수 있습니다.',
    dl_new_no_logout: '이때 앱의 로그아웃은 쓰지 마세요. Claude 서버에서도 로그인이 끝나 보관한 로그인이 작동하지 않게 됩니다.',
    dl_new_go: '보관하고 앱 로그인 비우기',
    dl_done: '데스크톱 앱이 이제 {a} 계정입니다. 앱을 열어 확인하세요.', dl_done_open: '데스크톱 앱이 이제 {a} 계정입니다. 앱을 엽니다.',
    dl_done_blank: '데스크톱 앱이 이 PC에서 로그아웃되었습니다. 앱을 열어 다른 계정으로 로그인하세요.',
    dl_done_blank_open: '데스크톱 앱이 이 PC에서 로그아웃되었고, 지금 앱을 엽니다. 다른 계정으로 로그인하세요.',
    dl_open_yourself: '데스크톱 앱은 직접 여세요.',
    dl_quit_title: '데스크톱 앱을 종료하고 {a} 계정으로 바꿀까요?',
    dl_quit_body: '데스크톱 앱이 실행 중입니다. baton이 앱에서 돌고 있는 작업이 없는지 확인한 뒤 앱을 종료하고, {a} 계정으로 바꾼 다음 다시 엽니다.',
    dl_quit_check: '앱의 세션에서 Claude가 작업 중이거나 내 답을 기다리고 있으면, 또는 백그라운드 작업(명령, 에이전트, 워크플로, /loop)이 돌고 있으면 종료하지 않고 그 세션을 알려 드립니다.',
    dl_quit_go: '종료하고 바꾸기',
    dl_new_quit: '데스크톱 앱이 실행 중입니다. 앱에서 돌고 있는 작업이 없으면 baton이 앱을 종료한 뒤 로그인을 비우고 다시 엽니다.',
    dl_done_quit: '데스크톱 앱을 종료하고 {a} 계정으로 바꿨습니다. 앱을 다시 엽니다.',
    dl_done_blank_quit: '데스크톱 앱을 종료하고 이 PC에서 로그아웃시킨 뒤 다시 엽니다. 다른 계정으로 로그인하세요.',
    dl_reopened: '데스크톱 앱은 다시 열었고, 원래 계정 그대로입니다.',
    dl_reopen_yourself: '데스크톱 앱은 원래 계정 그대로입니다. 직접 여세요.',
    dl_untitled: '(제목 없음)',
    dl_work_turn: 'Claude가 작업 중', dl_work_waiting: 'Claude가 내 답을 기다리는 중', dl_work_command: '백그라운드 명령 실행 중',
    dl_work_agent: '백그라운드 에이전트나 워크플로 실행 중', dl_work_wakeup: 'Claude가 다시 깨어날 예정(/loop 또는 예약 작업)',
    dl_work_unknown: '작업 중인지 지금은 알 수 없음(잠시 뒤 다시 시도하세요)',
    dl_recovered_first: '끊긴 전환을 먼저 되돌렸습니다.',
    dl_stashed: '로그아웃 상태의 데스크톱 앱에 남아 있던 것을 따로 보관했습니다(토큰 기록 없음). 계정 탭에서 지울 수 있습니다.',
    dl_rename: '이름 바꾸기', dl_rename_title: '‘{a}’ 이름 바꾸기', dl_rename_label: '이름 (baton에서만 쓰는 이름)',
    dl_rename_hint: '40자까지 쓸 수 있습니다. 비워 두면 baton이 붙이는 이름으로 돌아갑니다.', dl_rename_go: '이름 저장', dl_renamed: '이름을 저장했습니다.',
    dl_forget: '보관 삭제', dl_forget_title: '‘{a}’의 보관한 로그인을 지울까요?',
    dl_forget_body: 'baton이 보관한 {a}의 로그인을 지웁니다. 데스크톱 앱은 바뀌지 않습니다. 다시 {a} 계정으로 바꾸려면 앱에서 로그인(이메일 코드)하세요.',
    dl_forget_servers: '보관 삭제는 Claude 서버의 로그인을 끝내지 않습니다. 로그인까지 끝내려면 앱을 그 계정으로 바꾼 뒤 앱에서 로그아웃하고, 여기서 보관을 삭제하세요.',
    dl_forget_go: '보관한 로그인 삭제', dl_forgot: '{a}의 보관한 로그인을 지웠습니다.',
    dl_unnamed: '로그아웃 상태의 앱이 남긴 것 (토큰 기록 없음)', dl_unnamed_kept: '{t} 따로 보관',
    dl_unnamed_title: '로그아웃 상태의 앱이 남긴 것을 지울까요?', dl_unnamed_body: '데스크톱 앱에 로그인 토큰이 기록되지 않은 동안 앱이 남긴 것을 지웁니다.',
    dl_unnamed_go: '지우기', dl_unnamed_gone: '지웠습니다.',
    dl_forget_all: '보관한 로그인 모두 지우기', dl_forget_all_title: '보관한 로그인을 모두 지울까요?',
    dl_forget_all_body: 'baton이 Claude 데스크톱 앱용으로 보관한 로그인 {n}개를 모두 지웁니다.',
    dl_forget_all_stays: '데스크톱 앱은 {a} 계정에 그대로 로그인되어 있습니다.',
    // what signed-out apps left (no token recorded), counted apart from the sign-ins
    dl_unnamed_too: '로그아웃 상태의 데스크톱 앱이 남긴 것(토큰 기록 없음) {n}개도 지웁니다.',
    dl_unnamed_only: 'baton이 로그아웃 상태의 데스크톱 앱이 남긴 것(토큰 기록 없음) {n}개를 지웁니다. 보관한 로그인은 없습니다.',
    dl_leftover_unnamed: '로그아웃 상태의 데스크톱 앱이 남긴 것(토큰 기록 없음) {n}개가 아직 남아 있습니다.',
    dl_leftover_unnamed_too: '로그아웃 상태의 데스크톱 앱이 남긴 것(토큰 기록 없음) {n}개도 남아 있습니다.',
    dl_forget_all_go: '모두 지우기', dl_forgot_all: '보관한 로그인 {n}개를 지웠습니다.',
    dl_open_after: '바꾼 뒤 데스크톱 앱 열기',
    dl_no_logout: '이 기능을 켠 동안에는, 더 쓰지 않을 계정이 아니라면 Claude 데스크톱 앱에서 로그아웃하지 마세요. 로그아웃하면 Claude 서버에서도 로그인이 끝나 baton이 보관한 로그인도 작동하지 않습니다(다시 로그인하려면 이메일 코드가 필요합니다). 계정을 추가하려면 여기서 ‘데스크톱 앱에 계정 추가’를 쓰세요.',
    dl_cutoff: '데스크톱 앱 로그인 전환이 중간에 끊겼습니다. 데스크톱 앱을 완전히 종료한 뒤 원래대로 되돌리세요.', dl_putback: '되돌리기',
    dl_recovered: '끊긴 전환을 되돌렸습니다. 데스크톱 앱은 다시 {a} 계정입니다.', dl_recovered_blank: '끊긴 전환을 되돌렸습니다. 데스크톱 앱은 다시 로그아웃 상태입니다.',
    dl_recovered_none: '끊긴 전환이 없어 되돌릴 것이 없었습니다.',
    // a switch that was cut off and is not put back: the app can be left as it is instead
    dl_leave: '지금 그대로 두기', dl_leave_title: '데스크톱 앱을 지금 그대로 둘까요?',
    dl_leave_body: '앱은 지금 로그인된 계정에 그대로 남고, baton이 이 전환을 위해 따로 둔 것은 지웁니다. baton이 보관한 로그인은 그대로 보관됩니다.',
    dl_leave_hint: '되돌리기가 계속 실패하면 앱을 지금 그대로 둘 수 있습니다.',
    dl_left: '데스크톱 앱을 지금 그대로 두었습니다. 앱은 {a} 계정에 로그인되어 있습니다.',
    dl_left_blank: '데스크톱 앱을 지금 그대로 두었습니다. 앱은 로그아웃 상태입니다.',
    dl_left_none: '끊긴 전환이 없어 정리할 것이 없었습니다.',
    // copies a switch that stopped early left in the app's folder
    dl_copies: '중간에 멈춘 전환이 데스크톱 앱 폴더에 사본 {n}개(앱 설정과 로그인 파일의 사본)를 남겼습니다.',
    dl_copies_go: '사본 지우기', dl_copies_gone: '사본을 지웠습니다.',
    dl_acct_pill: '데스크톱 앱: 지금 로그인됨',
    dl_remove_note: 'baton이 보관한 이 Claude 계정의 데스크톱 앱 로그인은 그대로 남습니다. 지우려면 계정 탭에서 지우세요.',
    // uninstalling while a switch that was cut off is not settled: said before the window closes
    err_cut_off_running: 'Claude 데스크톱 앱 로그인 전환이 중간에 끊겼습니다. baton이 정리할 수 있도록 먼저 데스크톱 앱을 완전히 종료(트레이 아이콘에서도)한 뒤 제거하세요.',
    err_cut_off_inside_claude: 'Claude 데스크톱 앱 로그인 전환이 중간에 끊겼습니다. baton은 Claude Code 밖에서 실행할 때만 정리할 수 있는데, 이 창은 Claude Code 안에서 열렸습니다(CLAUDECODE가 설정됨). 이 창을 닫고 시작 메뉴(또는 baton 폴더)에서 baton을 다시 연 뒤 제거하세요.',
    err_dl_off: '데스크톱 앱 로그인 전환이 꺼져 있습니다. 설정에서 켜세요.',
    err_dl_inside_claude: 'baton 창이 Claude Code 안에서 열려(CLAUDECODE가 설정됨) 데스크톱 앱 로그인을 바꾸지 않습니다. 이 창을 닫고 시작 메뉴(또는 baton 폴더)에서 baton을 다시 여세요.',
    err_dl_unsupported_layout: '이 방식으로 설치된 Claude 데스크톱 앱은 아직 로그인 전환을 지원하지 않습니다.',
    err_dl_unsupported_layout_no_app: '이 PC에서 Claude 데스크톱 앱의 데이터 폴더를 찾지 못했습니다. 데스크톱 앱을 한 번 연 뒤 다시 시도하세요.',
    err_dl_unsupported_layout_several: 'Claude-Data 폴더(로컬 앱 데이터 안)에 데스크톱 앱 설정이 든 폴더가 여러 개라(다시 설치하며 남은 것일 수 있습니다) 앱이 어느 쪽을 쓰는지 알 수 없어 아무것도 바꾸지 않습니다.',
    err_dl_unsupported_layout_link: '데스크톱 앱의 데이터 폴더가 다른 곳을 가리키는 링크라 baton이 로그인을 바꾸지 않습니다.',
    err_dl_synced_store: 'baton 데이터 폴더가 OneDrive처럼 이 PC 밖으로 동기화되는 폴더 안에 있어 로그인을 보관하지 않습니다. 이 PC에만 있는 폴더로 옮기세요(BATON_HOME).',
    err_dl_elevated: 'baton이 관리자 권한으로 실행 중입니다. 데스크톱 앱 로그인을 바꾸려면 일반 권한으로 다시 여세요.',
    err_dl_busy: '데스크톱 앱에 대한 다른 작업이 진행 중입니다. 잠시 뒤 다시 시도하세요.',
    err_dl_app_running: 'Claude 데스크톱 앱이 아직 실행 중입니다. 완전히 종료(트레이 아이콘에서도)한 뒤 다시 시도하세요.',
    err_dl_app_working: '데스크톱 앱에서 아직 작업이 돌고 있어 앱을 종료하지 않았고 아무것도 바꾸지 않았습니다. 작업을 멈추거나 끝날 때까지 기다린 뒤 다시 시도하세요.',
    err_dl_quit_failed: 'Claude 데스크톱 앱이 제때 종료되지 않아 로그인을 바꾸지 않았습니다. 앱이 무언가를 묻고 있거나 아직 닫히는 중일 수 있습니다. 직접 종료(트레이 아이콘에서도)한 뒤 다시 시도하세요.',
    err_dl_quit_failed_refused: 'Claude 데스크톱 앱이 지금은 종료할 수 없다고 해서 아무것도 바꾸지 않았습니다. 직접 종료(트레이 아이콘에서도)한 뒤 다시 시도하세요.',
    err_dl_unknown_format: '데스크톱 앱이 이 baton이 모르는 방식으로 로그인을 저장하고 있습니다(앱이 업데이트되었을 수 있습니다). 아무것도 바꾸지 않았고, 앱은 지금 계정에 그대로 로그인되어 있습니다. 다시 바꾸려면 먼저 baton을 업데이트하세요.',
    err_dl_already: '데스크톱 앱이 이미 그 계정으로 로그인되어 있습니다.',
    err_dl_unsaved_current: '데스크톱 앱의 지금 로그인을 보관하지 못해 아무것도 바꾸지 않았습니다.',
    err_dl_unsaved_no_account: '데스크톱 앱이 로그인되어 있지만 아직 어느 계정인지 기록하지 않아 그 로그인을 보관할 수 없어 아무것도 바꾸지 않았습니다. 데스크톱 앱을 한 번 열어 계정이 보일 때까지 기다린 뒤 완전히 종료(트레이 아이콘에서도)하고 다시 시도하세요.',
    err_dl_unsaved_store_permissions: 'baton이 로그인을 보관하는 폴더에 전용 권한(나와 시스템만)을 줄 수 없어 아무것도 보관하지 않았고 바꾸지도 않았습니다.',
    err_dl_unsaved_kept_unreadable: 'baton이 보관한 {a}의 로그인을 읽지 못해 데스크톱 앱의 지금 로그인이 누구의 것인지 알 수 없어 아무것도 바꾸지 않았습니다. 잠시 뒤 다시 시도하세요. 계속 실패하면 목록에서 {a}의 ‘보관 삭제’를 누르세요.',
    err_dl_not_kept: '그 계정의 로그인을 보관하고 있지 않습니다. 데스크톱 앱에서 한 번 로그인하면(‘데스크톱 앱에 계정 추가’) 바꿀 수 있습니다.',
    err_dl_ambiguous: '그 이름에 맞는 계정이 여러 개입니다 ({a}).',
    err_dl_damaged: '보관한 그 계정의 로그인이 온전하지 않아 아무것도 바꾸지 않았습니다. 보관을 삭제하고 앱에서 다시 로그인하세요.',
    err_dl_tokens_of_another: '데스크톱 앱의 지금 로그인이 baton이 다른 계정용으로 보관한 토큰과 같아 누구의 로그인인지 알 수 없어 아무것도 바꾸지 않았습니다. 데스크톱 앱을 한 번 열었다가 완전히 종료(트레이 아이콘에서도)한 뒤 다시 시도하세요.',
    err_dl_switch_failed: '전환을 마치지 못해 데스크톱 앱을 원래대로 되돌려 놓았습니다.',
    err_dl_switch_unrestored: '전환을 마치지 못했고 아직 모두 되돌리지 못했습니다. 데스크톱 앱을 열지 말고 이 창 위쪽의 ‘되돌리기’를 누르세요.',
    err_dl_settle_unrestored: '데스크톱 앱을 지금 그대로 두지 못했습니다. 다른 프로그램이 앱의 파일을 잠시 쓰고 있을 수 있습니다. 데스크톱 앱을 열지 말고 잠시 뒤 다시 시도하세요.',
    err_dl_used_since: '이 전환이 넣어 둔 로그인으로 데스크톱 앱을 이미 사용했는데, 지금은 baton이 그 로그인을 보관할 수 없습니다(전환이 꺼져 있거나 baton 폴더가 동기화됨). 되돌리면 그 로그인이 사라지므로 아무것도 바꾸지 않았습니다. 대신 앱을 지금 그대로 둘 수 있습니다.',
    err_dl_bad_label: '그 이름은 쓸 수 없습니다.', err_dl_bad_label_too_long: '이름은 40자까지 쓸 수 있습니다.',
    err_dl_bad_label_characters: '이름에는 제어 문자, 줄 구분 문자, 쉼표를 쓸 수 없습니다.',
    err_dl_bad_label_looks_like_id: '이름이 계정 id처럼 보이면 안 됩니다(16진수 8자리 이상).',
    err_dl_bad_label_taken: '다른 계정이 이미 그 이름을 쓰고 있습니다.',
    err_dl_launch_unsupported: 'baton이 여기서 데스크톱 앱을 열 수 없습니다. 직접 여세요.',
    err_needs_desktop_ack: '안내를 읽고 동의한 뒤에 켤 수 있습니다.',
    err_desktop_logins: '지금은 데스크톱 앱 로그인 정보를 읽거나 바꾸지 못했습니다. 다시 시도하세요.',
  },
  en: {
    tab_sessions: 'Sessions', tab_accounts: 'Accounts', tab_settings: 'Settings', tab_help: 'Help', tab_office: 'Office',
    claude_desktop: 'Claude Code {v} (desktop app)', claude_cli: 'Claude Code {v} (CLI)', claude_checking: 'Checking Claude Code…',
    claude_missing: 'Claude Code not found',
    mode_auto: 'Auto switch', mode_confirm: 'Ask before switching', mode_off: 'Switching off', mode_unset: 'Switch mode not set',
    lang_label: 'Language',
    theme_label: 'Theme', theme_title: 'Window colours: as Windows has them, or always light or dark',
    theme_auto: '◐ Auto', theme_light: '☀ Light', theme_dark: '☾ Dark',
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
    also_open_hint: 'This conversation is also open in {w}. baton moves this session to a copy as soon as Claude finishes its current response.',
    ao_desktop: 'the Claude desktop app', ao_terminal: 'a terminal', ao_vscode: 'VS Code', ao_account: 'another session on {a}', ao_other: 'another Claude Code window',
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
    claude_app_setup: 'The Claude desktop app has not set up Claude Code yet. Open its Code tab once.',
    claude_cli_old: 'The Claude Code CLI on this PC ({v}) is too old for baton (needs {min} or newer).',
    claude_bundle_off: 'Using the Claude Code inside the Claude desktop app is turned off. Turn it on again in Settings → Advanced.',
    // choosing which Claude Code to use (Settings → Advanced)
    claude_app_old: 'The Claude desktop app\'s Claude Code ({v}) is too old. Update the Claude desktop app (Help → Check for updates) and open its Code tab once.',
    claude_none_old: 'Claude Code was found, but every copy is too old for baton (needs {min} or newer). Update one in Settings → Advanced → Claude Code to use, or update the Claude desktop app.',
    claude_app_installing: 'The Claude desktop app is still installing its Claude Code. Wait a moment, then check again.',
    cc_auto: 'Automatic (recommended)', cc_auto_now: 'The newest Claude Code on this PC that works with baton (now: {v}, {src})', cc_auto_none: 'The newest Claude Code on this PC that works with baton (none right now)',
    cc_desktop: 'The Claude desktop app\'s', cc_desktop_now: 'Follows the app\'s updates (now: {v})', cc_desktop_none: 'Follows the app\'s updates (none that works right now)',
    cc_other: 'Other (full path to claude.exe)', cc_other_hint: 'e.g. C:\\Users\\…\\claude.exe', cc_in_use: 'in use', cc_update: 'Update', cc_updating: 'Updating…',
    cc_update_started: 'Claude Code is updating in a window of its own. The new version shows here when it is done.',
    cc_why_too_old: 'too old for baton (needs {min} or newer)', cc_why_too_old_app: 'too old for baton (needs {min} or newer): update the Claude desktop app', cc_why_not_runnable: 'does not start',
    cc_why_installing: 'being installed by the Claude app', cc_why_wrapper: '.cmd wrapper; baton needs claude.exe', cc_why_known_bad: 'has a known problem with baton',
    cc_why_missing: 'no longer there', cc_why_old: 'it is too old',
    cc_src_desktop: 'Claude desktop app', cc_src_native: 'native installer', cc_src_npm: 'npm', cc_src_winget: 'WinGet', cc_src_scoop: 'Scoop', cc_src_path: 'PATH', cc_src_config: 'path you set',
    cc_fallback: 'The Claude Code you chose is not available right now: {why}. baton uses Claude Code {v} ({src}) until it can be used again.',
    need_claude_old: 'No Claude Code on this PC works with baton, so sessions cannot start.', claude_unusable: 'No usable Claude Code', cc_update_cli: 'Update the CLI',
    cc_note: 'Sessions already running keep their Claude Code; this applies to sessions started from now on.',
    err_not_updatable: 'This Claude Code cannot be updated from here. Press Check again, then try again.', err_claude_updating: 'Claude Code is already being updated. Wait for that window to finish.',
    // what to do about a Claude Code CLI too old for baton (the banner, the first-run screen)
    claude_cli_old_btn: 'Press Update the CLI to have it update itself (claude update).',
    claude_cli_old_later: 'After this setup, press Update the CLI at the top of the window, or run claude update in PowerShell.',
    claude_cli_old_app: 'If you do not use the CLI itself, opening the Code tab of the Claude desktop app once also works: baton then uses the Claude Code that comes with the app.',
    cc_why_not_set_up: 'the Claude desktop app has not set up its Claude Code yet (open its Code tab once)',
    recheck: 'Check again', elevated: 'baton is running as administrator, so sessions would too. Close it and open it again normally.',
    start_title: 'Start a session', start_folder: 'Project folder', browse: 'Browse', recent_folders: 'Recent folders',
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
    held_desktop: 'the Claude desktop app', held_cli: 'Claude Code in a terminal (CLI)', held_vscode: 'VS Code', held_other: 'another Claude Code window',
    held_recheck: 'Check again', import_go_copy: 'Continue in a copy', held_still: 'Still open.', held_free: 'Closed now: it can go on as the same conversation.',
    // a conversation open elsewhere goes on in a copy; one baton runs comes to the front
    copy_auto: 'It is open in {app}, so it continues in a copy: everything said so far, going on separately from the original.',
    held_recheck_hint: 'If you quit {app} first, the same conversation can go on instead.',
    conv_in_baton: 'This conversation is already running in baton: Start brings its window to the front.',
    in_baton_front: 'This conversation is already running in baton; its window is in front now.',
    err_in_baton: 'This conversation is already running in baton. Its row is under Sessions.',
    err_deleting: 'This conversation is being deleted.',
    // deleting a conversation (to the Recycle Bin), and what keeps one from going now
    del_btn: 'Delete conversation', del_title: 'Delete this conversation?', del_loading: 'Looking at what would go…',
    del_leftover: 'Leftover files of a deleted conversation',
    del_body: 'baton moves the conversation and what Claude Code saved with it (subagents, saved tool outputs, /rewind checkpoints) to the Recycle Bin. It disappears from baton, from Claude Code\'s resume list and from the office. You can restore it from the Recycle Bin.',
    del_outside: 'It was made outside baton ({src}); it goes from there too.',
    del_desktop_off: 'baton also takes it off the Claude desktop app for: {a}.', del_app_account: 'the desktop app\'s own account',
    del_desktop_off_done: 'It no longer shows in the Claude desktop app for: {a}. You can show it there again with “{b}”.',
    del_copies: '{n} other conversations went on from this one. They keep everything said, but can no longer open the large tool outputs saved with this one.',
    del_copies_one: 'Another conversation went on from this one. It keeps everything said, but can no longer open the large tool outputs saved with this one.',
    del_copies_more: 'baton could not read every other conversation in its folder: more of them may use the tool outputs saved with this one.',
    del_kept: 'Your project files, the folder\'s memory, Claude Code\'s prompt history and your other conversations stay as they are.',
    del_go: 'Move to Recycle Bin', del_done: 'Moved the conversation to the Recycle Bin.',
    del_reopened: 'A Claude Code opened it again meanwhile, so it may come back with what that one writes.',
    del_still: 'Checked again: still the same.', del_free: 'Nothing keeps it now.',
    del_in_baton: 'It is running in baton. End that session first.',
    err_gone: 'It is gone already.',
    err_bad_session: 'baton no longer finds this conversation where it was listed. Close this and look again.',
    err_copy_starting: 'A copy of it is starting in baton. Try again after the copy\'s first message.',
    err_open_elsewhere: 'It is open in {app}. Close it there first.',
    err_open_elsewhere_desktop: 'It is open in the Claude desktop app. Quit the desktop app completely (also from its tray icon) first.',
    err_desktop_app: 'The Claude desktop app has it as its own session. Delete it in the desktop app.',
    err_desktop_app_prior: 'The desktop app keeps it as an earlier part of one of its sessions. Delete that session in the desktop app first.',
    err_desktop_busy: 'The Claude desktop app has a list open that shows it. Quit the desktop app completely (also from its tray icon), then try again.',
    err_desktop_waiting: 'The Claude desktop app has a list open that shows it. Quit the desktop app completely (also from its tray icon), then try again.',
    err_two_files: 'This conversation has two different files; baton can\'t tell which to delete.',
    err_other_name: 'Its file also has a name outside Claude Code\'s folders, so baton leaves it alone.',
    err_cannot_check: 'baton couldn\'t check whether it is open right now. Try again in a moment.',
    err_no_recycle_bin: 'Windows can\'t put it in the Recycle Bin ({d}), so nothing was deleted.',
    err_path_too_long: 'A path in it is too long for the Recycle Bin, so nothing was deleted.',
    err_in_use: 'Another program is using one of its files. Try again in a moment.',
    err_partly: 'Only part of it went to the Recycle Bin. Try again.',
    err_linked_folder: 'Its file is in a folder reached through a link or junction, or in one baton can\'t read, so baton leaves it as it is.',
    err_session_window: 'A baton session that had it has ended, but its window is still open. Close that window, then try again.',
    err_recycle_failed: 'Windows could not move it to the Recycle Bin, so nothing was deleted. Try again.',
    err_changed: 'Its files moved while it was being deleted, so it is still there. Try again.',
    err_delete: 'baton could not delete it just now. Try again.',
    // a session moving to a copy, a conversation running in baton, quitting the desktop app to go on in it
    run_moving: 'Moving to a copy', conv_running: 'Running in baton',
    held_recheck_hint_desktop: 'If you quit the Claude desktop app completely (also from its tray icon) first, the same conversation can go on instead.',
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
      'baton never reads or stores the credentials of its accounts; Claude Code signs in and refreshes each account itself. If you turn on switching the desktop app\'s sign-in, baton keeps copies of the app\'s sign-in files as they are, never decrypted or sent anywhere.',
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
    set_claude_which: 'Claude Code to use',
    claude_which_note: 'Either way, sessions open in a Claude Code window of their own (a terminal screen), not inside the desktop app.', set_claude_bundle: 'Also use the Claude Code inside the Claude desktop app',
    saved: 'Saved.', set_lang: 'Language', lang_auto: 'Same as Windows',
    help_title: 'How baton works',
    help_points: [
      'Each account has its own Claude Code profile; Claude Code manages its sign-in.',
      'A session opens as a Claude Code window. Work there as usual.',
      'When the account in use hits its limit, baton stops Claude Code safely and reopens the same conversation on the next account (about 5-10 seconds).',
      'The conversation and project folder stay the same; background commands that were running end and are not re-run.',
      'The Claude desktop app\'s own sign-in does not change unless you switch it yourself on the Accounts tab (an unofficial option, off unless you turn it on in Settings); baton\'s accounts are signed in separately.',
      'To paste a picture (a screenshot, say) in a session window, press Alt+V. Ctrl+V and the terminal\'s other paste keys paste only text. An image file is attached when you drag it onto the window or paste its full path; type @ and part of a name to point Claude at a file in the folder.',
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
    uninstall_go: 'Uninstall', uninstall_confirm: 'This window closes and the uninstall window asks once more. End running sessions first.', uninstall_ok: 'Open uninstall',
    office_set: 'Show the office (just for fun)',
    office_set_hint: 'Shows the Claude Code conversations on this PC as people at work in an office. An Office tab appears, and the office can pop out into a window of its own for another screen. It only reads the conversations and changes nothing.',
    office_window_title: 'baton office', office_floor_guide: 'Floor guide', office_follow: 'Follow the latest session', office_no_floors: 'No floors yet.',
    office_floor_sessions: '{n} sessions', office_floor_session1: '1 session', office_active_now: 'active now', office_quiet_more: '{n} quiet floors', office_quiet_more1: '1 quiet floor', office_quiet_less: 'Hide quiet floors',
    office_popout: 'Pop out', office_popout_tip: 'Opens the office alone in a window of its own, handy on another screen.',
    office_history: 'history', office_untitled: 'Untitled session · {id}',
    office_zoom_in: 'Zoom in', office_zoom_out: 'Zoom out', office_zoom_reset: 'Reset the view', office_hint: 'Drag to move · wheel to zoom · double-click to reset', office_lift: '{dir} {n}F',
    office_empty_title: 'No Claude has clocked in yet', office_empty_body: 'Start a conversation in Claude Code and you will see it at work here.',
    office_off_title: 'The office is closed', office_off: 'Turn on “Show the office” in Settings to open it again.',
    office_gone_title: 'Closing time', office_gone: 'The baton window closed, so the office did too.', office_reconnect: 'Connection lost, reconnecting…',
    office_f_project: 'Project', office_f_session: 'Session', office_f_account: 'Account', office_f_state: 'State', office_f_source: 'From',
    office_src_baton: 'baton', office_src_cli: 'Claude Code (terminal)', office_src_desktop: 'Claude desktop app', office_no_account: 'outside baton',
    office_todos: 'Meeting room whiteboard', office_people: 'People in the office', office_people_n: '{n} at work', office_people_empty: 'Nobody here.',
    office_log: 'Activity', office_log_empty: 'Nothing yet.', office_legend: 'Who works where',
    office_me: '{name} (me)', office_agent: 'Subagent', office_more_agents: '+{n} more', office_tools_n: '{n} tools', office_tools_n1: '1 tool',
    office_click_focus: 'click to bring its session window forward', office_secs: '{n}s', office_mins: '{n}m', office_hours: '{n}h',
    office_st_arriving: 'Arriving', office_st_working: 'Working', office_st_thinking: 'Thinking', office_st_talking: 'Talking', office_st_asking: 'Asking',
    office_st_error: 'Trouble', office_st_idle: 'Resting', office_st_waiting: 'Waiting', office_st_reporting: 'Reporting', office_st_leaving: 'Leaving',
    office_b_think: 'Thinking', office_b_secs: 'for {n}s', office_b_ask: 'Needs your answer', office_b_report: 'Reporting back',
    office_b_leave: '👋 Off home!', office_b_hello: '👋 Clocked in!', office_b_shift_out: '👋 My shift is over!', office_b_shift_in: '🔁 Taking over!',
    office_b_shift: 'Shift change: {d}', office_b_compact: 'Tidied up the conversation', office_b_denied: 'Stopped', office_b_clear: 'Fresh start!', office_b_end: 'Done for the day!',
    office_l_think: 'Thinking', office_l_fail: 'Failed: {d}', office_l_deny: 'Rejected or interrupted', office_l_compact: 'Compacted the long conversation',
    office_l_idle: 'Finished its turn, resting', office_l_wait: 'Finished its turn, waiting for its subagents', office_wait: 'Waiting for its subagents', office_l_hire: 'Hired {type}', office_l_report: 'Reported back', office_l_leave: 'Went home',
    office_l_ask: 'Asks: {d}', office_l_ask_plain: 'Waiting for your answer', office_l_shift: 'Shift change: {d}', office_l_clear: 'Started over with /clear', office_l_end: 'The session ended',
    office_room_library: 'Library', office_room_dev: 'Dev', office_room_server: 'Server room', office_room_search: 'Search', office_room_meeting: 'Meeting room',
    office_room_desk: 'My desk', office_room_seat: 'Claude’s desk', office_room_lounge: 'Lounge', office_room_elevator: 'Elevator',
    office_room_library_d: 'reading and searching files', office_room_dev_d: 'writing and editing code', office_room_server_d: 'commands, git', office_room_search_d: 'the web and outside tools',
    office_room_meeting_d: 'plans, to-dos, skills', office_room_desk_d: 'questions for you', office_room_seat_d: 'thinking, talking, calling agents and hearing back',
    office_room_lounge_d: 'a coffee once the turn is over and its agents left', office_room_elevator_d: 'arrivals, departures, shifts',
    // office: side columns, and what is hidden from it
    office_resize_side: 'Floor guide width', office_resize_panel: 'Panel width', office_resize_tip: 'Drag to resize · double-click to reset · Enter to fold or unfold', office_folded: 'folded',
    office_guide_btn: 'Floor guide', office_guide_tip: 'Opens the floor guide over the office', office_panel_btn: 'Panel', office_panel_tip: 'Opens the session, the people and the activity',
    office_show_s: 'Show in the office: {name}', office_show_floor: 'Show this floor (project) in the office: {name}', office_show_tip: 'Unticked items are hidden here in the office only; the sessions themselves are not touched.',
    office_hidden_more: 'Show {n} hidden', office_hidden_more1: 'Show 1 hidden', office_hidden_less: 'Fold hidden items', office_unhide_all: 'Show everything again',
    office_hidden_n: '{n} hidden', office_floor_off: 'floor hidden', office_follow_tip: 'hidden sessions are skipped', office_quiet_tip: 'Floors with no activity in the last 3 days',
    office_all_hidden_title: 'Every session is hidden', office_all_hidden: 'Tick a floor or a session in the floor guide to watch it here again.',
    err_office_off: 'The office is turned off. Turn it on in Settings.', err_no_popout: 'This baton cannot open the office in a window of its own.',
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
    err_deleting_any: 'A conversation is being deleted. Wait until it is done, then try again.',
    switch_now: 'Requested: it starts on {a} shortly.', switch_now_any: 'Requested: it starts on a free account shortly.',
    num_bad: 'Enter a whole number of 1 or more.',
    err_bad_folder: 'Choose an existing folder on this PC (e.g. C:\\Users\\…).', err_long_folder: 'That path is too long. Choose a folder with a shorter path.',
    ago_now: 'just now', ago_min: '{n} min ago', ago_hour: '{n} h ago', ago_day: '{n} d ago',
    // showing a session in the Claude desktop app
    dk_btn: 'Choose desktop app accounts', dk_btn_wait: 'It can show in the desktop app after its first message.',
    dk_title: 'Show in the Claude desktop app', dk_loading: 'Looking at the desktop app…', dk_apply: 'Apply',
    dk_intro: 'Pick the accounts whose Claude desktop app lists this session in its Code tab. It is not listed for the accounts you leave unticked. Nothing is uploaded: the desktop app opens the same conversation on this computer.',
    dk_active: 'Signed in to the desktop app now', dk_used: 'Shows when you switch to this account in the desktop app',
    dk_unused: 'Not used in the desktop app yet: it shows once you sign in with this account there',
    dk_same: '{a} and {b} are the same Claude account.', dk_app_only: 'The account the Claude desktop app is signed in to', dk_app_only_note: 'Not one of baton\'s accounts.',
    dk_former: 'No longer one of baton\'s accounts; untick it to take the session off this list.', dk_app_former: 'Another account of the desktop app',
    dk_why_api_key: 'Uses an API key, not a Claude sign-in, so the desktop app has no list for it.',
    dk_why_cloud: 'Uses {p}, not a Claude sign-in, so the desktop app has no list for it.',
    dk_why_unknown: 'baton does not know which Claude account this is yet. Press Check sign-ins on the Accounts tab.',
    dk_why_token: 'Signs in with a long-lived token, so baton cannot tell which Claude account it is.',
    dk_why_gateway: 'Signs in through a gateway, not a Claude sign-in, so the desktop app has no list for it.',
    dk_cloud_other: 'a cloud provider',
    dk_why_own: 'Made in the desktop app on this account, so it stays there. To hide it, archive it in the desktop app.',
    dk_why_archived: 'Archived in the desktop app on this account. To list it again, unarchive it there.',
    dk_why_released: 'You went on with it in the desktop app on this account, so it is the desktop app\'s now. To hide it, archive it there.',
    dk_why_deleted: 'You deleted it in the desktop app. Tick it to list it again.',
    dk_copy: 'The desktop app keeps the original you opened there; this tick is for baton\'s copy, “{t} (baton)”.',
    dk_archived: 'Archived in the desktop app; untick it to take it off the list.',
    dk_split: 'baton\'s conversation file and the desktop app\'s are no longer one file, so what is said on one side does not show on the other. baton stops updating this session in the desktop app. You can still untick accounts.',
    dk_problem_conflict: 'Your own Claude Code folder already has a different conversation file with this id, so it cannot be shown (baton never replaces that file).',
    dk_problem_other_drive: 'This conversation is on another drive than your own Claude Code folder, so the desktop app cannot open it.',
    dk_problem_link_failed: 'baton could not make this conversation openable for the desktop app.',
    dk_problem_no_file: 'It can show in the desktop app after its first message.',
    dk_running: 'The desktop app is open now: for the account it is signed in to, a change shows after you quit it completely (also from its tray icon) and open it again.',
    dk_tip_copy: 'If you open it in the desktop app while baton is running it, baton goes on in a copy so neither side overwrites the other. The desktop app keeps the original and the copy shows next to it.',
    dk_tip_hide: 'To hide it, untick it here rather than deleting it in the desktop app: deleting it there can let the conversation file be cleaned up later.',
    dk_tip_limits: 'Going on with it in the desktop app uses the limits of the account the desktop app is signed in to.',
    dk_done: 'Done.', dk_next_open: 'It shows the next time you open the Claude desktop app.',
    dk_restart: 'Quit the Claude desktop app (also from its tray icon) and open it again to see the change.',
    dk_switch: 'It shows when you switch to that account in the desktop app.', dk_gone_quit: 'It disappears from the desktop app after you quit it. Keep this window open until then.',
    dk_gone: 'It is no longer listed for the accounts you unticked.', dk_none: 'Nothing changed.',
    dk_pill: 'Desktop: {a}', dk_pill_split: '⚠ Desktop: files split', dk_app_account: 'desktop app account',
    err_no_file: 'It can show in the desktop app after its first message.', err_unknown_place: 'That account cannot be chosen now. Open this again.',
    err_split: 'baton\'s and the desktop app\'s conversation files split, so nothing new can be ticked.', err_no_desktop: 'The Claude desktop app was not found on this PC.',
    err_conflict: 'Your own Claude Code folder already has a different conversation file with this id, so it cannot be shown.',
    err_other_drive: 'This conversation is on another drive than your own Claude Code folder, so the desktop app cannot open it.',
    err_link_failed: 'baton could not make this conversation openable for the desktop app.',
    dk_kept: 'baton keeps its sign-in: you can switch the desktop app to it on the Accounts tab.',
    // switching the Claude desktop app's sign-in between your own accounts (unofficial; off unless turned on)
    dl_set: 'Switch the Claude desktop app between your accounts (unofficial)',
    dl_set_hint: 'baton keeps each account\'s desktop app sign-in on this PC and puts the one you choose back while the app is quit, so you switch without an email code. Off by default.',
    dl_set_where: 'Switch it on the Accounts tab.',
    dl_leftover: 'baton still keeps {n} desktop app sign-in(s) from before.', dl_leftover_go: 'Delete them',
    dl_notice_title: 'Before baton switches the desktop app\'s sign-in',
    dl_notice_points: [
      'This lets baton switch the Claude desktop app between your own Claude accounts without signing in again. It stays off unless you turn it on.',
      'How: while the desktop app is fully quit, baton moves the app\'s sign-in (its cookies, its site storage and six entries of its settings file, two of them encrypted tokens) into {path} and puts the chosen account\'s back. baton only copies and moves these files as they are: it never decrypts them and never sends them anywhere. The cookies and tokens stay encrypted as the app keeps them, usable only from your Windows account on this PC; the site storage is not encrypted, in the app either. Some of the app\'s own preferences kept in its site storage follow the account too.',
      'This is not an Anthropic feature. No Anthropic source says it is allowed, and it is not how the app is meant to be used. Anthropic may treat it, especially switching after a usage limit, as getting around its products or limits, and can suspend accounts. baton does not hide that one PC uses several accounts. The risk to your accounts is yours.',
      'It is unofficial and can stop working with any update of the desktop app; the app may then ask you to sign in again (an email code). baton changes nothing when the app looks different from what it knows.',
      'It switches only when you ask, and only while the desktop app is fully quit. If the app runs, baton quits it first, only when nothing in it is working, and opens it again after. It never switches on its own, and never because of a usage limit.',
      'Only your own accounts. While this is on, use Log out in the Claude desktop app only for an account you want to stop using: Log out ends that sign-in on Claude\'s servers, so baton\'s copy of it stops working too (signing in again needs an email code). To add an account, use “Add an account to the desktop app” on baton\'s Accounts tab.',
      'Turning this off, or uninstalling baton, deletes every sign-in baton kept. The desktop app stays signed in to the account it is on. This does not sign the accounts out on Claude\'s servers: to end a sign-in too, switch the app to it, use Log out there, then forget it in baton.',
    ],
    dl_notice_ack: 'I have read this, I accept the risk to my accounts, and I want baton to switch the desktop app\'s sign-in when I ask.',
    dl_on: 'Turn on', dl_on_done: 'Switching the desktop app\'s sign-in is on. Switch it on the Accounts tab.',
    dl_off_title: 'Turn off switching the desktop app\'s sign-in?',
    dl_off_body: 'baton deletes the {n} sign-in(s) it keeps for the desktop app.',
    dl_off_none: 'baton keeps no sign-in of the desktop app yet, so nothing is deleted.',
    dl_off_stays: 'The desktop app stays signed in to {a}. To use another account there again, sign in to it in the app (an email code).',
    dl_off_blank: 'The desktop app is signed out now, so it will ask you to sign in.',
    dl_off_go: 'Turn off and delete them', dl_off_go_none: 'Turn off', dl_off_done: 'Switching the desktop app\'s sign-in is off.',
    dl_not_revoked: 'Deleting them does not sign the accounts out on Claude\'s servers.',
    dl_title: 'Claude desktop app sign-in',
    dl_sub: 'Choose the account the Claude desktop app signs in with. If the app runs, baton checks that nothing in it is working, quits it, switches, and opens it again.',
    dl_pill_running: 'Running', dl_pill_quit: 'Not running',
    dl_running: 'The desktop app is running. To switch, baton checks that nothing in it is working, quits it, and opens it again.',
    dl_running_quit_it: 'The desktop app is running. Quit it completely (also from its tray icon) first.',
    dl_loading: 'Looking at the desktop app\'s sign-ins…', dl_switching: 'Switching the desktop app\'s sign-in…',
    dl_now: 'Signed in now', dl_baton: 'baton: {a}', dl_not_baton: 'Not one of baton\'s accounts',
    dl_kept: 'Kept {t}', dl_not_kept_pill: 'Not kept yet', dl_not_kept: 'Not kept yet: baton keeps it when you switch away.',
    dl_damaged_pill: 'Not whole',
    dl_signed_out: 'The desktop app is signed out on this PC. Open it and sign in to the account to add.',
    dl_none: 'baton keeps no sign-in of the desktop app yet.',
    dl_use: 'Switch the desktop app to this account', dl_use_now: 'The desktop app is on this account now.', dl_use_not_kept: 'baton keeps no sign-in for it yet.',
    dl_new: 'Add an account to the desktop app', dl_new_title: 'Add an account to the desktop app',
    dl_new_body: 'baton keeps the sign-in of {a} and empties the desktop app\'s sign-in on this PC only. Nothing is sent to Claude, so {a} stays signed in where baton keeps it. Then sign in to the other account in the desktop app (an email code), and switch between them here.',
    dl_new_blank: 'The desktop app has no sign-in to keep. baton sets aside what is left of it on this PC and empties it. Then sign in to the account to add in the desktop app (an email code), and switch between your accounts here.',
    dl_new_no_logout: 'Do not use the app\'s Log out for this: it ends the sign-in on Claude\'s servers too, and the kept copy stops working.',
    dl_new_go: 'Keep it and empty the app\'s sign-in',
    dl_done: 'The desktop app is now on {a}. Open it to check.', dl_done_open: 'The desktop app is now on {a}. Opening it.',
    dl_done_blank: 'The desktop app is signed out on this PC. Open it and sign in to the other account.',
    dl_done_blank_open: 'The desktop app is signed out on this PC and opening now. Sign in to the other account.',
    dl_open_yourself: 'Open the desktop app yourself.',
    dl_quit_title: 'Quit the desktop app and switch it to {a}?',
    dl_quit_body: 'The desktop app is running. baton checks that nothing in it is working, quits it, switches it to {a}, and opens it again.',
    dl_quit_check: 'If Claude is working or waiting for your answer in one of its sessions, or something runs there in the background (a command, an agent, a workflow, a /loop), baton does not quit it and tells you which session.',
    dl_quit_go: 'Quit and switch',
    dl_new_quit: 'The desktop app is running. When nothing in it is working, baton quits it, empties its sign-in, and opens it again.',
    dl_done_quit: 'baton quit the desktop app and switched it to {a}. Opening it again.',
    dl_done_blank_quit: 'baton quit the desktop app and signed it out on this PC; it is opening again. Sign in to the other account.',
    dl_reopened: 'The desktop app was opened again, on the account it was on.',
    dl_reopen_yourself: 'The desktop app is on the account it was on; open it yourself.',
    dl_untitled: '(untitled)',
    dl_work_turn: 'Claude is working', dl_work_waiting: 'Claude is waiting for your answer', dl_work_command: 'a command runs in the background',
    dl_work_agent: 'a background agent or workflow runs', dl_work_wakeup: 'Claude is to wake up again (a /loop or a scheduled job)',
    dl_work_unknown: 'baton cannot tell now whether it is working (try again in a moment)',
    dl_recovered_first: 'A switch that was cut off was undone first.',
    dl_stashed: 'What the signed-out desktop app had left was set aside (no token recorded); you can delete it on the Accounts tab.',
    dl_rename: 'Rename', dl_rename_title: 'Rename “{a}”', dl_rename_label: 'Name (only used inside baton)',
    dl_rename_hint: 'Up to 40 characters. Leave it empty to go back to the name baton gives it.', dl_rename_go: 'Save the name', dl_renamed: 'Name saved.',
    dl_forget: 'Forget', dl_forget_title: 'Forget the sign-in of “{a}”?',
    dl_forget_body: 'baton deletes the sign-in of {a} it keeps. The desktop app is not changed. To switch to {a} again, sign in to it in the app (an email code).',
    dl_forget_servers: 'This does not sign the account out on Claude\'s servers. To end that sign-in too, switch the app to it, use Log out there, then forget it here.',
    dl_forget_go: 'Delete the kept sign-in', dl_forgot: 'Deleted the kept sign-in of {a}.',
    dl_unnamed: 'Left by the signed-out app (no token recorded)', dl_unnamed_kept: 'set aside {t}',
    dl_unnamed_title: 'Delete what the signed-out app left?', dl_unnamed_body: 'baton deletes what the desktop app had left while no sign-in token was recorded in it.',
    dl_unnamed_go: 'Delete it', dl_unnamed_gone: 'Deleted.',
    dl_forget_all: 'Delete all kept sign-ins', dl_forget_all_title: 'Delete every kept sign-in?',
    dl_forget_all_body: 'baton deletes the {n} sign-in(s) it keeps for the Claude desktop app.',
    dl_forget_all_stays: 'The desktop app stays signed in to {a}.',
    // what signed-out apps left (no token recorded), counted apart from the sign-ins
    dl_unnamed_too: 'It also deletes {n} item(s) a signed-out desktop app left (no token recorded).',
    dl_unnamed_only: 'baton deletes {n} item(s) a signed-out desktop app left (no token recorded). It keeps no sign-in.',
    dl_leftover_unnamed: 'baton still keeps {n} item(s) a signed-out desktop app left (no token recorded).',
    dl_leftover_unnamed_too: 'It also keeps {n} item(s) a signed-out desktop app left (no token recorded).',
    dl_forget_all_go: 'Delete them all', dl_forgot_all: 'Deleted {n} kept sign-in(s).',
    dl_open_after: 'Open the desktop app afterwards',
    dl_no_logout: 'While this is on, use Log out in the Claude desktop app only for an account you want to stop using: Log out ends that sign-in on Claude\'s servers, so baton\'s copy of it stops working too (signing in again needs an email code). To add an account, use “Add an account to the desktop app” here.',
    dl_cutoff: 'A switch of the desktop app\'s sign-in was cut off. Quit the desktop app completely, then put it back as it was.', dl_putback: 'Put it back',
    dl_recovered: 'A switch that was cut off was undone: the desktop app is back on {a}.', dl_recovered_blank: 'A switch that was cut off was undone: the desktop app is signed out again.',
    dl_recovered_none: 'No switch was cut off; there was nothing to put back.',
    // a switch that was cut off and is not put back: the app can be left as it is instead
    dl_leave: 'Leave it as it is', dl_leave_title: 'Leave the desktop app as it is?',
    dl_leave_body: 'It stays signed in to the account it is on now, and baton deletes what it set aside for this switch. Sign-ins baton kept stay kept.',
    dl_leave_hint: 'If putting it back keeps failing, you can leave the app as it is.',
    dl_left: 'Left the desktop app as it is: it stays signed in to {a}.',
    dl_left_blank: 'Left the desktop app as it is: it is signed out.',
    dl_left_none: 'No switch was cut off; there was nothing to settle.',
    // copies a switch that stopped early left in the app's folder
    dl_copies: 'A switch that stopped early left {n} copy(ies) in the desktop app\'s folder (of the app\'s settings and a sign-in\'s files).',
    dl_copies_go: 'Delete the copies', dl_copies_gone: 'Deleted the copies.',
    dl_acct_pill: 'Desktop app: signed in now',
    dl_remove_note: 'The desktop app sign-in baton keeps for this Claude account stays; delete it on the Accounts tab.',
    // uninstalling while a switch that was cut off is not settled: said before the window closes
    err_cut_off_running: 'A switch of the Claude desktop app\'s sign-in was cut off. Quit the desktop app completely (also from its tray icon) first, so baton can settle it, then uninstall.',
    err_cut_off_inside_claude: 'A switch of the Claude desktop app\'s sign-in was cut off, and baton settles it only when run outside Claude Code; this window was opened from inside it (CLAUDECODE is set). Close this window, open baton again from the Start menu (or from its folder), then uninstall.',
    err_dl_off: 'Switching the desktop app\'s sign-in is turned off. Turn it on in Settings.',
    err_dl_inside_claude: 'baton\'s window was opened from inside Claude Code (CLAUDECODE is set), so it does not switch the desktop app\'s sign-in. Close this window and open baton again from the Start menu (or from its folder).',
    err_dl_unsupported_layout: 'Switching the sign-in is not supported for this kind of Claude desktop app install yet.',
    err_dl_unsupported_layout_no_app: 'baton finds no data folder of the Claude desktop app on this PC. Open the desktop app once, then try again.',
    err_dl_unsupported_layout_several: 'More than one folder in Claude-Data (in your local app data) holds the desktop app\'s settings, perhaps left by a reinstall, so baton cannot tell which one the app uses; it changes nothing.',
    err_dl_unsupported_layout_link: 'The desktop app\'s data folder is a link to another place, which baton does not switch.',
    err_dl_synced_store: 'baton\'s data folder is in OneDrive or another folder that syncs off this PC, so baton keeps no sign-in there. Move it (BATON_HOME) to a folder that stays on this PC.',
    err_dl_elevated: 'baton is running as administrator. Open it again normally to switch the desktop app\'s sign-in.',
    err_dl_busy: 'Another change to the desktop app is under way. Try again in a moment.',
    err_dl_app_running: 'The Claude desktop app is still running. Quit it completely (also from its tray icon), then try again.',
    err_dl_app_working: 'Something in the desktop app is still working, so baton did not quit it and changed nothing. Stop it (or let it finish), then try again.',
    err_dl_quit_failed: 'The Claude desktop app did not quit in time, so its sign-in was not changed. It may be asking you something, or still be closing: quit it yourself (also from its tray icon), then try again.',
    err_dl_quit_failed_refused: 'The Claude desktop app would not quit now, so nothing was changed. Quit it yourself (also from its tray icon), then try again.',
    err_dl_unknown_format: 'The desktop app keeps its sign-in in a way this baton does not know (the app may have been updated). Nothing was changed, and the app stays signed in to the account it is on. Update baton before switching again.',
    err_dl_already: 'The desktop app is already signed in to that account.',
    err_dl_unsaved_current: 'baton could not keep the sign-in the desktop app has now, so nothing was changed.',
    err_dl_unsaved_no_account: 'The desktop app is signed in but has not said to which account yet, so baton cannot keep that sign-in; nothing was changed. Open the desktop app once, wait until it shows your account, quit it completely (also from its tray icon), then try again.',
    err_dl_unsaved_store_permissions: 'baton could not give the folder where it keeps sign-ins permissions of its own (only you and the system), so it kept nothing and changed nothing.',
    err_dl_unsaved_kept_unreadable: 'baton could not read the sign-in it keeps for {a}, so it cannot tell whose sign-in the desktop app has now; nothing was changed. Try again in a moment. If that keeps failing, press Forget on {a} in the list.',
    err_dl_not_kept: 'baton keeps no sign-in for that account. Sign in to it once in the desktop app (Add an account to the desktop app), then you can switch to it.',
    err_dl_ambiguous: 'That name fits more than one account ({a}).',
    err_dl_damaged: 'The sign-in baton kept for that account is incomplete, so nothing was changed. Forget it and sign in to the account again in the app.',
    err_dl_tokens_of_another: 'The sign-in the desktop app has now carries the tokens baton keeps for another account, so whose it is cannot be told; nothing was changed. Open the desktop app once, quit it completely (also from its tray icon), then try again.',
    err_dl_switch_failed: 'The switch did not finish, and baton put the desktop app back as it was.',
    err_dl_switch_unrestored: 'The switch did not finish and baton could not put everything back yet. Do not open the desktop app; press Put it back at the top of this window.',
    err_dl_settle_unrestored: 'baton could not leave the desktop app as it is yet: another program may be using one of the app\'s files for a moment. Do not open the desktop app; try again in a moment.',
    err_dl_used_since: 'The desktop app has been used on the sign-in this switch put in, and baton cannot keep that sign-in now (switching is off, or baton\'s folder syncs). Putting the switch back would throw it away, so nothing was changed. You can leave the app as it is instead.',
    err_dl_bad_label: 'That name cannot be used.', err_dl_bad_label_too_long: 'A name can be at most 40 characters.',
    err_dl_bad_label_characters: 'A name cannot hold control characters, line separators or commas.',
    err_dl_bad_label_looks_like_id: 'A name cannot look like an account id (8 or more hex digits).',
    err_dl_bad_label_taken: 'Another account already goes by that name.',
    err_dl_launch_unsupported: 'baton cannot open the desktop app from here; open it yourself.',
    err_needs_desktop_ack: 'Read and accept the notice first, then turn it on.',
    err_desktop_logins: 'baton could not read or change the desktop app\'s sign-ins just now. Try again.',
  },
};

const S = { state: null, lang: 'ko', tab: null, lastJSON: '', onboardingShown: false, mainDirty: false,
  ui: { doctorOut: null, doctorOk: true, doctorRunning: false, advancedOpen: false, signinPrivate: false, dismissed: {}, runOpen: {} },
  // dl: the desktop app's sign-ins as the card last read them (desktop/logins), read again when the
  // state's summary of them changes (key); busy while a change is under way.
  dl: { key: '', view: null, err: null, loading: false, busy: false, recoverErr: null } };

function t(key, vars) {
  let s = (T[S.lang] && T[S.lang][key]) ?? T.en[key] ?? key;
  // A value goes in as it is: given as a string, $& or $' in a name or a path would be read as patterns.
  if (vars && typeof s === 'string') for (const [k, v] of Object.entries(vars)) s = s.replaceAll('{' + k + '}', () => String(v));
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

// The office opens alone in a window of its own with #office in its address
// (read here, before connect() tidies the address; kept for reloads).
const officeOnly = location.hash === '#office';

async function connect() {
  const k = new URLSearchParams(location.search).get('k');
  if (!k) return;
  history.replaceState(null, '', officeOnly ? '/#office' : '/');
  const r = await fetch('/api/session', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code: k }) });
  if (!r.ok) return;
  token = (await r.json()).token || '';
  try { sessionStorage.setItem('baton_token', token); } catch (_) { /* this load only */ }
}

// api calls the GUI's API; signal (optional) lets a long poll be called off.
async function api(path, body, signal) {
  const opts = { method: body === undefined ? 'GET' : 'POST', headers: ids(), credentials: 'omit', signal };
  if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
  const r = await fetch('/api/' + path, opts);
  let data = {};
  try { data = await r.json(); } catch (_) { /* empty */ }
  if (!r.ok) {
    const e = new Error(data.error || r.statusText);
    e.code = data.code; e.details = data.details; e.status = r.status;
    e.data = data; // more of why, where the server says it (a delete's blocker)
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
    if (window.batonTheme) window.batonTheme.set(st.config.gui_theme || 'auto'); // also a change made in another window
    if (!S.tab) S.tab = st.accounts.length ? 'sessions' : 'accounts';
    if (json !== S.lastJSON) { S.lastJSON = json; render(); }
    if (!officeOnly) maybeOnboard();
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
  if (S.tab === 'office' && !st.config.office) S.tab = 'sessions';
  if (officeOnly) { renderOfficeOnly(); return; }
  renderTop(st);
  renderTabs(st);
  renderBanners(st);
  const main = document.getElementById('main');
  document.getElementById('app').classList.toggle('wide', S.tab === 'office');
  // The office keeps its own page alive across redraws: it animates.
  if (S.tab === 'office') { showOffice(main); return; }
  hideOffice();
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

// claudeChip says which Claude Code sessions start with; a click shows where
// to choose another one.
function claudeChip(st) {
  if (st.claude_pending) return h('span', { class: 'chip', text: t('claude_checking') });
  const c = st.claude;
  const open = () => { S.tab = 'settings'; S.ui.advancedOpen = true; render(); const el = document.getElementById('claude-pick'); if (el) el.scrollIntoView({ block: 'center' }); };
  if (!c.found) {
    const some = (c.installs || []).some((i) => i.problem !== 'wrapper');
    return h('button', { class: 'chip bad', text: t(some ? 'claude_unusable' : 'claude_missing'), title: c.error || '', onclick: open });
  }
  const label = t(c.source === 'desktop' ? 'claude_desktop' : 'claude_cli', { v: c.version });
  return h('button', { class: 'chip ok', text: label, title: c.path + (c.fallback ? '\n' + claudeFallbackText(c) : ''), onclick: open });
}

// ---- which Claude Code (Settings → Advanced) ------------------------------------------

function claudeSource(src) { return t('cc_src_' + src); }

// claudeWhy says why a copy of Claude Code cannot be used.
function claudeWhy(i, min) {
  if (i.problem === 'too_old') return t(i.source === 'desktop' ? 'cc_why_too_old_app' : 'cc_why_too_old', { min });
  return t('cc_why_' + i.problem);
}

// claudeFallbackText: the copy chosen cannot be used now, and which one is used
// instead (the chosen one is the selected row, so its path is not repeated).
function claudeFallbackText(c) {
  const fb = c.fallback;
  const why = fb.why === 'too_old' ? t('cc_why_old') : t('cc_why_' + fb.why);
  return t('cc_fallback', { why, v: c.version, src: claudeSource(c.source) });
}

// claudeOldCli is the newest Claude Code CLI found that is only too old.
function claudeOldCli(c) {
  return (c.installs || []).filter((i) => i.source !== 'desktop' && i.problem === 'too_old')
    .sort((a, b) => b.version.localeCompare(a.version, undefined, { numeric: true }))[0];
}

// claudeProblemText says why no Claude Code can be used and what to do: on
// the banner, beside its Update the CLI button, and on the first-run screen
// (inDialog), which has no such button.
function claudeProblemText(st, inDialog) {
  const c = st.claude, ins = c.installs || [];
  const oldCli = claudeOldCli(c);
  const desk = ins.find((i) => i.source === 'desktop'); // the list has the app's newest copy first
  if (c.bundle_off) return t('claude_bundle_off');
  if (oldCli && desk && desk.problem === 'too_old') return t('claude_none_old', { min: c.min });
  if (oldCli) {
    // The desktop app is there but its Code tab was never opened: opening
    // it gives a new enough Claude Code too.
    const app = !desk && c.desktop_app && (st.config.allow_desktop_bundle || st.config.claude_path === 'desktop');
    return [t('claude_cli_old', { v: oldCli.version, min: c.min }), t(inDialog ? 'claude_cli_old_later' : 'claude_cli_old_btn'),
      app ? t('claude_cli_old_app') : ''].filter(Boolean).join(' ');
  }
  if (desk && desk.problem === 'too_old') return t('claude_app_old', { v: desk.version });
  if (desk && desk.problem === 'installing') return t('claude_app_installing');
  return c.desktop_app ? t('claude_app_setup') : t('ob_claude_missing');
}

// claudeErrorDetails: baton's own message lists every copy and its fix, in
// English. It is kept under Details, since the text above it and Settings say
// the same; its lines are kept as they are.
function claudeErrorDetails(c) {
  return c.error ? h('details', { class: 'small muted mt6', open: S.ui.claudeErrOpen, ontoggle: (e) => { S.ui.claudeErrOpen = e.target.open; } },
    h('summary', { text: t('details') }), h('div', { class: 'prewrap', text: c.error })) : null;
}

const samePath = (a, b) => !!a && !!b && a.toLowerCase() === b.toLowerCase();

// claudeUpdate runs an old Claude Code CLI's own update in a window of its own.
async function claudeUpdate(path) {
  try { await api('claude/update', { path }); toast(t('cc_update_started')); S.lastJSON = ''; await refresh(); } catch (e) { showError(e); }
}

function claudeUpdateButton(st, i, label, primary) {
  const busy = st.jobs.some((j) => j.kind === 'claude-update' && j.running && j.target === i.path);
  return h('button', { class: 'btn' + (primary ? ' primary' : ''), disabled: busy, onclick: () => claudeUpdate(i.path) },
    busy ? h('span', { class: 'spin' }) : null, busy ? ' ' + t('cc_updating') : label);
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
  const theme = st.config.gui_theme || 'auto';
  const look = h('select', { 'aria-label': t('theme_label'), title: t('theme_title'), class: 'chip',
    onchange: (e) => { if (window.batonTheme) window.batonTheme.set(e.target.value); setConfig('gui_theme', e.target.value, false, true); } },
    ['auto', 'light', 'dark'].map((v) => h('option', { value: v, text: t('theme_' + v), selected: theme === v })));
  look.classList.add('auto-w');
  document.getElementById('topbar').replaceChildren(claudeChip(st), modeChip(st), look, sel);
}

function renderTabs(st) {
  const tabs = [['sessions', st.runs.length], st.config.office ? ['office'] : null, ['accounts', st.accounts.length], ['settings'], ['help']].filter(Boolean);
  document.getElementById('tabs').replaceChildren(...tabs.map(([id, n]) => h('button', {
    role: 'tab', 'aria-selected': String(S.tab === id), onclick: () => { S.tab = id; render(); document.getElementById('main').focus(); },
  }, t('tab_' + id), n ? h('span', { class: 'count', text: String(n) }) : null)));
}

function renderBanners(st) {
  const out = [];
  for (const p of st.problems || []) out.push(h('div', { class: 'banner bad' }, h('div', { class: 'grow', text: t('problem', { e: p }) })));
  if (st.elevated) out.push(h('div', { class: 'banner warn' }, h('div', { class: 'grow', text: t('elevated') })));
  if (!st.claude_pending && !st.claude.found) {
    const c = st.claude, oldCli = claudeOldCli(c);
    const some = (c.installs || []).some((i) => i.problem !== 'wrapper');
    out.push(h('div', { class: 'banner warn' }, h('div', { class: 'grow' }, h('b', { text: t(some ? 'need_claude_old' : 'need_claude') }),
      h('div', { text: claudeProblemText(st, false) }), claudeErrorDetails(c)),
      oldCli ? claudeUpdateButton(st, oldCli, t('cc_update_cli'), true) : null,
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
  // A switch of the desktop app's sign-in that was cut off, on every tab and also while
  // switching is off: the app must not be opened half switched.
  // When Put it back was refused in a way waiting cannot mend, the banner says why and offers
  // to leave the app as it is instead (it asks first).
  const dl = st.desktop_logins;
  if (dl && dl.cut_off) {
    const why = S.dl.recoverErr, asIs = !!why && leavable(why);
    const text = !why ? t('dl_cutoff') : loginErr(why) + (asIs && why.code !== 'dl_used_since' ? ' ' + t('dl_leave_hint') : '');
    out.push(h('div', { class: 'banner warn' }, h('div', { class: 'grow', text }),
      h('button', { class: 'btn' + (asIs ? '' : ' primary'), text: t('dl_putback'), disabled: dl.running || S.dl.busy, title: dl.running ? t('dl_running_quit_it') : '',
        onclick: () => recoverLogins() }),
      asIs ? h('button', { class: 'btn primary', text: t('dl_leave'), disabled: dl.running || S.dl.busy, title: dl.running ? t('dl_running_quit_it') : '',
        onclick: () => openSettle() }) : null));
  } else if (dl) S.dl.recoverErr = null;
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
    st.recent_sessions.length ? h('div', {}, st.recent_sessions.map((r) => h('div', { class: 'row recent' },
      h('div', { class: 'grow' },
        h('div', { class: 'title' }, h('span', { text: convTitle(r) }), deskPills(r.desktop)),
        h('div', { class: 'sub', title: r.cwd, text: `${baseName(r.cwd)} · ${ago(r.ended)} · ${r.where === 'claude-code' ? t('conv_where_home') : t('conv_where', { a: r.where })}` })),
      h('div', { class: 'actions' },
        st.desktop ? h('button', { class: 'btn ghost', text: t('dk_btn'), onclick: () => openDesktop(r.session_id) }) : null,
        h('button', { class: 'btn ghost danger', text: t('del_btn'), onclick: () => openDelete(r) }),
        h('button', { class: 'btn', text: t('continue_btn'), disabled: !st.claude.found || !canStart,
          onclick: () => startSession({ folder: r.cwd, mode: 'pick', session_id: r.session_id, where: r.where,
            conv: { session_id: r.session_id, where: r.where, title: r.title, first_prompt: r.first_prompt, cwd: r.cwd, modified: r.ended } }) })))))
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
  // A move to a copy (the conversation was opened elsewhere) is not an account switch.
  const moving = r.moving && (r.state === 'stopping' || r.state === 'switching');
  return h('span', { class: 'pill ' + cls, text: moving ? t('run_moving') : t('run_' + r.state) });
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
  } }, h('option', { value: '', text: t('switch_btn') }),
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
  // The conversation open somewhere else too: two writers split it.
  const places = [...new Set((r.also_open || []).map((o) => t('ao_' + o.where, { a: o.account || '' })))];
  const elsewhere = places.join(', ');
  const toggle = h('button', { class: 'btn ghost details-btn', 'aria-expanded': String(open), text: open ? t('details_less') : t('details_more'),
    onclick: () => { S.ui.runOpen[r.run_id] = !open; render(); } });
  const facts = [
    h('span', { class: 'fact strong', text: r.account ? t('run_on', { a: r.account }) : t('run_on_none') }),
    r.context != null ? contextFact(r.context) : null,
    changesFact(r),
    r.remote_control ? h('span', { class: 'pill info', title: t('rc_where', { a: r.account || '' }), text: t('rc_on') }) : null,
    elsewhere ? h('span', { class: 'pill warn', text: '⚠ ' + t('conv_open') }) : null,
    deskPills(r.desktop),
  ].flat().filter(Boolean);
  return h('div', { class: 'row run' + (open ? ' open' : '') },
    h('div', { class: 'grow' },
      h('div', { class: 'title' }, h('span', { title: r.cwd, text: baseName(r.cwd) }), runState(r), claudeState(r)),
      h('div', { class: 'facts' }, facts),
      elsewhere ? h('div', { class: 'hint', text: t('also_open_hint', { w: elsewhere }) }) : null,
      r.claude === 'waiting' && r.waiting_for ? h('div', { class: 'hint', text: t('cl_waiting_hint', { w: r.waiting_for }) }) : null,
      asking ? h('div', { class: 'hint', text: t('asking_hint') + ' ' + t('asking_stop_hint') }) : null,
      waiting ? h('div', { class: 'hint', text: r.account ? t('waiting_hint') : t('waiting_first') }) : null,
      open ? runDetails(st, r) : null),
    h('div', { class: 'actions' },
      r.window ? h('button', { class: 'btn', title: t('show_window_tip'), text: t('show_window'),
        onclick: () => api('sessions/focus', { run_id: r.run_id }).catch(showError) }) : null,
      canSwitch ? sel : null,
      // While the session window asks, baton reads only that window.
      asking || r.state === 'ending' ? null : h('button', { class: 'btn ghost danger', text: t('stop_btn'), onclick: () => confirmStop(r) }),
      toggle));
}

// runDetails is what a session row shows once opened.
function runDetails(st, r) {
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
      h('button', { class: 'btn small-btn', text: t('open_log'), onclick: () => api('open', { what: 'run', run_id: r.run_id }).catch(showError) }),
      // The desktop app opens a conversation from its file: none before the first message.
      st.desktop ? h('button', { class: 'btn small-btn', text: t('dk_btn'), disabled: !r.has_file || !r.session_id,
        title: r.has_file ? null : t('dk_btn_wait'), onclick: () => openDesktop(r.session_id) }) : null));
}

// ---- the Claude desktop app ------------------------------------------------------

// deskPills says where a session shows in the Claude desktop app (b: the
// state's desktop badge), and warns when its file split from the app's.
function deskPills(b) {
  if (!b) return [];
  const names = (b.places || []).map((p) => p || t('dk_app_account'));
  return [
    names.length ? h('span', { class: 'pill info', text: t('dk_pill', { a: names.join(', ') }) }) : null,
    b.split ? h('span', { class: 'pill warn', title: t('dk_split'), text: t('dk_pill_split') }) : null,
  ];
}

// openDesktop picks the accounts whose Claude desktop app lists a session.
async function openDesktop(sessionId) {
  const body = h('div', {}, h('p', { class: 'muted small' }, h('span', { class: 'spin' }), ' ', t('dk_loading')));
  const err = h('div', { class: 'err' });
  const apply = h('button', { class: 'btn primary', text: t('dk_apply'), disabled: true });
  openDialog(h('h2', { text: t('dk_title') }), h('p', { class: 'small dk-intro', text: t('dk_intro') }), body, err,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), apply));
  let v;
  try { v = await api('desktop/places?session_id=' + encodeURIComponent(sessionId)); } catch (e) { body.replaceChildren(h('p', { class: 'err', text: errText(e) })); return; }
  // What the user decides: baton's own ticks. The desktop app's own records stay as they are.
  // Every ticked row is sent, also a locked one (a list the app took over since stays ticked:
  // left out, it would count as unticked); a locked box cannot be changed. A row ticked when
  // the dialog opened that the user left so is sent as kept ('kept:' + key, desktop.Kept):
  // baton leaves it as it is by then, so a record deleted in the app meanwhile is not
  // written again. Only a box the user ticked here is sent as a tick (its key). 'thread'
  // (desktop.Ticked) says the view had the session's thread: the session may go on in
  // another conversation while this is open (/clear), and the ticks are the session's.
  const was = new Set(v.places.filter((p) => p.key && p.checked).map((p) => p.key));
  const picked = new Set(was);
  body.replaceChildren(...[
    v.title ? h('p', { class: 'dk-session', text: v.title }) : null,
    v.split ? h('div', { class: 'tip warn-tip', text: t('dk_split') }) : null,
    v.problem ? h('div', { class: 'tip warn-tip', text: t('dk_problem_' + v.problem) }) : null,
    h('div', { class: 'dk-places' }, v.places.map((p) => desktopRow(v, p, picked))),
    v.app_running ? h('p', { class: 'small dk-running', text: t('dk_running') }) : null,
    h('ul', { class: 'small muted dk-tips' }, [t('dk_tip_copy'), t('dk_tip_hide'), t('dk_tip_limits')].map((x) => h('li', { text: x }))),
  ].filter(Boolean));
  apply.disabled = false;
  apply.addEventListener('click', async () => {
    apply.disabled = true;
    err.textContent = '';
    try {
      const places = [...picked].map((k) => (was.has(k) ? 'kept:' + k : k));
      if (v.thread) places.push('thread');
      const r = await api('desktop/apply', { session_id: sessionId, places });
      closeDialog(); toast(desktopDone(r)); S.lastJSON = ''; await refresh();
    } catch (e) {
      err.textContent = errText(e);
      // A failed apply may have taken unticked rows off already (what it did stays done):
      // such a row is no longer ticked as the dialog opened, so a tick it gets again is
      // the user's own, sent as its key.
      for (const k of [...was]) if (!picked.has(k)) was.delete(k);
    } finally { apply.disabled = false; }
  });
}

// desktopRow is one account's list in the desktop app, ticked where the
// session shows. A row the desktop app owns, or that cannot be chosen, says why.
function desktopRow(v, p, picked) {
  const names = p.accounts || [];
  const fixed = p.locked || p.disabled;
  const label = p.app_only ? (p.email || t(p.former ? 'dk_app_former' : 'dk_app_only')) : names.join(', ');
  const notes = [];
  if (p.app_only) notes.push(t('dk_app_only_note'));
  else if (p.former) notes.push(t('dk_former'));
  if (names.length > 1) notes.push(t('dk_same', { a: names.slice(0, -1).join(', '), b: names[names.length - 1] }));
  if (p.why) notes.push(p.why === 'cloud' ? t('dk_why_cloud', { p: cloudName(p.provider) }) : t('dk_why_' + p.why));
  if (p.copy) notes.push(t('dk_copy', { t: v.title }));
  if (p.archived) notes.push(t('dk_archived'));
  if (p.kept && !p.active) notes.push(t('dk_kept'));
  const state = !p.key ? '' : p.active ? t('dk_active') : p.used ? t('dk_used') : t('dk_unused');
  const box = h('input', { type: 'checkbox', checked: p.locked ? p.checked : picked.has(p.key), disabled: fixed,
    onchange: (e) => {
      if (e.target.checked) picked.add(p.key); else picked.delete(p.key);
      row.classList.toggle('selected', e.target.checked);
    } });
  const row = h('label', { class: 'choice dk' + (box.checked && !fixed ? ' selected' : '') + (fixed ? ' off' : '') }, box,
    h('span', { class: 'grow' },
      h('b', {}, h('span', { text: label }), p.email && !p.app_only ? h('span', { class: 'dk-email', text: p.email }) : null),
      state ? h('span', { class: 'small muted', text: state }) : null,
      notes.map((n) => h('span', { class: 'small dk-note' + (p.why === 'deleted' || p.why === 'released' ? ' warn-text' : ''), text: n }))));
  return row;
}

// desktopDone says what an Apply changed, and when the desktop app shows it:
// the account it is signed in to when it opens (a running app is opened
// again), other accounts when the user switches to them there.
function desktopDone(r) {
  if (!r.added.length && !r.removed.length) return t('dk_none');
  const say = [t('dk_done')];
  if (r.active_added) say.push(t(r.app_running ? 'dk_restart' : 'dk_next_open'));
  if (r.other_added) say.push(t('dk_switch'));
  if (r.active_removed) say.push(t('dk_gone_quit'));
  else if (!r.added.length) say.push(t('dk_gone'));
  return say.join(' ');
}

// cloudName names the cloud provider an account signs in through.
function cloudName(id) {
  return { bedrock: 'Amazon Bedrock', vertex: 'Google Vertex AI', foundry: 'Microsoft Foundry' }[id] || t('dk_cloud_other');
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

  // The conversation a start would continue. One that another Claude Code
  // has open goes on in a copy; one a baton session runs brings its window
  // forward instead (unless the user ticks a copy).
  function target() {
    if (form.mode === 'continue') return convs[0] || null;
    if (form.mode === 'pick' && form.sel) return convs.find((c) => c.session_id === form.sel.session_id && c.where === form.sel.where) || null;
    return null;
  }
  const heldElsewhere = (c) => !!c && c.open && !c.in_baton;
  const copyOf = (c) => !!c && (heldElsewhere(c) || form.copy);
  function drawCopy() {
    const c = target();
    if (!c) { copyBox.replaceChildren(); return; }
    const why = heldElsewhere(c) ? h('div', { class: 'tip', text: t('copy_auto', { app: holderName(c.holder) }) })
      : c && c.in_baton ? h('div', { class: 'tip', text: t('conv_in_baton') }) : '';
    copyBox.replaceChildren(why,
      h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: copyOf(c), disabled: heldElsewhere(c), onchange: (e) => { form.copy = e.target.checked; } }),
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
      const res = await api('sessions/start', { folder: cleanPath(form.folder), account: form.account, mode: form.mode === 'pick' ? 'resume' : form.mode,
        session_id: form.sel ? form.sel.session_id : '', where: form.sel ? form.sel.where : '', trust: form.trust,
        copy: form.mode !== 'new' && copyOf(target()), remote_control: form.rc });
      closeDialog();
      // It runs in baton already: its window came forward and nothing started.
      if (res.focused) toast(t('in_baton_front'));
      // Opened elsewhere since the dialog looked: it went on in a copy anyway.
      else if (res.copied) toast(t('start_done') + ' ' + t('copy_auto', { app: holderName(res.holder) }));
      else toast(t('start_done') + ' ' + t('first_start'));
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
  return madeBy(c.source);
}

// madeBy names what made a conversation (its first rows' entrypoint).
function madeBy(source) {
  return t({ 'claude-desktop': 'src_desktop', cli: 'src_cli', 'claude-vscode': 'src_vscode' }[source] || 'src_other');
}

function convItem(c, withFolder) {
  return h('div', { class: 'grow' },
    h('div', { class: 'title' }, h('span', { text: convTitle(c) }),
      c.in_baton ? h('span', { class: 'pill ok', text: t('conv_running') })
        : c.open ? h('span', { class: 'pill warn', text: t('conv_open') }) : null,
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
  // Deleting the chosen one asks in a dialog of its own; this one comes back
  // as it was afterwards (or on Cancel), its list read anew after a delete.
  const del = h('button', { class: 'btn ghost danger left', text: t('del_btn'), disabled: true, onclick: () => {
    if (!sel) return;
    const d = document.getElementById('dialog');
    const self = d.firstElementChild;
    openDelete(sel, {
      show: () => { d.replaceChildren(self); if (!d.open) d.showModal(); search.focus(); },
      done: async () => { await load(); draw(); },
    });
  } });
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
    del.disabled = !sel;
    if (sel && sel.folder_ok === false) {
      hint.replaceChildren(h('div', { class: 'tip', text: sel.folder_problem === 'gone' ? t('conv_gone_hint') : t('conv_bad_hint') }));
    } else if (sel && sel.open) {
      hint.replaceChildren(heldNote(sel, recheck, note));
    } else {
      hint.replaceChildren(...(note ? [h('div', { class: 'tip ok-tip', text: note })] : []));
    }
    more.replaceChildren(...(capped ? [h('p', { class: 'muted small mt6', text: t('import_more') })] : []));
  }
  draw();
  openDialog(h('h2', { text: t('import_title') }), h('p', { class: 'muted small', text: t('import_sub') }),
    h('div', { class: 'field' }, search), list, more, hint,
    h('div', { class: 'foot' }, del, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), go));
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

// holderName names what has a conversation open (its entrypoint).
function holderName(holder) {
  return t({ 'claude-desktop': 'held_desktop', cli: 'held_cli', 'claude-vscode': 'held_vscode' }[holder] || 'held_other');
}

// heldNote: the conversation is open in another app, so it goes on in a copy
// by itself. Check again is there for one who quit that app meanwhile: then
// the same conversation goes on.
function heldNote(c, recheck, note) {
  const app = holderName(c.holder);
  // Closing the desktop app's window does not let go of the conversation:
  // the app must be quit, from its tray icon too.
  const how = c.holder === 'claude-desktop' ? t('held_recheck_hint_desktop') : t('held_recheck_hint', { app });
  const btn = h('button', { class: 'btn small-btn', text: t('held_recheck'), title: how, onclick: async () => {
    btn.disabled = true;
    try { await recheck(); } finally { btn.disabled = false; }
  } });
  return h('div', { class: 'tip' },
    h('div', { text: t('copy_auto', { app }) }),
    h('div', { class: 'small muted mt6' }, h('span', { text: note || how }), ' ', btn));
}

// ---- deleting a conversation -----------------------------------------------------------

// openDelete asks before conversation c (a row: session_id, where, its title)
// goes to the Recycle Bin. It shows what goes and what stays, or what keeps
// it from going now, with Check again for after the user ended or closed
// that. back, from the Bring-in dialog: show() brings that dialog back (on
// Cancel and after a delete), done() reads its list anew.
async function openDelete(c, back) {
  const where = c.where === 'claude-code' ? t('conv_where_home') : t('conv_where', { a: c.where });
  const body = h('div', {}, h('p', { class: 'muted small' }, h('span', { class: 'spin' }), ' ', t('del_loading')));
  const err = h('div', { class: 'err' });
  // A delete that stopped may have taken it off the desktop app's lists already. Said here, apart
  // from err and the plan: the look after it clears err and finds nothing left there to take off.
  const offNote = h('div', {});
  const offDone = new Set();
  const go = h('button', { class: 'btn primary danger', text: t('del_go'), disabled: true });
  const d = openDialog(h('h2', { text: t('del_title') }), body, offNote, err,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: () => leave() }), go));
  const dlg = d.firstElementChild;
  // The user may have closed this dialog (Esc) or gone back while waiting.
  const ours = () => d.open && d.firstElementChild === dlg;
  const leave = () => { if (!ours()) return; if (back) back.show(); else closeDialog(); };
  const url = 'sessions/delete-plan?session_id=' + encodeURIComponent(c.session_id) + '&where=' + encodeURIComponent(c.where);
  let blocked = ''; // the first blocker's code at the last look
  async function look(again) {
    let p;
    try { p = await api(url); } catch (e) {
      if (!ours()) return;
      go.disabled = true; err.textContent = '';
      body.replaceChildren(h('p', { class: 'err', text: delErr(e) }));
      return;
    }
    if (!ours()) return;
    const first = p.blockers[0];
    // Check again says how it went; a blocker shown says it all.
    const note = !again ? '' : first ? (first.code === blocked ? t('del_still') : '') : blocked ? t('del_free') : '';
    if (again || first) err.textContent = '';
    blocked = first ? first.code : '';
    go.disabled = !p.ok;
    body.replaceChildren(...planView(c, p, where,
      first ? blockerNote(first, note, () => look(true)) : note ? h('div', { class: 'tip ok-tip', text: note }) : null));
  }
  go.addEventListener('click', async () => {
    go.disabled = true; err.textContent = '';
    let r;
    try { r = await api('sessions/delete', { session_id: c.session_id, where: c.where }); } catch (e) {
      err.textContent = delErr(e);
      for (const a of (e.data && e.data.desktop_off) || []) offDone.add(a || t('del_app_account'));
      const off = offDone.size ? t('del_desktop_off_done', { a: [...offDone].join(', '), b: t('dk_btn') }) : '';
      offNote.replaceChildren(...(off ? [h('div', { class: 'tip warn-tip', text: off })] : []));
      S.lastJSON = ''; refresh(); // part of it may have gone
      if (!ours()) { toast([delErr(e), off].filter(Boolean).join(' '), true); return; } // closed meanwhile
      await look(false); // what is left, or what refused it
      return;
    }
    toast(t('del_done') + (r.warning === 'reopened' ? ' ' + t('del_reopened') : ''));
    S.lastJSON = ''; refresh();
    if (!back) { if (ours()) closeDialog(); return; }
    if (ours()) back.show();
    await back.done();
  });
  await look(false);
}

// planView is a delete's plan p for row c: what it is and where it is kept,
// status (what keeps it from going, or how Check again went), what goes with
// it and what stays.
function planView(c, p, where, status) {
  // A delete that stopped half way leaves files with no conversation to name them by: the
  // server says so (leftover). A title can't tell: a conversation behind a link or junction,
  // which baton does not read through, has none, nor has one whose file is empty or whose rows
  // can't be read. Those are still there, so the row names them. With no title, folder or
  // source baton read nothing of what made it, so it says nothing of that either (made_in
  // stays "outside" when no name of its file was seen).
  const nameless = !p.title && !p.cwd && !p.source;
  const leftover = !!p.leftover;
  const folder = p.cwd || c.cwd || '';
  const off = (p.desktop_off || []).map((a) => a || t('del_app_account'));
  const notes = p.notes || [];
  const copies = notes.find((n) => n.code === 'copies_use_outputs');
  return [
    h('p', { class: 'dk-session', text: p.title || (leftover ? t('del_leftover') : convTitle(c)) }),
    h('p', { class: 'small muted', title: folder, text: [baseName(folder), where].filter(Boolean).join(' · ') }),
    status,
    h('p', { class: 'small', text: t('del_body') }),
    p.made_in === 'outside' && !nameless ? h('p', { class: 'small', text: t('del_outside', { src: madeBy(p.source) }) }) : null,
    off.length ? h('p', { class: 'small', text: t('del_desktop_off', { a: off.join(', ') }) }) : null,
    copies ? h('div', { class: 'tip', text: copies.n === 1 ? t('del_copies_one') : t('del_copies', { n: copies.n }) }) : null,
    notes.some((n) => n.code === 'not_fully_checked') ? h('p', { class: 'small muted', text: t('del_copies_more') }) : null,
    h('p', { class: 'small muted', text: t('del_kept') }),
  ].filter(Boolean);
}

// blockerNote says what keeps a conversation from being deleted now, with
// Check again (as heldNote): the user may end or close it meanwhile. One a
// baton session runs can bring that session's window forward.
function blockerNote(b, note, recheck) {
  const btn = h('button', { class: 'btn small-btn', text: t('recheck'), onclick: async () => {
    btn.disabled = true;
    try { await recheck(); } finally { btn.disabled = false; }
  } });
  // A session_window run has ended, so it is not among S.state.runs (sessions still going), but
  // its window may still be open: the server brings it forward, or says it is gone (no_window).
  const runId = b.run_id && (b.code === 'session_window' ||
    (b.code === 'in_baton' && S.state.runs.some((r) => r.run_id === b.run_id && r.window))) ? b.run_id : '';
  return h('div', { class: 'tip warn-tip' },
    h('div', { text: delText(b) }),
    h('div', { class: 'small muted mt6' }, note ? h('span', { text: note + ' ' }) : null, btn,
      runId ? ' ' : null, runId ? h('button', { class: 'btn small-btn', text: t('show_window'), title: t('show_window_tip'),
        onclick: () => api('sessions/focus', { run_id: runId }).catch(showError) }) : null));
}

// delText says why a conversation can't be deleted now; b is a blocker of
// the plan, or of a refused delete (or only its code).
function delText(b) {
  // err_in_baton is a start's: Start brings its window forward.
  if (b.code === 'in_baton') return t('del_in_baton');
  if (b.code === 'desktop_app' && b.detail === 'prior') return t('err_desktop_app_prior');
  // Closing the desktop app's window does not let go of it.
  if (b.code === 'open_elsewhere' && b.holder === 'claude-desktop') return t('err_open_elsewhere_desktop');
  const s = t('err_' + b.code, { app: holderName(b.holder), d: b.drive || '' });
  return b.drive ? s : s.replace(/\s*\(\)/, ''); // no drive known: no brackets
}

// delErr explains a failed plan or delete.
function delErr(e) {
  const b = (e.data && e.data.blocker) || { code: e.code };
  return b.code === 'in_baton' || T.en['err_' + b.code] ? delText(b) : errText(e);
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
      h('button', { class: 'btn primary', text: t('add_account'), onclick: () => openAdd() }))), loginsCard(st)];
  }
  const rows = st.accounts.map((a, i) => accountRow(st, a, i));
  return [...signinCards(st), h('div', { class: 'card' }, head, h('div', {}, rows), st.accounts.length > 1 ? h('p', { class: 'muted small', text: t('order_hint') }) : null),
    loginsCard(st)];
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
      h('div', { class: 'title' }, a.id, accountStatus(a), a.in_use ? h('span', { class: 'pill info', text: t('in_use'), title: t('in_use_hint') }) : null,
        // Only a pill: switching the desktop app is on its own card, never beside a limit.
        a.desktop_login === 'active' ? h('span', { class: 'pill info', text: t('dl_acct_pill') }) : null),
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
  // The desktop app's sign-in is its own: removing the account here leaves the kept one.
  const a = S.state.accounts.find((x) => x.id === id);
  openDialog(h('h2', { text: t('remove_title', { a: id }) }), h('p', { text: t('remove_body') }),
    a && a.desktop_kept ? h('p', { class: 'small muted', text: t('dl_remove_note') }) : null, err,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), go));
}

// ---- the desktop app's sign-in ----------------------------------------------------------
// Switching the Claude desktop app between the user's own accounts: unofficial, and off
// unless turned on in Settings after its notice. The state poll carries a summary
// (desktop_logins); the card reads every kept sign-in (desktop/logins) when that changes.
// Nothing here switches on its own: only the buttons do.

// loginErr says why a change of the desktop app's sign-in was refused; for a few refusals
// the server's detail says more (a key or an item's name, never a value).
function loginErr(e) {
  const d = e.details || '';
  if (e.code === 'dl_unsaved_current' && d === 'no_account') return t('err_dl_unsaved_no_account');
  // Trying again does not help here, so it says why.
  if (e.code === 'dl_unsaved_current' && d === 'store_permissions') return t('err_dl_unsaved_store_permissions');
  // Another account's kept sign-in cannot be read: named, so it can be forgotten when trying again does not help.
  if (e.code === 'dl_unsaved_current' && d.startsWith('kept_unreadable:')) return t('err_dl_unsaved_kept_unreadable', { a: d.slice('kept_unreadable:'.length) });
  if (e.code === 'dl_damaged' && d === 'tokens_of_another') return t('err_dl_tokens_of_another');
  if (e.code === 'dl_bad_label') return t(T.en['err_dl_bad_label_' + d] ? 'err_dl_bad_label_' + d : 'err_dl_bad_label');
  if (e.code === 'dl_ambiguous') return t('err_dl_ambiguous', { a: d });
  if (e.code === 'dl_unknown_format') return t('err_dl_unknown_format') + (d ? ' (' + d + ')' : '');
  // What in the desktop app is working, session by session (workingList shows it as a list).
  if (e.code === 'dl_app_working') return [t('err_dl_app_working'), ...workingLines(e)].join(' ');
  if (e.code === 'dl_quit_failed') return t(d === 'refused' ? 'err_dl_quit_failed_refused' : 'err_dl_quit_failed');
  // Which layout of the app's folders it is, when baton can tell.
  if (e.code === 'dl_unsupported_layout' && T.en['err_dl_unsupported_layout_' + d]) return t('err_dl_unsupported_layout_' + d);
  return errText(e);
}

async function loadLogins(key) {
  S.dl.loading = true;
  try { S.dl.view = await api('desktop/logins'); S.dl.err = null; } catch (e) { S.dl.err = e; }
  S.dl.key = key; S.dl.loading = false;
  if (S.tab === 'accounts') render();
}

// loginsAct asks for a change of the desktop app's sign-in, or of what baton keeps of it;
// the card says one is under way (a switch copies the whole sign-in), and reads them again.
async function loginsAct(path, body) {
  S.dl.busy = true; render();
  try { return await api('desktop/logins/' + path, body); }
  finally { S.dl.busy = false; S.dl.key = ''; S.lastJSON = ''; refresh(); }
}

// loginsCard is the Accounts tab's card: the accounts the desktop app was signed in to,
// the one it is on first, each switched to with one click. While the app runs, a switch asks
// first: baton then quits it (when nothing in it is working), switches, and opens it again.
function loginsCard(st) {
  const dl = st.desktop_logins;
  if (!dl || !dl.on) return null;
  const key = JSON.stringify([dl, st.accounts.map((a) => [a.id, a.desktop_login || '', !!a.desktop_kept])]);
  if (S.dl.key !== key && !S.dl.loading) loadLogins(key);
  const v = S.dl.view, busy = S.dl.busy;
  const unnamed = (v && v.unnamed) || [];
  const left = dl.kept + (dl.unnamed || 0);
  let body;
  if (!v) body = S.dl.err ? h('p', { class: 'err', text: loginErr(S.dl.err) }) : h('p', { class: 'muted small' }, h('span', { class: 'spin' }), ' ', t('dl_loading'));
  else if (!v.logins.length && !unnamed.length) body = h('p', { class: 'muted', text: t('dl_none') });
  else body = h('div', {}, v.logins.map((g) => loginRow(dl, g, busy)), unnamed.map((u) => unnamedRow(u, busy)));
  const open = h('input', { type: 'checkbox', checked: S.ui.dlOpen !== false, onchange: (e) => { S.ui.dlOpen = e.target.checked; } });
  return h('div', { class: 'card dl-card' },
    h('div', { class: 'card-head' }, h('div', {},
      h('h3', { class: 'dl-head' }, h('span', { text: t('dl_title') }),
        h('span', { class: 'pill ' + (dl.running ? 'warn' : 'ok'), text: t(dl.running ? 'dl_pill_running' : 'dl_pill_quit'), title: dl.running ? t('dl_running') : '' })),
      h('p', { class: 'muted small', text: t('dl_sub') }))),
    dl.problem ? h('div', { class: 'tip warn-tip', text: loginErr({ code: dl.problem, details: dl.problem_detail }) }) : null,
    busy ? h('p', { class: 'small' }, h('span', { class: 'spin' }), ' ', t('dl_switching')) : null,
    copiesTip(dl),
    body,
    v && !v.active && !dl.problem ? h('p', { class: 'small', text: t('dl_signed_out') }) : null,
    h('div', { class: 'dl-foot' },
      h('div', { class: 'actions start' },
        h('button', { class: 'btn', text: t('dl_new'), disabled: busy || !v || !!dl.problem, title: dl.running ? t('dl_running') : '',
          onclick: () => openLoginsAdd(v, dl.running) }),
        h('button', { class: 'btn ghost danger', text: t('dl_forget_all'), disabled: busy || !left, onclick: () => openForgetAll(dl) })),
      h('label', { class: 'check' }, open, h('span', { text: t('dl_open_after') })),
      h('div', { class: 'tip warn-tip small', text: t('dl_no_logout') })));
}

// loginRow is one account the desktop app was signed in to. Switching to it needs a
// whole kept sign-in; the button says why not otherwise. While the app runs, it asks first.
function loginRow(dl, g, busy) {
  const why = g.active ? t('dl_use_now') : !g.kept ? t('dl_use_not_kept')
    : g.damaged ? t('err_dl_damaged') : dl.problem ? loginErr({ code: dl.problem, details: dl.problem_detail }) : '';
  const baton = g.baton || [];
  const who = [g.email, g.org_name].filter(Boolean).join(' · ');
  return h('div', { class: 'row dl' },
    h('div', { class: 'grow' },
      h('div', { class: 'title' }, h('span', { text: g.label }),
        g.active ? h('span', { class: 'pill ok', text: t('dl_now') }) : null,
        baton.length ? h('span', { class: 'pill info', text: t('dl_baton', { a: baton.join(', ') }) }) : h('span', { class: 'pill off', text: t('dl_not_baton') }),
        !g.kept ? h('span', { class: 'pill warn', text: t('dl_not_kept_pill'), title: t('dl_not_kept') })
          : g.damaged ? h('span', { class: 'pill bad', text: t('dl_damaged_pill'), title: t('err_dl_damaged') })
            : h('span', { class: 'pill off', text: t('dl_kept', { t: g.kept_at ? ago(g.kept_at) : '' }) })),
      who ? h('div', { class: 'sub', text: who }) : null,
      !g.kept ? h('div', { class: 'sub', text: t('dl_not_kept') }) : null),
    h('div', { class: 'actions' },
      h('button', { class: 'btn primary', text: t('dl_use'), disabled: busy || !!why, title: why || (dl.running ? t('dl_running') : ''),
        onclick: () => (dl.running ? openQuitSwitch(g) : switchLogin(g)) }),
      g.kept ? h('button', { class: 'btn ghost', text: t('dl_rename'), disabled: busy, onclick: () => openLoginRename(g) }) : null,
      g.kept ? h('button', { class: 'btn ghost danger', text: t('dl_forget'), disabled: busy, onclick: () => openLoginForget(g) }) : null));
}

// unnamedRow is what a signed-out app had left, set aside by a switch: no token recorded, never
// switched to, kept only until the user deletes it.
function unnamedRow(u, busy) {
  return h('div', { class: 'row dl' },
    h('div', { class: 'grow' }, h('div', { class: 'title' }, h('span', { text: t('dl_unnamed') })),
      h('div', { class: 'sub', text: u.kept_at ? t('dl_unnamed_kept', { t: ago(u.kept_at) }) : u.name })),
    h('div', { class: 'actions' },
      h('button', { class: 'btn ghost danger', text: t('dl_forget'), disabled: busy, onclick: () => confirmLogins(t('dl_unnamed_title'), [t('dl_unnamed_body')],
        t('dl_unnamed_go'), async () => { await loginsAct('drop', { account: u.name }); return t('dl_unnamed_gone'); }) })));
}

// switchSaid is what a switch did: done first, then whatever else it did on the way.
function switchSaid(r, done, open) {
  return [r.recovered ? t('dl_recovered_first') : '', done, open && !r.opened ? t('dl_open_yourself') : '',
    r.stashed ? t('dl_stashed') : ''].filter(Boolean).join(' ');
}

async function switchLogin(g) {
  const open = S.ui.dlOpen !== false;
  try {
    const r = await loginsAct('use', { account: g.account, open });
    toast(switchSaid(r, t(r.opened ? 'dl_done_open' : 'dl_done', { a: g.label }), open));
  } catch (e) { toast(loginErr(e), true); }
}

// openQuitSwitch switches to g while the desktop app runs: it asks first, since baton quits the
// app (only when nothing in it is working), switches it, and opens it again. What is working is
// listed in the dialog, session by session.
function openQuitSwitch(g) {
  const err = h('div', { class: 'err' });
  const go = h('button', { class: 'btn primary', text: t('dl_quit_go'), onclick: async () => {
    go.disabled = true; err.textContent = '';
    try {
      const r = await loginsAct('use', { account: g.account, open: true, quit: true });
      closeDialog();
      toast(switchSaid(r, t(r.quit && r.opened ? 'dl_done_quit' : r.opened ? 'dl_done_open' : 'dl_done', { a: g.label }), true));
    } catch (e) { showLoginErr(err, e); go.disabled = false; }
  } });
  openDialog(h('h2', { text: t('dl_quit_title', { a: g.label }) }), h('p', { text: t('dl_quit_body', { a: g.label }) }),
    h('p', { class: 'small muted', text: t('dl_quit_check') }), err,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), go));
}

// workingLines are the sessions a refusal to quit the app names (e.data.working), one line each.
function workingLines(e) {
  const ws = (e.data && e.data.working) || [];
  return ws.map((w) => (w.title || t('dl_untitled')) + ': ' + t(T.en['dl_work_' + w.what] ? 'dl_work_' + w.what : 'dl_work_unknown'));
}

// showLoginErr shows why a switch was refused in box: the sessions at work as a list, and, when
// baton had quit the app for it, that the app is open again on the account it was on.
function showLoginErr(box, e) {
  const lines = e.code === 'dl_app_working' ? workingLines(e) : [];
  const quit = e.data && e.data.quit;
  box.replaceChildren(h('div', { text: e.code === 'dl_app_working' ? t('err_dl_app_working') : loginErr(e) }),
    lines.length ? h('ul', { class: 'small' }, lines.map((x) => h('li', { text: x }))) : null,
    // Not opened because a switch is cut off: the error says what to do, and the app must not open.
    quit && e.data.open_error !== 'dl_switch_unrestored' ? h('div', { text: t(e.data.opened ? 'dl_reopened' : 'dl_reopen_yourself') }) : null);
}

// openLoginsAdd keeps the sign-in the app has, then empties it on this PC, so the user can
// sign in to another account there: what the app's Log out would do, without ending the
// kept one on Claude's servers.
function openLoginsAdd(v, running) {
  const cur = v.logins.find((g) => g.active);
  const err = h('div', { class: 'err' });
  const go = h('button', { class: 'btn primary', text: t('dl_new_go'), onclick: async () => {
    go.disabled = true; err.textContent = '';
    // An app baton quits opens again whatever the box says.
    const open = S.ui.dlOpen !== false || running;
    try {
      const r = await loginsAct('add', { open, quit: !!running });
      closeDialog();
      toast(switchSaid(r, t(r.quit && r.opened ? 'dl_done_blank_quit' : r.opened ? 'dl_done_blank_open' : 'dl_done_blank'), open));
    } catch (e) {
      // Signed out already, with nothing left to set aside: the user signs in in the app.
      if (e.code === 'dl_already') err.textContent = t('dl_signed_out'); else showLoginErr(err, e);
      go.disabled = false;
    }
  } });
  openDialog(h('h2', { text: t('dl_new_title') }), h('p', { text: cur ? t('dl_new_body', { a: cur.label }) : t('dl_new_blank') }),
    running ? h('p', { text: t('dl_new_quit') }) : null,
    h('div', { class: 'tip warn-tip', text: t('dl_new_no_logout') }), err,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), go));
}

// confirmLogins asks before something baton keeps is deleted (for good: never the Recycle
// Bin), then has act do it; act returns what to say, and errOf words a refusal.
function confirmLogins(title, lines, goText, act, errOf = loginErr) {
  const err = h('div', { class: 'err' });
  const go = h('button', { class: 'btn primary danger', text: goText, onclick: async () => {
    go.disabled = true; err.textContent = '';
    try { const said = await act(); closeDialog(); toast(said); } catch (e) { err.textContent = errOf(e); go.disabled = false; }
  } });
  openDialog(h('h2', { text: title }), ...lines.filter(Boolean).map((x) => h('p', { text: x })), err,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), go));
}

function openLoginForget(g) {
  confirmLogins(t('dl_forget_title', { a: g.label }), [t('dl_forget_body', { a: g.label }), t('dl_forget_servers')], t('dl_forget_go'),
    async () => { await loginsAct('drop', { account: g.account }); return t('dl_forgot', { a: g.label }); });
}

// openForgetAll deletes every kept sign-in: on the card, and in Settings for what is left
// from before while switching is off. dl is the state's summary.
function openForgetAll(dl) {
  confirmLogins(t('dl_forget_all_title'), [dlKept(dl, 'dl_forget_all_body', 'dl_unnamed_only', 'dl_unnamed_too'),
    dl.kept ? t('dl_not_revoked') : '', dl.active ? t('dl_forget_all_stays', { a: dl.active }) : dl.problem ? '' : t('dl_off_blank')],
  t('dl_forget_all_go'), async () => {
    // deleted counts the sign-ins only, as the dialog did: what signed-out apps left went too.
    const r = await loginsAct('drop-all', {});
    return r.deleted ? t('dl_forgot_all', { n: r.deleted }) : t('dl_unnamed_gone');
  });
}

// dlKept words what baton keeps of the desktop app's sign-ins, from the state's summary dl:
// the sign-ins by text one, and apart from them what signed-out apps left, which holds no
// sign-in (by text only when there is nothing else, else too), so neither counts as the other.
function dlKept(dl, one, only, too) {
  const n = dl.kept || 0, m = dl.unnamed || 0;
  if (!n) return m ? t(only, { n: m }) : '';
  return t(one, { n }) + (m ? ' ' + t(too, { n: m }) : '');
}

function openLoginRename(g) {
  const err = h('div', { class: 'err' });
  const save = async () => {
    go.disabled = true; err.textContent = '';
    try { await loginsAct('rename', { account: g.account, label: input.value.trim() }); closeDialog(); toast(t('dl_renamed')); }
    catch (e) { err.textContent = loginErr(e); go.disabled = false; }
  };
  const input = h('input', { type: 'text', value: g.named || '', maxlength: '40', autocomplete: 'off', spellcheck: 'false',
    placeholder: baseLabel(g), onkeydown: (e) => { if (e.key === 'Enter') save(); } });
  const go = h('button', { class: 'btn primary', text: t('dl_rename_go'), onclick: save });
  openDialog(h('h2', { text: t('dl_rename_title', { a: g.label }) }),
    h('label', { class: 'field' }, h('span', { text: t('dl_rename_label') }), input, h('div', { class: 'small muted', text: t('dl_rename_hint') })),
    err, h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), go));
  input.focus();
}

// baseLabel is what a kept sign-in is called without a name of its own (as the server names it).
function baseLabel(g) { return (g.baton && g.baton[0]) || g.email || g.account.slice(0, 8); }

// recoverLogins puts back a switch that was cut off; the banner offers it while the app is quit.
// A refusal the banner can offer more for is kept for it (see leavable).
async function recoverLogins() {
  try {
    const r = await loginsAct('recover', {});
    S.dl.recoverErr = null;
    await refresh(); // the account the app is back on
    const a = S.state.desktop_logins && S.state.desktop_logins.active;
    toast(!r.undone ? t('dl_recovered_none') : a ? t('dl_recovered', { a }) : t('dl_recovered_blank'));
  } catch (e) {
    S.dl.recoverErr = leavable(e) ? e : null;
    render();
    toast(loginErr(e), true);
  }
}

// leavable: Put it back was refused in a way that waiting cannot mend, so the app may be left
// as it is instead: the app was used on the switched sign-in, which cannot be kept now; or the
// switch cannot be undone; or the sign-in it has now cannot be told apart.
function leavable(e) {
  const d = e.details || '';
  return e.code === 'dl_used_since' || e.code === 'dl_switch_unrestored' ||
    (e.code === 'dl_unsaved_current' && (d === 'no_account' || d.startsWith('kept_unreadable:'))) || (e.code === 'dl_damaged' && d === 'tokens_of_another');
}

// openSettle leaves the desktop app as it is, for a switch that was cut off and is not put
// back, after asking: it stays on the sign-in it has, and what the switch set aside goes.
function openSettle() {
  confirmLogins(t('dl_leave_title'), [t('dl_leave_body')], t('dl_leave'), async () => {
    const r = await loginsAct('settle', {});
    S.dl.recoverErr = null;
    await refresh(); // the account the app stays on
    const a = S.state.desktop_logins && S.state.desktop_logins.active;
    return !r.settled ? t('dl_left_none') : a ? t('dl_left', { a }) : t('dl_left_blank');
  }, settleErr);
}

// settleErr words a refused Leave it as it is. dl_switch_unrestored is not the switch's own
// failure here, whose text sends the user to Put it back: Settle fails so while another
// program holds one of the app's files (it changes nothing then) or a move cannot finish
// yet, which passes; `baton desktop login recover --leave-as-is` says the same.
function settleErr(e) {
  return e.code === 'dl_switch_unrestored' ? t('err_dl_settle_unrestored') : loginErr(e);
}

// copiesTip offers to delete the copies switches that stopped early left in the app's folder
// (only copies: the app never reads them, so it may run meanwhile).
function copiesTip(dl) {
  if (!dl.leftover) return null;
  const go = h('button', { class: 'btn small-btn', text: t('dl_copies_go'), disabled: S.dl.busy, onclick: async () => {
    go.disabled = true;
    try { await loginsAct('tidy', {}); toast(t('dl_copies_gone')); } catch (e) { toast(loginErr(e), true); go.disabled = false; }
  } });
  return h('div', { class: 'tip' }, h('span', { text: t('dl_copies', { n: dl.leftover }) + ' ' }), go);
}

// loginsSetting turns switching on (after its notice) or off (after saying what goes). It is
// greyed out, with why, where it cannot work here; what is left from before can be deleted
// while it is off.
function loginsSetting(st) {
  const dl = st.desktop_logins, on = st.config.desktop_logins;
  if (!dl) return [];
  const left = dl.kept + (dl.unnamed || 0);
  if (!st.desktop && !on && !left) return [];
  const blocked = !on && !!dl.problem;
  const box = h('input', { type: 'checkbox', checked: on, disabled: blocked, onchange: (e) => {
    e.target.checked = on; // as it is set, until the dialog has changed it
    if (on) openLoginsOff(st); else turnLoginsOn(st);
  } });
  return [
    h('label', { class: 'check mt14' + (blocked ? ' off' : '') }, box, h('span', { text: t('dl_set') })),
    h('p', { class: 'small muted', text: on ? t('dl_set_hint') + ' ' + t('dl_set_where') : t('dl_set_hint') }),
    blocked ? h('p', { class: 'small warn-text', text: loginErr({ code: dl.problem, details: dl.problem_detail }) }) : null,
    !on && left ? h('div', { class: 'tip' }, h('span', { text: dlKept(dl, 'dl_leftover', 'dl_leftover_unnamed', 'dl_leftover_unnamed_too') + ' ' }),
      h('button', { class: 'btn small-btn danger', text: t('dl_leftover_go'), onclick: () => openForgetAll(dl) })) : null,
    !on ? copiesTip(dl) : null,
  ];
}

async function turnLoginsOn(st) {
  // The notice is shown again whenever it was not accepted as it reads now (turning
  // switching off forgets the acceptance).
  if (!st.config.desktop_logins_ack) { openLoginsNotice(); return; }
  if (await setConfig('desktop_logins', true, false, true)) toast(t('dl_on_done'));
}

// openLoginsNotice is what the user reads and accepts before switching is turned on. The
// box is never ticked for them, and the server checks it too.
function openLoginsNotice() {
  const dl = S.state.desktop_logins || {};
  const err = h('div', { class: 'err' });
  const ack = h('input', { type: 'checkbox', onchange: (e) => { go.disabled = !e.target.checked; } });
  const go = h('button', { class: 'btn primary', text: t('dl_on'), disabled: true, onclick: async () => {
    go.disabled = true; err.textContent = '';
    try {
      await api('config', { key: 'desktop_logins', value: 'true', acknowledge: true });
      closeDialog(); toast(t('dl_on_done')); S.dl.key = ''; S.lastJSON = ''; refresh();
    } catch (e) { err.textContent = loginErr(e); go.disabled = !ack.checked; }
  } });
  openDialog(h('h2', { text: t('dl_notice_title') }),
    h('div', { class: 'notice dl-notice' }, h('ul', {}, t('dl_notice_points').map((p) => h('li', { text: p.replaceAll('{path}', () => dl.store || '') })))),
    h('label', { class: 'check' }, ack, h('span', { text: t('dl_notice_ack') })), err,
    h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }), go));
}

// openLoginsOff says what turning switching off deletes, and where the app stays.
function openLoginsOff(st) {
  const dl = st.desktop_logins || {};
  const what = dlKept(dl, 'dl_off_body', 'dl_unnamed_only', 'dl_unnamed_too');
  confirmLogins(t('dl_off_title'), [what || t('dl_off_none'), dl.kept ? t('dl_not_revoked') : '',
    dl.active ? t('dl_off_stays', { a: dl.active }) : dl.problem ? '' : t('dl_off_blank')],
  t(what ? 'dl_off_go' : 'dl_off_go_none'), async () => {
    await api('config', { key: 'desktop_logins', value: 'false' });
    S.dl.view = null; S.dl.key = ''; S.lastJSON = ''; refresh();
    return t('dl_off_done');
  });
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
      const s = S.state, c = s.claude;
      let msg = t('ob_claude_wait');
      // Not found, or every copy too old: the same words as the banner.
      if (!s.claude_pending) msg = !c.found ? claudeProblemText(s, true) : c.source === 'desktop'
        ? t('ob_claude_desktop', { v: c.version }) : t('ob_claude_cli', { v: c.version });
      return [h('h2', { text: t('ob_claude_title') }), h('p', { text: msg }), !s.claude_pending && !c.found ? claudeErrorDetails(c) : null];
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
    // Its own notice: the account switch's would not do.
    if (e.code === 'needs_desktop_ack') { openLoginsNotice(); return false; }
    // Turning the desktop app's sign-in switching on is refused with its own codes, some
    // of which only loginErr words (a store that cannot get permissions of its own).
    if (key === 'desktop_logins') { toast(loginErr(e), true); return false; }
    showError(e);
    return false;
  }
}

// claudePicker lists every Claude Code on this PC to choose from: finding the
// best one automatically first, then the desktop app's (whichever version it
// has), each copy found, and a path typed in.
function claudePicker(st) {
  const c = st.claude, ins = c.installs || [];
  const choice = st.config.claude_path || '';
  // The server says which row claude_path is (a Scoop shim is listed as the
  // claude.exe it leads to; a desktop version the app removed follows the
  // app); until it looked again after a change, claude_path itself.
  const key = c.choice === choice && typeof c.selected === 'string' ? c.selected : choice;
  const typed = key && key !== 'desktop' && !ins.some((i) => samePath(i.path, key));
  const sel = S.ui.claudeOther || typed ? 'other' : key;
  const pick = (value) => { S.ui.claudeOther = false; setConfig('claude_path', value); };
  const row = (o) => h('div', { class: 'choice' + (o.selected ? ' selected' : '') + (o.off ? ' off' : '') },
    h('label', { class: 'grow' },
      h('input', { type: 'radio', name: 'claudepick', checked: o.selected, disabled: o.off, onchange: o.pick }),
      h('span', {}, h('b', {}, o.title, o.tag ? h('span', { class: 'pill ok', text: o.tag }) : null),
        o.sub ? h('span', { class: 'small muted', text: o.sub }) : null, ...(o.more || []))),
    o.button || null);

  const auto = ins.find((i) => samePath(i.path, c.auto));
  const desk = ins.find((i) => i.source === 'desktop' && !i.problem);
  const out = [
    row({ selected: sel === '', title: t('cc_auto'), pick: () => pick('auto'),
      sub: auto ? t('cc_auto_now', { v: auto.version, src: claudeSource(auto.source) }) : t('cc_auto_none') }),
    sel === '' ? h('label', { class: 'check choice-sub' },
      h('input', { type: 'checkbox', checked: st.config.allow_desktop_bundle, onchange: (e) => setConfig('allow_desktop_bundle', e.target.checked) }),
      h('span', { text: t('set_claude_bundle') })) : null,
    row({ selected: sel === 'desktop', title: t('cc_desktop'), pick: () => pick('desktop'),
      sub: desk ? t('cc_desktop_now', { v: desk.version }) : t('cc_desktop_none') }),
  ];
  // The copies that work first, newest first; then the others, with why.
  const listed = ins.map((i, n) => ({ i, n })).sort((a, b) => (!!a.i.problem - !!b.i.problem) ||
    (b.i.version || '').localeCompare(a.i.version || '', undefined, { numeric: true }) || a.n - b.n).map((x) => x.i);
  for (const i of listed) {
    const selected = sel !== 'other' && samePath(i.path, key);
    const title = i.version ? 'Claude Code ' + i.version : i.problem === 'wrapper' ? i.path.split('\\').pop() : 'Claude Code';
    out.push(row({ selected, off: !!i.problem && !selected, pick: () => pick(i.path),
      title, tag: i.in_use ? t('cc_in_use') : '', sub: claudeSource(i.source),
      more: [h('div', { class: 'small mono muted', text: i.path }), i.problem ? h('div', { class: 'small warn-text', text: claudeWhy(i, c.min) }) : null],
      button: i.problem === 'too_old' && i.source !== 'desktop' ? claudeUpdateButton(st, i, t('cc_update')) : null }));
  }
  const input = h('input', { type: 'text', class: 'mono', placeholder: t('cc_other_hint'), value: S.ui.claudeOtherText ?? (typed ? choice : ''),
    oninput: (e) => { S.ui.claudeOtherText = e.target.value; } });
  const apply = async () => {
    const p = cleanPath(input.value);
    if (p && await setConfig('claude_path', p)) { S.ui.claudeOther = false; S.ui.claudeOtherText = undefined; render(); }
  };
  out.push(row({ selected: sel === 'other', title: t('cc_other'), pick: () => { S.ui.claudeOther = true; render(); } }),
    sel === 'other' ? h('div', { class: 'inline choice-sub' }, input, h('button', { class: 'btn', text: t('apply'), onclick: apply })) : null);
  return [h('h3', { id: 'claude-pick', text: t('set_claude_which'), class: 'mt14' }),
    c.found && c.fallback ? h('div', { class: 'hint mb8', text: claudeFallbackText(c) }) : null,
    ...out,
    h('p', { class: 'small muted', text: t('cc_note') + ' ' + t('claude_which_note') })];
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

  const advanced = h('details', { open: S.ui.advancedOpen, ontoggle: (e) => { S.ui.advancedOpen = e.target.open; } }, h('summary', { text: t('set_advanced') }), h('div', { class: 'mt12' },
    num('cooldown_seconds', t('set_cooldown')), num('max_switches_per_hour', t('set_max')), num('exhausted_retry_minutes', t('set_retry')),
    h('label', { class: 'field' }, h('span', { text: t('set_prompt') }), prompt,
      h('div', { class: 'mt6' }, h('button', { class: 'btn', text: t('apply'), onclick: () => setConfig('continue_prompt', prompt.value) }))),
    share,
    h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: c.remote_control, onchange: (e) => setConfig('remote_control', e.target.checked) }),
      h('span', { text: t('set_rc') })),
    claudePicker(st)));
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
    h('label', { class: 'check mt14' }, h('input', { type: 'checkbox', checked: c.office, onchange: (e) => setConfig('office', e.target.checked) }),
      h('span', { text: t('office_set') })),
    h('p', { class: 'small muted', text: t('office_set_hint') }),
    loginsSetting(st),
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
          h('p', { text: t('uninstall_body') }),
          uninstallLogins(st.desktop_logins) ? h('p', { class: 'small', text: uninstallLogins(st.desktop_logins) }) : null,
          h('p', { class: 'small muted', text: t('uninstall_confirm') }),
          h('div', { class: 'foot' }, h('button', { class: 'btn', text: t('cancel'), onclick: closeDialog }),
            h('button', { class: 'btn primary danger', text: t('uninstall_ok'), onclick: async () => { closeDialog(); await api('uninstall', {}).catch(showError); } }))) }))),
  ];
}

// uninstallLogins says what uninstalling deletes of the desktop app's sign-ins, and where the
// app stays (on its account, or signed out), as `baton uninstall` and turning switching off
// say it; '' when baton keeps nothing for it. dl is the state's summary.
function uninstallLogins(dl) {
  if (!dl || !(dl.kept || dl.unnamed)) return '';
  return [dlKept(dl, 'dl_forget_all_body', 'dl_unnamed_only', 'dl_unnamed_too'),
    dl.active ? t('dl_forget_all_stays', { a: dl.active }) : dl.problem ? '' : t('dl_off_blank')].filter(Boolean).join(' ');
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

// ---- office --------------------------------------------------------------------------------
// The office (office.js) is mounted once into a root of its own that render()
// puts back instead of rebuilding, so its animation and its log survive the
// redraws every state change brings.

let officeHost = null;
function officeRoot() {
  if (officeHost || !window.BatonOffice) return officeHost;
  officeHost = h('div', { class: 'office-host' });
  window.BatonOffice.mount(officeHost, {
    api, t, h, standalone: officeOnly,
    lang: () => S.lang,
    accounts: () => (S.state ? S.state.accounts.map((a) => a.id) : []),
    focus: (runId) => api('sessions/focus', { run_id: runId }).catch(showError),
    popout: () => api('office/popout', {}).catch(showError),
    // The side columns as this kind of window left them (config.json: the
    // page's own storage is new every launch), and what is hidden.
    layout: () => ((S.state && S.state.config.office_layout) || {})[officeOnly ? 'popout' : 'tab'] || {},
    saveLayout: (p) => api('office/layout', Object.assign({ window: officeOnly ? 'popout' : 'tab' }, p)),
    hide: (body) => api('office/hide', body),
    error: showError,
  });
  return officeHost;
}

function showOffice(main) {
  const root = officeRoot();
  if (!root) { main.replaceChildren(h('p', { class: 'muted', text: t('error', { e: 'office.js' }) })); return; }
  if (main.firstChild !== root || main.childNodes.length !== 1) main.replaceChildren(root);
  window.BatonOffice.show();
}

function hideOffice() {
  if (officeHost && window.BatonOffice) window.BatonOffice.hide();
}

// renderOfficeOnly draws the pop-out window: the office alone, nothing else.
function renderOfficeOnly() {
  document.body.classList.add('office-only');
  document.title = t('office_window_title');
  document.getElementById('app').classList.add('wide');
  showOffice(document.getElementById('main'));
}

// ---- start ---------------------------------------------------------------------------------

connect().catch(() => {}).then(refresh).then(schedule);
