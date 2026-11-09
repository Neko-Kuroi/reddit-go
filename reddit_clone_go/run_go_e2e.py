"""Phase 6: the Python version's 99-item E2E, applied to the Go server.

Item-for-item port of ~/workspace/reddit_clone_audit/run_fix_e2e.py.
Differences from the original harness:
  - drives the real Go server over HTTP (no ASGI transport);
  - DB assertions use raw SQL (the tables are the shared schema);
  - item 1 (create_all) becomes "Go migrations applied to an empty DB";
  - item 69 asserts the Python-stored email exactly; the Go server
    stores the address as typed (the Python value comes from
    pydantic EmailStr lowercasing the domain part only), so this item
    is expected to show the one documented difference.
  - the two double-click items are fired from two OS threads (see
    double_click): a single asyncio client dispatches the pair
    near-sequentially, so the second request would arrive after the
    first commits and read as a deliberate toggle — the Python ASGI
    harness overlapped naturally because both handlers shared one
    event loop with the transaction held open until response end.

Run with the audit venv python, Go server on :8129, database rc_go_p6
already migrated by the Go binary.
"""
import asyncio
import os
import re
import uuid

import httpx
from sqlalchemy import text
from sqlalchemy.ext.asyncio import create_async_engine

BASE = "http://localhost:8129"
DB_URL = os.environ.get(
    "GO_E2E_DATABASE_URL",
    "postgresql+asyncpg://rc_test:rc_test_pass@localhost:5432/rc_go_p6",
)
engine = create_async_engine(DB_URL)

RESULTS = []


def check(name, condition, extra=""):
    RESULTS.append((name, bool(condition)))
    print(f"{'PASS' if condition else 'FAIL'} {name}" + (f"  [{extra}]" if extra else ""), flush=True)


class Client:
    def __init__(self):
        # trust_env=False: this VM exports proxy variables that would
        # otherwise route localhost traffic through the proxy.
        self.http = httpx.AsyncClient(
            base_url=BASE, follow_redirects=False, trust_env=False)

    async def close(self):
        await self.http.aclose()

    def csrf(self):
        return self.http.cookies.get("csrf_token", "")

    async def get(self, url, **kwargs):
        return await self.http.get(url, **kwargs)

    async def form_post(self, url, data, csrf=True, **kwargs):
        body = dict(data)
        if csrf:
            body["csrf_token"] = self.csrf()
        return await self.http.post(url, data=body, **kwargs)

    async def hx_post(self, url, data, csrf=True, **kwargs):
        headers = dict(kwargs.pop("headers", {}))
        if csrf:
            headers["X-CSRFToken"] = self.csrf()
        return await self.http.post(url, data=data, headers=headers, **kwargs)


async def scalar(sql, **params):
    async with engine.connect() as conn:
        return await conn.scalar(text(sql), params)


async def register(client, username, email, password="password123"):
    await client.get("/register")
    return await client.form_post(
        "/register", {"username": username, "email": email, "password": password}
    )


