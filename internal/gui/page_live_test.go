package gui

import (
	"context"
	"encoding/json"
	"html"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"baton/internal/winui"
)

// The page itself, run by Edge without a window (headless) on app.js with
// the server's answers made up: what dialogs say after an answer, which no
// Go test can read. It starts a browser, so only on asking:
// BATON_LIVE_PAGE=1 go test ./internal/gui -run LivePage

// livePageHTML loads app.js with fetch answering from window.answers (by
// path, in order; the state poll never answers), then the checks in
// livePageChecks, which write what they read into #out.
const livePageHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>page</title></head>
<body>
<div id="app"><div id="topbar"></div><nav id="tabs"></nav><div id="banners"></div><main id="main"></main></div>
<div id="toast"></div><dialog id="dialog"></dialog><pre id="out"></pre>
<script>
window.answers = {};
window.answer = (path, status, body) => { (window.answers[path] = window.answers[path] || []).push({ status, body }); };
window.fetch = (url) => {
  const path = String(url).replace(/^\/api\//, '').split('?')[0];
  const q = window.answers[path] || [];
  if (!q.length) return new Promise(() => {});
  const a = q.shift();
  return Promise.resolve(a.gate).then(() => new Response(JSON.stringify(a.body), { status: a.status, headers: { 'Content-Type': 'application/json' } }));
};
</script>
<script src="app.js"></script>
<script src="checks.js"></script>
</body></html>
`

const livePageChecks = `
(async () => {
  const out = {};
  const settle = async () => { for (let i = 0; i < 30; i++) await new Promise((r) => setTimeout(r, 0)); };
  const dialog = () => document.getElementById('dialog').textContent;
  const click = (sel) => document.querySelector('#dialog ' + sel).click();
  try {
    window.refresh = async () => {};
    window.render = () => {};
    S.lang = 'en';
    S.state = { runs: [], accounts: [], config: {}, desktop: true };

    // A name with $ patterns in it goes in as it is.
    out.dollar = t('dl_rename_title', { a: "R&D $& Co $' $$" });

    // A delete refused after it took the conversation off the desktop app.
    const plan = { ok: true, blockers: [], title: 'Fix the login bug', cwd: 'C:\\proj', desktop_off: ['a', ''], notes: [] };
    const held = { code: 'open_elsewhere', holder: 'cli' };
    answer('sessions/delete-plan', 200, plan);
    answer('sessions/delete', 409, { error: 'x', code: 'open_elsewhere', blocker: held, desktop_off: ['a', ''] });
    answer('sessions/delete-plan', 200, Object.assign({}, plan, { ok: false, blockers: [held], desktop_off: [] }));
    answer('sessions/delete-plan', 200, Object.assign({}, plan, { ok: false, blockers: [held], desktop_off: [] }));
    const c = { session_id: 'aaaaaaaa-0000-4000-8000-000000000001', where: 'a', cwd: 'C:\\proj', title: 'Fix the login bug' };
    openDelete(c);
    await settle();
    out.del_before = dialog();
    click('.btn.primary.danger');
    await settle();
    out.del_refused = dialog();
    click('.warn-tip .small-btn'); // Check again
    await settle();
    out.del_again = dialog();

    // One that stopped part way, its dialog closed meanwhile: the toast says it.
    let open;
    answer('sessions/delete-plan', 200, plan);
    window.answers['sessions/delete'] = [{ status: 500, gate: new Promise((r) => { open = r; }),
      body: { error: 'x', code: 'partly', details: '3', desktop_off: ['a'], moved: 1, left: 3 } }];
    openDelete(c);
    await settle();
    click('.btn.primary.danger');
    await settle();
    closeDialog();
    open();
    await settle();
    out.del_closed_toast = document.getElementById('toast').textContent;

    // What baton keeps of the desktop app's sign-ins: what signed-out apps left is no sign-in.
    const only = { on: false, kept: 0, unnamed: 2, store: 'C:\\b' };
    S.state = { runs: [], accounts: [], desktop: true, config: { desktop_logins: false }, desktop_logins: only };
    const box = document.createElement('div');
    box.replaceChildren(...loginsSetting(S.state).filter(Boolean));
    out.leftover_only = box.textContent;
    openForgetAll(only);
    out.forget_all_only = dialog();
    answer('desktop/logins/drop-all', 200, { ok: true, deleted: 0 });
    click('.btn.primary.danger');
    await settle();
    out.forgot_all_only = document.getElementById('toast').textContent;
    const both = { on: true, kept: 1, unnamed: 2, active: 'work', store: 'C:\\b' };
    openForgetAll(both);
    out.forget_all_both = dialog();
    openForgetAll(Object.assign({}, both, { active: '' }));
    out.forget_all_signed_out = dialog();
    openLoginsOff({ desktop_logins: both });
    out.off_both = dialog();
    openLoginsOff({ desktop_logins: only });
    out.off_only = dialog();
    box.replaceChildren(...loginsSetting({ desktop: true, config: { desktop_logins: false }, desktop_logins: Object.assign({}, both, { on: false }) }).filter(Boolean));
    out.leftover_both = box.textContent;
    closeDialog();

    // A switch that was cut off: Put it back refused in a way waiting cannot mend offers to leave
    // the app as it is (after asking); a refusal that passes (the app runs) does not.
    const banners = () => document.getElementById('banners').textContent;
    const cut = { on: false, cut_off: true, running: false, kept: 0, active: 'work', store: 'C:\b' };
    S.state = { runs: [], accounts: [], desktop: true, claude: { found: true }, config: { switch_mode: 'off', desktop_logins: false }, desktop_logins: cut };
    renderBanners(S.state);
    out.banner_cut = banners();
    answer('desktop/logins/recover', 409, { error: 'x', code: 'dl_used_since' });
    await recoverLogins();
    renderBanners(S.state);
    out.banner_used = banners();
    [...document.querySelectorAll('#banners button')].find((b) => b.textContent === t('dl_leave')).click();
    out.leave_dialog = dialog();
    // Refused while another program holds one of the app's files: try again, not Put it back.
    answer('desktop/logins/settle', 409, { error: 'x', code: 'dl_switch_unrestored' });
    click('.btn.primary.danger');
    await settle();
    out.leave_refused = dialog();
    answer('desktop/logins/settle', 200, { ok: true, settled: true });
    cut.active = 'home';
    click('.btn.primary.danger');
    await settle();
    out.left_toast = document.getElementById('toast').textContent;
    renderBanners(S.state);
    out.banner_after_leave = banners();
    answer('desktop/logins/recover', 409, { error: 'x', code: 'dl_switch_unrestored' });
    await recoverLogins();
    renderBanners(S.state);
    out.banner_unrestored = banners();
    answer('desktop/logins/recover', 409, { error: 'x', code: 'dl_app_running' });
    await recoverLogins();
    renderBanners(S.state);
    out.banner_running = banners();
    S.state.desktop_logins = Object.assign({}, cut, { cut_off: false });
    renderBanners(S.state);
    out.banner_gone = banners();

    // Copies a switch left in the app's folder, deleted on request.
    const copies = { on: false, kept: 0, unnamed: 0, leftover: 2, store: 'C:\b' };
    box.replaceChildren(...loginsSetting({ desktop: true, config: { desktop_logins: false }, desktop_logins: copies }).filter(Boolean));
    out.copies = box.textContent;
    answer('desktop/logins/tidy', 200, { ok: true, deleted: 2 });
    [...box.querySelectorAll('button')].find((b) => b.textContent === t('dl_copies_go')).click();
    await settle();
    out.copies_toast = document.getElementById('toast').textContent;

    // A delete's newer refusals, and a session window that is still open: its session has
    // ended, so the state lists no run for it (it lists only sessions still going).
    S.state = { runs: [], accounts: [], config: {}, desktop: true };
    out.del_window = blockerNote({ code: 'session_window', run_id: 'r1' }, '', async () => {}).textContent;
    // A session still going shows its window when the state says it has one.
    out.del_in_baton = blockerNote({ code: 'in_baton', run_id: 'r2' }, '', async () => {}).textContent;
    S.state = { runs: [{ run_id: 'r2', window: true }], accounts: [], config: {}, desktop: true };
    out.del_in_baton_window = blockerNote({ code: 'in_baton', run_id: 'r2' }, '', async () => {}).textContent;
    out.del_linked = delText({ code: 'linked_folder' });
    out.del_changed = delErr({ code: 'changed', data: {} });
    out.del_generic = delErr({ code: 'delete', message: 'x', data: {} });

    // A conversation behind a link or junction: baton read no name, but it is no leftover of a
    // delete; one that is says so (the server tells which). The server says made_in "outside"
    // for both (no name of the file was seen), which says nothing of where it was made. One
    // behind a link that baton could name, made outside, says so.
    const nameless = { ok: false, blockers: [{ code: 'linked_folder' }], title: '', cwd: '', source: '', made_in: 'outside', notes: [], leftover: false };
    box.replaceChildren(...planView(c, nameless, 'a', null));
    out.del_linked_plan = box.textContent;
    box.replaceChildren(...planView(c, Object.assign({}, nameless, { ok: true, blockers: [], leftover: true }), 'a', null));
    out.del_leftover_plan = box.textContent;
    // The conversation's own file, empty or with rows that can't be read: no title, folder or
    // source, but no leftover either. Its row in Recent sessions has no title.
    const empty = Object.assign({}, nameless, { ok: true, blockers: [], made_in: 'baton', items: { files: 1, folders: 2, runs: 1, bytes: 0 } });
    box.replaceChildren(...planView(Object.assign({}, c, { title: '' }), empty, 'a', null));
    out.del_empty_plan = box.textContent;
    S.lang = 'ko';
    box.replaceChildren(...planView(Object.assign({}, c, { title: '' }), empty, 'a', null));
    out.ko_del_empty_plan = box.textContent;
    S.lang = 'en';
    box.replaceChildren(...planView(c, Object.assign({}, nameless, { title: 'Tidy the docs', cwd: 'C:\\proj', source: 'cli' }), 'claude-code', null));
    out.del_linked_named = box.textContent;

    // Uninstall: what goes of the desktop app's sign-ins, and where the app stays.
    const uninstall = (dl) => {
      const help = document.createElement('div');
      help.replaceChildren(...viewHelp({ version: 'x', home: 'C:\\b', config: {}, desktop_logins: dl }).filter(Boolean));
      [...help.querySelectorAll('button')].find((b) => b.textContent === t('uninstall_go')).click();
      return dialog();
    };
    out.un_on = uninstall({ on: true, kept: 2, unnamed: 0, active: 'work' });
    out.un_blank = uninstall({ on: true, kept: 1, unnamed: 0, active: '' });
    out.un_unnamed = uninstall({ on: false, kept: 0, unnamed: 2 });
    out.un_none = uninstall({ on: false, kept: 0, unnamed: 0 });
    closeDialog();
    out.un_running = errText({ code: 'cut_off_running' });
    out.un_inside = errText({ code: 'cut_off_inside_claude' });
    out.dl_inside = loginErr({ code: 'dl_inside_claude' });
    out.store_perm = loginErr({ code: 'dl_unsaved_current', details: 'store_permissions' });
    // Why the app's folders are not a layout baton switches, when it can tell.
    out.layout_several = loginErr({ code: 'dl_unsupported_layout', details: 'several' });
    out.layout_other = loginErr({ code: 'dl_unsupported_layout', details: 'other' });
    // Turning switching on after its notice was accepted before goes through the setting, not
    // the notice's dialog; its refusal is worded the same way.
    answer('config', 409, { error: 'x', code: 'dl_unsaved_current', details: 'store_permissions' });
    await turnLoginsOn({ config: { desktop_logins_ack: true } });
    out.store_perm_on = document.getElementById('toast').textContent;

    // Switching while the desktop app runs asks first; what is working is listed, session by
    // session; a switch that failed after the quit says the app is open again.
    openQuitSwitch({ account: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', label: 'home' });
    out.quit_ask = dialog();
    answer('desktop/logins/use', 409, { error: 'x', code: 'dl_app_working',
      working: [{ title: 'fix the bug', what: 'turn' }, { title: '', what: 'command' }, { title: 'x', what: 'later' }] });
    click('.btn.primary');
    await settle();
    out.quit_working = dialog();
    out.quit_working_items = String(document.querySelectorAll('#dialog li').length);
    answer('desktop/logins/use', 409, { error: 'x', code: 'dl_switch_failed', quit: true, opened: true });
    click('.btn.primary');
    await settle();
    out.quit_reopened = dialog();
    answer('desktop/logins/use', 200, { ok: true, to: 'b', quit: true, opened: true });
    click('.btn.primary');
    await settle();
    out.quit_done = document.getElementById('toast').textContent;
    out.quit_refused = loginErr({ code: 'dl_quit_failed', details: 'refused' });
    out.quit_late = loginErr({ code: 'dl_quit_failed' });
    out.working_toast = loginErr({ code: 'dl_app_working', data: { working: [{ title: 'loop', what: 'wakeup' }] } });

    S.lang = 'ko';
    out.ko_forget = t('dl_forget_body', { a: '개인' });
    out.ko_working = loginErr({ code: 'dl_app_working', data: { working: [{ title: '루프', what: 'wakeup' }] } });
    out.ko_used = t('err_dl_used_since');
    out.ko_inside = errText({ code: 'cut_off_inside_claude' });
    out.ko_dl_inside = loginErr({ code: 'dl_inside_claude' });
    out.ko_layout_no_app = loginErr({ code: 'dl_unsupported_layout', details: 'no_app' });
    out.ko_leave_refused = settleErr({ code: 'dl_switch_unrestored' });
    out.done = 'yes';
  } catch (e) {
    out.error = String(e && e.stack || e);
  }
  document.getElementById('out').textContent = JSON.stringify(out);
})();
`

func TestLivePage(t *testing.T) {
	if os.Getenv("BATON_LIVE_PAGE") != "1" {
		t.Skip("starts Edge: BATON_LIVE_PAGE=1 runs it")
	}
	edge := winui.FindEdge()
	if edge == "" {
		t.Skip("no Edge here")
	}
	dir := t.TempDir()
	js, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"app.js": string(js), "checks.js": livePageChecks, "page.html": livePageHTML} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Edge's profile goes outside the test's folder: Edge's helpers may let
	// go of it a moment after it ends.
	prof, err := os.MkdirTemp("", "baton-livepage-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for range 20 {
			if os.RemoveAll(prof) == nil {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	page := "file:///" + filepath.ToSlash(filepath.Join(dir, "page.html"))
	b, err := exec.CommandContext(ctx, edge, "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
		"--disable-extensions", "--disable-sync", "--disable-background-networking", "--disable-component-update",
		"--user-data-dir="+prof, "--virtual-time-budget=20000", "--dump-dom", page).Output()
	if err != nil {
		t.Fatalf("edge: %v", err)
	}
	dom := string(b)
	i, j := strings.Index(dom, `<pre id="out">`), strings.Index(dom, "</pre>")
	if i < 0 || j < i {
		t.Fatalf("no answer from the page:\n%s", dom)
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(html.UnescapeString(dom[i+len(`<pre id="out">`):j])), &out); err != nil || out["done"] != "yes" {
		t.Fatalf("the checks did not finish: %v %q", err, out)
	}

	has := func(key string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(out[key], w) {
				t.Errorf("%s: no %q in %q", key, w, out[key])
			}
		}
	}
	hasNot := func(key string, not ...string) {
		t.Helper()
		for _, w := range not {
			if strings.Contains(out[key], w) {
				t.Errorf("%s: %q in %q", key, w, out[key])
			}
		}
	}
	if out["dollar"] != "Rename “R&D $& Co $' $$”" {
		t.Errorf("dollar: %q", out["dollar"])
	}

	// The delete: refused after it took the conversation off the desktop
	// app, which the dialog keeps saying after a look finds it off already.
	off := "It no longer shows in the Claude desktop app for: a, the desktop app's own account. You can show it there again with “Choose desktop app accounts”."
	has("del_before", "baton also takes it off the Claude desktop app for: a, the desktop app's own account.")
	hasNot("del_before", off)
	has("del_refused", "It is open in Claude Code in a terminal (CLI). Close it there first.", off)
	has("del_again", "Checked again: still the same.", off)
	has("del_closed_toast", "Only part of it went", "It no longer shows in the Claude desktop app for: a.")

	// What signed-out apps left is counted apart from the sign-ins.
	leftovers := "2 item(s) a signed-out desktop app left (no token recorded)"
	has("leftover_only", "baton still keeps "+leftovers+".")
	has("forget_all_only", "baton deletes "+leftovers+". It keeps no sign-in.")
	has("forgot_all_only", "Deleted.")
	has("forget_all_both", "baton deletes the 1 sign-in(s) it keeps for the Claude desktop app. It also deletes "+leftovers+".",
		"does not sign the accounts out", "The desktop app stays signed in to work.")
	has("forget_all_signed_out", "The desktop app is signed out now")
	has("off_both", "baton deletes the 1 sign-in(s) it keeps for the desktop app. It also deletes "+leftovers+".",
		"does not sign the accounts out", "The desktop app stays signed in to work.")
	has("off_only", "baton deletes "+leftovers+". It keeps no sign-in.")
	has("leftover_both", "baton still keeps 1 desktop app sign-in(s) from before. It also keeps "+leftovers+".")
	for _, k := range []string{"leftover_only", "forget_all_only", "forgot_all_only", "off_only"} {
		hasNot(k, "sign-in(s)", "does not sign the accounts out")
	}
	hasNot("forget_all_both", "anything set aside", "the account it is on")
	hasNot("forget_all_signed_out", "stays signed in")
	has("ko_forget", "다시 개인 계정으로 바꾸려면")

	// A switch that was cut off.
	has("banner_cut", "was cut off", "Put it back")
	hasNot("banner_cut", "Leave it as it is")
	has("banner_used", "has been used on the sign-in this switch put in", "Put it back", "Leave it as it is")
	has("leave_dialog", "Leave the desktop app as it is?", "It stays signed in to the account it is on now", "Sign-ins baton kept stay kept.")
	has("leave_refused", "Leave the desktop app as it is?", "could not leave the desktop app as it is yet", "Do not open the desktop app; try again in a moment.")
	hasNot("leave_refused", "Put it back")
	has("ko_leave_refused", "데스크톱 앱을 열지 말고 잠시 뒤 다시 시도하세요")
	has("left_toast", "Left the desktop app as it is: it stays signed in to home.")
	hasNot("banner_after_leave", "Leave it as it is", "has been used")
	has("banner_unrestored", "could not put everything back yet", "If putting it back keeps failing", "Leave it as it is")
	has("banner_running", "was cut off", "Put it back")
	hasNot("banner_running", "Leave it as it is")
	hasNot("banner_gone", "cut off", "Put it back")
	has("copies", "A switch that stopped early left 2 copy(ies) in the desktop app's folder", "Delete the copies")
	has("copies_toast", "Deleted the copies.")

	// A delete's newer refusals. A session's window is offered when its session has ended (no
	// run in the state), or when the state says a session still going has one.
	has("del_window", "its window is still open", "Show window")
	hasNot("del_in_baton", "Show window")
	has("del_in_baton_window", "Show window")
	has("del_linked", "link or junction")
	has("del_linked_plan", "Fix the login bug")
	hasNot("del_linked_plan", "Leftover files", "made outside baton")
	has("del_leftover_plan", "Leftover files of a deleted conversation")
	hasNot("del_leftover_plan", "Fix the login bug", "made outside baton")
	has("del_empty_plan", "(untitled)")
	hasNot("del_empty_plan", "Leftover files", "made outside baton")
	has("ko_del_empty_plan", "(제목 없음)")
	hasNot("ko_del_empty_plan", "삭제하다 남은", "baton 밖")
	has("del_linked_named", "Tidy the docs", "It was made outside baton (Terminal); it goes from there too.")

	// Uninstall says where the desktop app stays and what goes of what baton keeps for it, as
	// `baton uninstall` does: signed in or signed out, never both.
	has("un_on", "baton deletes the 2 sign-in(s) it keeps for the Claude desktop app.", "The desktop app stays signed in to work.")
	has("un_blank", "baton deletes the 1 sign-in(s)", "The desktop app is signed out now")
	hasNot("un_blank", "stays signed in", "the other sign-ins")
	has("un_unnamed", "baton deletes 2 item(s) a signed-out desktop app left (no token recorded). It keeps no sign-in.")
	hasNot("un_unnamed", "sign-in(s)")
	hasNot("un_none", "sign-in(s)", "item(s)", "stays signed in", "signed out now")
	has("un_running", "was cut off", "Quit the desktop app completely", "then uninstall")
	// Opening baton again while this window is open only brings this window back (one window
	// per data folder), still inside Claude Code: it must close first.
	has("un_inside", "was cut off", "CLAUDECODE is set", "Close this window, open baton again from the Start menu (or from its folder), then uninstall")
	has("dl_inside", "CLAUDECODE is set", "Close this window and open baton again from the Start menu (or from its folder)")
	has("ko_inside", "이 창을 닫고 시작 메뉴(또는 baton 폴더)에서 baton을 다시 연 뒤 제거하세요")
	has("ko_dl_inside", "이 창을 닫고 시작 메뉴(또는 baton 폴더)에서 baton을 다시 여세요")
	has("store_perm", "permissions of its own", "kept nothing and changed nothing")
	has("layout_several", "More than one folder in Claude-Data", "it changes nothing")
	// Switching while the desktop app runs.
	has("quit_ask", "Quit the desktop app and switch it to home?", "checks that nothing in it is working", "Quit and switch")
	has("quit_working", "Something in the desktop app is still working", "fix the bug: Claude is working",
		"(untitled): a command runs in the background", "x: baton cannot tell now whether it is working")
	if got := out["quit_working_items"]; got != "3" {
		t.Errorf("quit_working_items = %v, want 3", got)
	}
	has("quit_reopened", "The desktop app was opened again, on the account it was on.")
	has("quit_done", "baton quit the desktop app and switched it to home. Opening it again.")
	has("quit_refused", "would not quit now", "nothing was changed")
	has("quit_late", "did not quit in time", "sign-in was not changed", "still be closing")
	has("working_toast", "Something in the desktop app is still working", "loop: Claude is to wake up again")
	has("ko_working", "데스크톱 앱에서 아직 작업이 돌고 있어", "루프: Claude가 다시 깨어날 예정")
	has("layout_other", "not supported for this kind of Claude desktop app install yet")
	has("ko_layout_no_app", "데이터 폴더를 찾지 못했습니다", "한 번 연 뒤 다시 시도하세요")
	has("store_perm_on", "permissions of its own", "kept nothing and changed nothing")
	has("del_changed", "Its files moved while it was being deleted")
	has("del_generic", "baton could not delete it just now")
	has("ko_used", "대신 앱을 지금 그대로 둘 수 있습니다")
}
