# Slack App 準備

**完全な manifest YAML はここに固定で書かない**。Slack App manifest は時間でフィールドが増減するため (例: トークン rotation や OAuth 仕様の追加)、Skill 内に貼り付けると古い指示を出す原因になる。

**一次資料を必ず確認すること**:
- OSS の `README.md` 内の manifest サンプル (このリポジトリのルート `README.md`)
- Slack API 公式 docs (App manifest reference): https://docs.slack.dev/reference/manifests/

OSS の README 自体が古いケースもありうるので、**最新の Slack 仕様で必須フィールドが増えていないか** Slack API docs 側を必ず突き合わせる。

## 本質的に必要な構成 (時間で変わりにくい部分)

### Bot Scopes

- `app_mentions:read` — `app_mention` イベントを受け取るために必要
- `chat:write` — Slack チャンネルにメッセージを投稿するために必要
- `files:write` — 添付ファイルを伴う通知を投稿する場合に必要

### Event Subscription

- 有効化する
- Bot Event として `app_mention` を購読
- Request URL は **`<Lambda Function URL>/slack/events`** (Lambda deploy 後に確定するため、最初は仮の値で App を作成し、後で書き換える)

### Interactivity

- 有効化する
- Request URL は Event Subscription と同じ `<Lambda Function URL>/slack/events`

### Socket Mode

- 無効 (このボットは HTTP ベースで動作する)

## 手順の流れ

1. Slack App を新規作成 (or 既存 App を編集)。manifest を貼る場合は **OSS の README** にあるサンプルを土台にし、Slack API docs で必須フィールドの過不足を確認する
2. App を workspace にインストールし、**Bot Token** (`xoxb-...`) を控える
3. **Signing Secret** (Basic Information 画面) を控える
4. 通知先のチャンネル ID (`C` で始まる文字列) を控える。Bot をそのチャンネルに招待しておく
5. Bot Token / Signing Secret / Channel ID は SSM の対応パラメータに投入する (F3 で作成済みのもの)

## Verification (Lambda 側との接続確認)

- Slack App の Event Subscription 設定画面で Request URL の Verification (URL 検証) ボタンを押す
- これが成功するためには、**SSM に実値の Signing Secret が入っており、その状態で Lambda が deploy されている必要がある** (Lambda 起動時に SSM から値を読む構成のため)
- 初回 deploy 時は dummy のまま Verification を試して失敗 → SSM に実値を入れ → Lambda 再 deploy → Verification 再実行、という順序になる

## 補足: token rotation / PKCE 等の新しい設定

Slack 側で `token_rotation_enabled` や PKCE 関連の設定項目が増えている場合がある。**このガイドは追従しない方針**なので、必要なら Slack API docs 側で要否を判断する。基本的には rotation 無効・PKCE 不要で動く前提だが、Slack のポリシー変更に応じて変わる可能性はある。