def _double_click_sync(cookies, url, value):
    """Fire the same vote POST from two threads released at the same
    instant (barrier), each on a pre-warmed connection."""
    import threading

    barrier = threading.Barrier(2)
    results = [None, None]

    def worker(i):
        with httpx.Client(base_url=BASE, cookies=cookies, trust_env=False,
                          follow_redirects=False) as c:
            c.get("/health")  # warm the connection before the race
            barrier.wait()
            r = c.post(url, data={"value": value},
                       headers={"X-CSRFToken": cookies.get("csrf_token", "")})
            results[i] = r.status_code

    threads = [threading.Thread(target=worker, args=(i,)) for i in range(2)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    return results


async def double_click(client, url, value=1):
    """POST the same vote twice at the same moment. Returns the two
    status codes."""
    cookies = {cookie.name: cookie.value for cookie in client.http.cookies.jar}
    loop = asyncio.get_running_loop()
    return await loop.run_in_executor(
        None, _double_click_sync, cookies, url, value)


async def main():
    applied = await scalar("SELECT count(*) FROM schema_migrations")
    check("新-2: スキーマが PostgreSQL に作られる（Go 版はマイグレーション0001-0009）", applied == 9, f"applied={applied}")

    alice = Client()
    bob = None
    extra_clients = []
    try:
        # ---- 従来55項目 ----
        r = await alice.get("/")
        check("未ログインのホームは /login へ 303（要ログイン仕様）", r.status_code == 303 and r.headers.get("location") == "/login", f"got {r.status_code}")
        r = await alice.get("/login")
        check("ログインページ表示（200）", r.status_code == 200, f"got {r.status_code}")
        check("乙-7: 初回応答で csrf_token Cookie が発行される", bool(alice.csrf()))
        m = re.search(r'<meta name="csrf-token" content="([^"]*)"', r.text)
        check("乙-7: base.html の meta トークンが Cookie と一致する", bool(m) and m.group(1) == alice.csrf())
        r = await alice.form_post("/register", {"username": "alice", "email": "alice@example.com", "password": "password123"}, csrf=False)
        check("乙-7: CSRF トークン無しの登録 POST は 403", r.status_code == 403, f"got {r.status_code}")
        r = await alice.get("/register")
        check("登録ページに csrf hidden フィールドがある", 'name="csrf_token"' in r.text)
        r = await alice.form_post("/register", {"username": "alice", "email": "alice@example.com", "password": "password123"})
        check("乙-7: CSRF 付き通常フォーム登録が成功する（303）", r.status_code == 303 and r.headers.get("location") == "/", f"got {r.status_code}")
        r = await alice.get("/")
        check("甲-6: PG 構成で登録後のログイン状態が保持される（ホームにユーザー名）", r.status_code == 200 and "alice" in r.text, f"got {r.status_code}")
        r = await alice.get("/u/alice")
        check("丙-7: プロフィールが表示され日付書式が新形式（〜に登録）", r.status_code == 200 and "に登録" in r.text, f"got {r.status_code}")
        r = await alice.get("/u/nobody_here")
        check("乙-6: 存在しないユーザーは 404 ページ（500 でない）", r.status_code == 404, f"got {r.status_code}")
        r = await alice.get("/subreddits/new")
        check("コミュニティ作成ページ表示（200）", r.status_code == 200, f"got {r.status_code}")
        r = await alice.form_post("/subreddits/new", {"name": "elixir", "description": "Elixir talk"}, csrf=False)
        check("乙-7: CSRF 無しのコミュニティ作成 POST は 403", r.status_code == 403, f"got {r.status_code}")
        r = await alice.form_post("/subreddits/new", {"name": "elixir", "description": "Elixir talk"})
        check("コミュニティ作成成功（303）", r.status_code == 303 and r.headers.get("location") == "/r/elixir", f"got {r.status_code}")
        r = await alice.form_post("/subreddits/new", {"name": "elixir", "description": "duplicate"})
        check("重複コミュニティ名はフォームエラー（422）", r.status_code == 422, f"got {r.status_code}")
        r = await alice.get("/r/elixir")
        check("乙-4: Subreddit ページに報告モーダルが含まれる", r.status_code == 200 and 'id="report-modal"' in r.text, f"got {r.status_code}")
        r = await alice.get("/r/elixir/submit")
        check("乙-1: /r/elixir/submit が 200 で投稿フォームを表示", r.status_code == 200, f"got {r.status_code}")
        r = await alice.form_post("/r/elixir/submit", {"title": "Hello", "post_type": "text", "body": "World", "url": ""})
        text_location = r.headers.get("location", "")
        check("テキスト投稿の作成（303）", r.status_code == 303 and "/r/elixir/" in text_location, f"got {r.status_code}")
        text_post_id = text_location.rstrip("/").split("/")[-1]
        r = await alice.form_post("/r/elixir/submit", {"title": "Link Post", "post_type": "link", "body": "", "url": "https://example.com"})
        link_location = r.headers.get("location", "")
        check("リンク投稿の作成（303）", r.status_code == 303 and "/r/elixir/" in link_location, f"got {r.status_code}")
        link_post_id = link_location.rstrip("/").split("/")[-1]
        r = await alice.form_post("/r/elixir/submit", {"title": "Too Long", "post_type": "text", "body": "x" * 40001, "url": ""})
        check("丙-3: 本文 40,001 字は 422 で拒否される", r.status_code == 422, f"got {r.status_code}")
        r = await alice.get(text_location)
        check("投稿詳細ページ表示（200）", r.status_code == 200, f"got {r.status_code}")
        specific_count = r.text.count(f'hx-post="/posts/{text_post_id}/vote"')
        check("乙-2: 詳細ページの投票 URL が正しく組み立てられる", specific_count == 2, f"specific_count={specific_count}")
        check("乙-4: 詳細ページに報告モーダルが含まれる", 'id="report-modal"' in r.text)
        r = await alice.hx_post(f"/posts/{text_post_id}/vote", {"value": 1}, csrf=False)
        check("乙-7: CSRF ヘッダー無しの投票 POST は 403", r.status_code == 403, f"got {r.status_code}")
        r = await alice.hx_post(f"/posts/{text_post_id}/vote", {"value": 1})
        check("自分の投稿への投票は 400（業務ルール）", r.status_code == 400, f"got {r.status_code}")
        r = await alice.form_post(f"/posts/{text_post_id}/comments", {"body": "first comment", "parent_id": ""})
        check("コメント投稿（303）", r.status_code == 303, f"got {r.status_code}")
        r = await alice.get(text_location)
        comment_match = re.search(r"reply-form-([0-9a-f-]{36})", r.text)
        first_comment_id = comment_match.group(1) if comment_match else ""
        check("乙-5: コメント表示と返信ボタン（toggleReplyForm 呼び出し）がある", r.status_code == 200 and "first comment" in r.text and "toggleReplyForm" in r.text, f"got {r.status_code}")
        r = await alice.form_post(f"/posts/{text_post_id}/comments", {"body": "reply comment", "parent_id": first_comment_id})
        check("返信コメント投稿（303）", r.status_code == 303, f"got {r.status_code}")
        r = await alice.form_post(f"/posts/{text_post_id}/comments", {"body": "bad parent", "parent_id": "not-a-uuid"})
        check("丙-6: 不正な形式の parent_id は 422（500 でない）", r.status_code == 422, f"got {r.status_code}")
        r = await alice.form_post(f"/posts/{text_post_id}/comments", {"body": "missing parent", "parent_id": str(uuid.uuid4())})
        check("丙-6: 存在しない parent_id は 422", r.status_code == 422, f"got {r.status_code}")
        r = await alice.form_post(f"/posts/{link_post_id}/comments", {"body": "cross parent", "parent_id": first_comment_id})
        check("丙-6: 別投稿のコメントを親に指定すると 422", r.status_code == 422, f"got {r.status_code}")
        r = await alice.hx_post("/r/elixir/leave", {})
        check("Leave（HTMX, CSRF ヘッダー）が成功する", r.status_code == 200, f"got {r.status_code}")
        r = await alice.hx_post("/r/elixir/join", {})
        check("Join（HTMX, CSRF ヘッダー）が成功する", r.status_code == 200, f"got {r.status_code}")
        r = await alice.get("/components/report-reasons")
        check("報告理由リセット用エンドポイント（200）", r.status_code == 200 and "スパム" in r.text, f"got {r.status_code}")
        r = await alice.get("/search?q=Hello")
        check("検索で投稿がヒットする", r.status_code == 200 and "Hello" in r.text, f"got {r.status_code}")
        check("乙-4: 検索ページに報告モーダルが含まれる", 'id="report-modal"' in r.text)
        r = await alice.form_post(f"/posts/{link_post_id}/delete", {})
        check("投稿の削除（作成者, 通常フォーム+CSRF）が成功する（303）", r.status_code == 303, f"got {r.status_code}")
        r = await alice.get(link_location)
        check("削除した投稿の詳細ページは 404 になる", r.status_code == 404, f"got {r.status_code}")
        r = await alice.form_post(f"/comments/{first_comment_id}/delete", {}, headers={"referer": text_location})
        check("コメントの削除（作成者）が成功する（303）", r.status_code == 303, f"got {r.status_code}")
        r = await alice.form_post("/logout", {}, csrf=False)
        check("乙-7: CSRF 無しのログアウト POST は 403", r.status_code == 403, f"got {r.status_code}")
        r = await alice.form_post("/logout", {})
        check("乙-7: CSRF 付きログアウトが成功する（303）", r.status_code == 303, f"got {r.status_code}")
        r = await alice.get("/")
        check("ログアウト後はホームへアクセスできない（303）", r.status_code == 303, f"got {r.status_code}")
        await alice.get("/register")
        r = await alice.form_post("/register", {"username": "alice", "email": "alice@example.com", "password": "password123"})
        check("重複ユーザー名はフォームエラー（422）", r.status_code == 422, f"got {r.status_code}")
        r = await alice.form_post("/register", {"username": "charlie", "email": "charlie@example.com", "password": "short"})
        check("短いパスワードはフォームエラー（422）", r.status_code == 422, f"got {r.status_code}")

        bob = Client()
        r = await register(bob, "bob", "bob@example.com")
        check("第2ユーザーの登録（303, 自動ログイン）", r.status_code == 303, f"got {r.status_code}")
        r = await bob.get("/u/alice")
        check("ログイン後に他人のプロフィール閲覧（200）", r.status_code == 200, f"got {r.status_code}")
        r = await bob.hx_post(f"/posts/{text_post_id}/vote", {"value": 1})
        score = await scalar("SELECT score FROM posts WHERE id = :id", id=text_post_id)
        check("乙-7/乙-2: CSRF ヘッダー付き HTMX 投票が成功し score=1", r.status_code == 200 and score == 1, f"got {r.status_code}, score={score}")
        r = await bob.hx_post(f"/posts/{text_post_id}/vote", {"value": 1})
        score = await scalar("SELECT score FROM posts WHERE id = :id", id=text_post_id)
        check("同じ投票の再送でトグル解除（score=0）", r.status_code == 200 and score == 0, f"got {r.status_code}, score={score}")
        r = await bob.hx_post("/reports", {"target_type": "post", "target_id": text_post_id, "reason": "invalid_reason"})
        check("不正な報告理由は 400", r.status_code == 400, f"got {r.status_code}")
        r = await bob.hx_post("/reports", {"target_type": "post", "target_id": text_post_id, "reason": "spam"})
        check("乙-3/乙-7: モーダル経由の報告送信（name 属性の値）が成功する", r.status_code == 200 and "報告しました" in r.text, f"got {r.status_code}")
        report_count = await scalar("SELECT count(*) FROM reports")
        check("報告が DB に保存されている", report_count == 1, f"count={report_count}")
        r = await bob.hx_post("/reports", {"target_type": "post", "target_id": str(uuid.uuid4()), "reason": "spam"})
        check("新-3: 存在しない対象への報告は 400", r.status_code == 400, f"got {r.status_code}")
        report_count = await scalar("SELECT count(*) FROM reports")
        check("新-3: 存在しない対象への報告は DB に保存されない", report_count == 1, f"count={report_count}")
        await bob.form_post("/logout", {})
        await bob.get("/login")
        r = await bob.form_post("/login", {"email": "alice@example.com", "password": "wrong-password"})
        check("ログイン失敗はエラー表示（401）", r.status_code == 401, f"got {r.status_code}")
        r = await bob.form_post("/login", {"email": "alice@example.com", "password": "password123"})
        check("正しい資格情報でログイン成功（303）", r.status_code == 303, f"got {r.status_code}")

        # ---- 外部レビュー修正の追加項目 ----
        author = Client(); extra_clients.append(author)
        r = await register(author, "voteauthor", "voteauthor@example.com")
        await author.form_post("/subreddits/new", {"name": "votetopic", "description": "vote tests"})
        r = await author.form_post("/r/votetopic/submit", {"title": "Concurrent Target", "post_type": "text", "body": "target", "url": ""})
        concurrent_location = r.headers.get("location", "")
        concurrent_id = concurrent_location.rstrip("/").split("/")[-1]
        await author.form_post(f"/posts/{concurrent_id}/comments", {"body": "concurrent comment", "parent_id": ""})
        comment_id = str(await scalar("SELECT id FROM comments WHERE post_id = :id", id=concurrent_id))

        voters = []
        for i in range(1, 11):
            c = Client(); extra_clients.append(c); voters.append(c)
            await register(c, f"voter{i:02d}", f"voter{i:02d}@example.com")

        responses = await asyncio.gather(*[
            voters[i].hx_post(f"/posts/{concurrent_id}/vote", {"value": 1}) for i in range(8)
        ])
        check("レビュー2: 同時投票の全リクエストが 200", all(x.status_code == 200 for x in responses), f"statuses={[x.status_code for x in responses]}")
        score = await scalar("SELECT score FROM posts WHERE id = :id", id=concurrent_id)
        check("レビュー2: 同時投票でも最終スコアが票数の合計と一致する", score == 8, f"score={score}")
        vote_count = await scalar("SELECT count(*) FROM votes WHERE target_type = 'post' AND target_id = :id", id=concurrent_id)
        check("レビュー2: 同時投票の票がすべて保存される", vote_count == 8, f"votes={vote_count}")

        statuses = await double_click(voters[8], f"/posts/{concurrent_id}/vote")
        check("レビュー2: 二重クリックでも 500 にならない", all(s == 200 for s in statuses), f"statuses={statuses}")
        voter09_id = await scalar("SELECT id FROM users WHERE username = 'voter09'")
        vote_count = await scalar("SELECT count(*) FROM votes WHERE user_id = :u AND target_type = 'post' AND target_id = :id", u=str(voter09_id), id=concurrent_id)
        check("レビュー2: 二重クリックでも票は1つだけ数えられる", vote_count == 1, f"votes={vote_count}")
        score = await scalar("SELECT score FROM posts WHERE id = :id", id=concurrent_id)
        check("レビュー2: 二重クリック後のスコアは1票分だけ増える", score == 9, f"score={score}")

        responses = await asyncio.gather(*[
            voters[i].hx_post(f"/comments/{comment_id}/vote", {"value": 1}) for i in range(8)
        ])
        check("レビュー2: コメントへの同時投票も全リクエストが 200", all(x.status_code == 200 for x in responses), f"statuses={[x.status_code for x in responses]}")
        comment_score = await scalar("SELECT score FROM comments WHERE id = :id", id=comment_id)
        check("レビュー2: コメントへの同時投票でもスコアが票数と一致する", comment_score == 8, f"score={comment_score}")
        statuses = await double_click(voters[9], f"/comments/{comment_id}/vote")
        voter10_id = await scalar("SELECT id FROM users WHERE username = 'voter10'")
        vote_count = await scalar("SELECT count(*) FROM votes WHERE user_id = :u AND target_type = 'comment' AND target_id = :id", u=str(voter10_id), id=comment_id)
        comment_score = await scalar("SELECT score FROM comments WHERE id = :id", id=comment_id)
        check("レビュー2: コメントの二重クリックでも 500 にならず票は1つ", all(s == 200 for s in statuses) and vote_count == 1, f"statuses={statuses}, votes={vote_count}")
        check("レビュー2: コメントの二重クリック後のスコアは1票分だけ増える", comment_score == 9, f"score={comment_score}")

        await author.form_post("/r/votetopic/submit", {"title": "Downvoted Target", "post_type": "text", "body": "down", "url": ""})
        await author.form_post("/r/votetopic/submit", {"title": "Upvoted Target", "post_type": "text", "body": "up", "url": ""})
        down_id = str(await scalar("SELECT id FROM posts WHERE title = 'Downvoted Target'"))
        up_id = str(await scalar("SELECT id FROM posts WHERE title = 'Upvoted Target'"))
        responses = await asyncio.gather(*[
            c.hx_post(f"/posts/{down_id}/vote", {"value": -1}) for c in voters
        ])
        r = await voters[0].hx_post(f"/posts/{up_id}/vote", {"value": 1})
        check("レビュー3: 大量の下げ票と上げ票の投票が成功する", all(x.status_code == 200 for x in responses) and r.status_code == 200)
        down_score, down_hot = await scalar("SELECT score FROM posts WHERE id = :id", id=down_id), await scalar("SELECT hot_score FROM posts WHERE id = :id", id=down_id)
        up_score, up_hot = await scalar("SELECT score FROM posts WHERE id = :id", id=up_id), await scalar("SELECT hot_score FROM posts WHERE id = :id", id=up_id)
        check("レビュー3: hot_score に符号が入り負の投稿が下がる", down_score == -10 and up_score == 1 and down_hot < up_hot, f"down=({down_score},{down_hot}), up=({up_score},{up_hot})")
        r = await author.get("/r/votetopic?sort=hot")
        check("レビュー3: Hot 一覧で下げ票の投稿が +1 の投稿より下に来る", r.status_code == 200 and r.text.index("Upvoted Target") < r.text.index("Downvoted Target"), f"got {r.status_code}")

        carol = Client(); extra_clients.append(carol)
        r = await register(carol, "Carol", "Carol@Example.COM")
        carol_row = await scalar("SELECT username || '|' || email FROM users WHERE lower(username) = 'carol'")
        carol_name, carol_email = (carol_row or "|").split("|")
        check("方式B: 登録した綴りのまま保存される（表示用に保持。メールは EmailStr がドメイン部のみ小文字化）", r.status_code == 303 and carol_name == "Carol" and carol_email == "Carol@example.com", f"got {r.status_code} email={carol_email}")
        other = Client(); extra_clients.append(other)
        await other.get("/register")
        r = await other.form_post("/register", {"username": "carol", "email": "different@example.com", "password": "password123"})
        check("方式B: ユーザー名の大文字違いは重複として登録できない", r.status_code == 422, f"got {r.status_code}")
        r = await other.form_post("/register", {"username": "differentcarol", "email": "CAROL@example.com", "password": "password123"})
        check("方式B: メールの大文字違いも重複として登録できない", r.status_code == 422, f"got {r.status_code}")
        await carol.form_post("/logout", {})
        await carol.get("/login")
        r = await carol.form_post("/login", {"email": "CaRoL@eXaMpLe.CoM", "password": "password123"})
        check("方式B: メールの大文字違いでもログインできる", r.status_code == 303, f"got {r.status_code}")
        r = await carol.get("/u/CAROL")
        check("方式B: /u/ALICE 形式の大文字違いでプロフィールが開き、表示名は登録時の綴りのまま", r.status_code == 200 and "Carol" in r.text, f"got {r.status_code}")
        r = await carol.form_post("/subreddits/new", {"name": "Foo_Bar", "description": "case test"})
        check("方式B: Subreddit 名が入力した綴りのまま作成される", r.status_code == 303 and r.headers.get("location") == "/r/Foo_Bar", f"got {r.status_code} {r.headers.get('location')}")
        r = await voters[0].form_post("/subreddits/new", {"name": "foo_bar", "description": "duplicate case"})
        check("方式B: Subreddit 名の大文字違いは重複として作成できない", r.status_code == 422, f"got {r.status_code}")
        r = await carol.get("/r/FOO_BAR")
        check("方式B: /r/FOO_BAR で開け、表示名は登録時の綴りのまま", r.status_code == 200 and "Foo_Bar" in r.text, f"got {r.status_code}")

        for url, label in [
            ("/?page=0", "ホーム"),
            ("/r/Foo_Bar?page=0", "Subreddit"),
            ("/r/Foo_Bar/posts?page=0", "Subreddit追加読み込み"),
            ("/u/Carol?page=0", "プロフィール"),
        ]:
            r = await carol.get(url)
            check(f"レビュー3: {label}の page=0 は 422（500ではない）", r.status_code == 422, f"got {r.status_code}")
        for url, label in [
            ("/?sort=random", "ホーム"),
            ("/r/Foo_Bar?sort=random", "Subreddit"),
            ("/r/Foo_Bar/posts?sort=random", "Subreddit追加読み込み"),
            ("/u/Carol?sort=random", "プロフィール"),
        ]:
            r = await carol.get(url)
            check(f"レビュー3: {label}の不正な sort は 422", r.status_code == 422, f"got {r.status_code}")

        payload = "<script>alert(1)</script>"
        r = await voters[0].hx_post("/reports", {"target_type": "post", "target_id": concurrent_id, "reason": payload})
        check("レビュー4: 細工した報告理由は 400 で返る", r.status_code == 400, f"got {r.status_code}")
        check("レビュー4: 報告エラーで生のタグが出ずエスケープされる", payload not in r.text and "&lt;script&gt;" in r.text)

        # ---- 再レビュー残件 ----
        racer = Client(); extra_clients.append(racer)
        r = await register(racer, "racer", "racer@example.com")
        await racer.form_post("/subreddits/new", {"name": "racetopic", "description": "race tests"})

        async def vote_total(target_type, target_id):
            return await scalar(
                "SELECT COALESCE(sum(value), 0) FROM votes WHERE target_type = :t AND target_id = :id",
                t=target_type, id=target_id)

        change_ok = True
        change_detail = ""
        for round_no in range(3):
            r = await racer.form_post("/r/racetopic/submit", {"title": f"Race Change {round_no}", "post_type": "text", "body": "race", "url": ""})
            race_post_id = r.headers.get("location", "").rstrip("/").split("/")[-1]
            voter = Client(); extra_clients.append(voter)
            await register(voter, f"racevoter{round_no}", f"racevoter{round_no}@example.com")
            await voter.hx_post(f"/posts/{race_post_id}/vote", {"value": 1})
            responses = await asyncio.gather(
                voter.hx_post(f"/posts/{race_post_id}/vote", {"value": -1}),
                voter.hx_post(f"/posts/{race_post_id}/vote", {"value": -1}),
            )
            pscore = await scalar("SELECT score FROM posts WHERE id = :id", id=race_post_id)
            total = await vote_total("post", race_post_id)
            if not (all(x.status_code == 200 for x in responses) and pscore == total):
                change_ok = False
                change_detail = f"round={round_no} statuses={[x.status_code for x in responses]} score={pscore} total={total}"
        check("残件A: 投票の同時変更でもスコアと票の合計が一致する", change_ok, change_detail)

        cancel_ok = True
        cancel_detail = ""
        for round_no in range(3):
            r = await racer.form_post("/r/racetopic/submit", {"title": f"Race Cancel {round_no}", "post_type": "text", "body": "race", "url": ""})
            race_post_id = r.headers.get("location", "").rstrip("/").split("/")[-1]
            voter = Client(); extra_clients.append(voter)
            await register(voter, f"cancelvoter{round_no}", f"cancelvoter{round_no}@example.com")
            await voter.hx_post(f"/posts/{race_post_id}/vote", {"value": 1})
            responses = await asyncio.gather(
                voter.hx_post(f"/posts/{race_post_id}/vote", {"value": 1}),
                voter.hx_post(f"/posts/{race_post_id}/vote", {"value": 1}),
            )
            pscore = await scalar("SELECT score FROM posts WHERE id = :id", id=race_post_id)
            total = await vote_total("post", race_post_id)
            if not (all(x.status_code == 200 for x in responses) and pscore == total):
                cancel_ok = False
                cancel_detail = f"round={round_no} statuses={[x.status_code for x in responses]} score={pscore} total={total}"
        check("残件A: 投票の同時取り消しでもスコアと票の合計が一致する", cancel_ok, cancel_detail)

        r = await racer.form_post("/r/racetopic/submit", {"title": "Race Comment", "post_type": "text", "body": "race", "url": ""})
        race_comment_post = r.headers.get("location", "").rstrip("/").split("/")[-1]
        await racer.form_post(f"/posts/{race_comment_post}/comments", {"body": "race comment", "parent_id": ""})
        race_comment_id = str(await scalar("SELECT id FROM comments WHERE post_id = :id", id=race_comment_post))
        cvoter = Client(); extra_clients.append(cvoter)
        await register(cvoter, "commentracer", "commentracer@example.com")
        await cvoter.hx_post(f"/comments/{race_comment_id}/vote", {"value": 1})
        responses = await asyncio.gather(
            cvoter.hx_post(f"/comments/{race_comment_id}/vote", {"value": -1}),
            cvoter.hx_post(f"/comments/{race_comment_id}/vote", {"value": -1}),
        )
        cscore = await scalar("SELECT score FROM comments WHERE id = :id", id=race_comment_id)
        ctotal = await vote_total("comment", race_comment_id)
        check("残件A: コメント投票の同時変更でもスコアと票の合計が一致する", all(x.status_code == 200 for x in responses) and cscore == ctotal, f"statuses={[x.status_code for x in responses]} score={cscore} total={ctotal}")

        sc_a = Client(); extra_clients.append(sc_a)
        sc_b = Client(); extra_clients.append(sc_b)
        await register(sc_a, "subrace_a", "subrace_a@example.com")
        await register(sc_b, "subrace_b", "subrace_b@example.com")
        await sc_a.get("/subreddits/new")
        await sc_b.get("/subreddits/new")
        responses = await asyncio.gather(
            sc_a.form_post("/subreddits/new", {"name": "RaceSub", "description": "race"}),
            sc_b.form_post("/subreddits/new", {"name": "RaceSub", "description": "race"}),
        )
        statuses = sorted(x.status_code for x in responses)
        check("残件B: Subreddit の同時作成で 500 が出ず、片方は重複エラー (422) になる", statuses == [303, 422], f"statuses={statuses}")
        race_sub_count = await scalar("SELECT count(*) FROM subreddits WHERE lower(name) = 'racesub'")
        check("残件B: 同時作成でも Subreddit は1つだけ作られる", race_sub_count == 1, f"count={race_sub_count}")

        joiner = Client(); extra_clients.append(joiner)
        await register(joiner, "joinracer", "joinracer@example.com")
        responses = await asyncio.gather(
            joiner.hx_post("/r/RaceSub/join", {}),
            joiner.hx_post("/r/RaceSub/join", {}),
        )
        member_count = await scalar(
            "SELECT count(*) FROM subreddit_members m JOIN users u ON u.id = m.user_id JOIN subreddits s ON s.id = m.subreddit_id WHERE u.username = 'joinracer' AND lower(s.name) = 'racesub'")
        check("残件B: Join の二重クリックで 500 にならない", all(x.status_code == 200 for x in responses), f"statuses={[x.status_code for x in responses]}")
        check("残件B: Join の二重クリックでも参加レコードは1つだけ", member_count == 1, f"members={member_count}")

        url_2048 = "https://example.com/" + "a" * (2048 - len("https://example.com/"))
        r = await racer.form_post("/r/racetopic/submit", {"title": "URL 2048", "post_type": "link", "body": "", "url": url_2048})
        check("残件C: URL 2048 文字のリンク投稿は作成できる (303)", r.status_code == 303, f"got {r.status_code}")
        r = await racer.form_post("/r/racetopic/submit", {"title": "URL 2049", "post_type": "link", "body": "", "url": url_2048 + "a"})
        check("残件C: URL 2049 文字は DB エラーではなく 422 で返る", r.status_code == 422, f"got {r.status_code}")

        r = await racer.form_post("/r/racetopic/submit", {"title": "Delete Redirect", "post_type": "text", "body": "redirect", "url": ""})
        del_post_location = r.headers.get("location", "")
        del_post_id = del_post_location.rstrip("/").split("/")[-1]
        await racer.form_post(f"/posts/{del_post_id}/comments", {"body": "to be deleted", "parent_id": ""})
        del_comment_id = str(await scalar("SELECT id FROM comments WHERE post_id = :id", id=del_post_id))
        r = await racer.form_post(f"/comments/{del_comment_id}/delete", {})
        check("残件C: コメント削除後のリダイレクト先が投稿ページになっている", r.status_code == 303 and r.headers.get("location") == f"/r/racetopic/{del_post_id}", f"got {r.status_code} location={r.headers.get('location')}")

        await carol.form_post("/subreddits/new", {"name": "paging", "description": "paging test"})
        carol_id = str(await scalar("SELECT id FROM users WHERE lower(username) = 'carol'"))
        paging_id = str(await scalar("SELECT id FROM subreddits WHERE name = 'paging'"))
        async with engine.begin() as conn:
            for i in range(1, 26):
                await conn.execute(
                    text("INSERT INTO posts (title, type, body, author_id, subreddit_id, hot_score) VALUES (:t, 'text', 'paging', :a, :s, 0.0)"),
                    {"t": f"Paging Post {i:02d}", "a": carol_id, "s": paging_id})
        r = await carol.get("/r/paging?sort=new")
        check("レビュー3: ちょうど25件では次ページなしと判定する", r.status_code == 200 and r.text.count("Paging Post") == 25 and "もっと見る" not in r.text, f"got {r.status_code}, count={r.text.count('Paging Post')}")
        async with engine.begin() as conn:
            await conn.execute(
                text("INSERT INTO posts (title, type, body, author_id, subreddit_id, hot_score) VALUES ('Paging Post 26', 'text', 'paging', :a, :s, 0.0)"),
                {"a": carol_id, "s": paging_id})
        r = await carol.get("/r/paging?sort=new")
        check("レビュー3: 26件あると次ページありと判定する", r.status_code == 200 and "もっと見る" in r.text and "page=2" in r.text, f"got {r.status_code}")
        r = await carol.get("/r/paging/posts?sort=new&page=2")
        check("レビュー3: 2ページ目は残り1件だけで次ページなし", r.status_code == 200 and r.text.count("Paging Post") == 1 and "もっと見る" not in r.text, f"got {r.status_code}, count={r.text.count('Paging Post')}")
    finally:
        await alice.close()
        if bob is not None:
            await bob.close()
        for c in extra_clients:
            await c.close()
        await engine.dispose()

    failed = [name for name, ok in RESULTS if not ok]
    print(f"\n==== E2E (Go server, PostgreSQL): {len(RESULTS) - len(failed)}/{len(RESULTS)} passed ====")
    if failed:
        print("FAILED:", failed)


if __name__ == "__main__":
    asyncio.run(main())
