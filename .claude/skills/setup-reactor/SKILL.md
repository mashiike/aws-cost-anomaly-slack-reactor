---
name: setup-reactor
description: aws-cost-anomaly-slack-reactor を新規に導入するための対話ガイド。AWS Cost Anomaly Detection の通知を Slack に流したい、このリポジトリ (aws-cost-anomaly-slack-reactor) を導入したい、Cost Anomaly のメール通知を Slack に切り替えたい、SNS + Lambda + Slack の初期構築をしたい、といった要望で起動する。`/setup-reactor` で明示発火。
---

# setup-reactor: aws-cost-anomaly-slack-reactor 初期セットアップ支援

このリポジトリ (`aws-cost-anomaly-slack-reactor`) を新規に導入する End User のための対話ガイド。**既存の AWS Cost Anomaly Detection Monitor のアラートを Slack に届ける状態**まで誘導する。

このガイドは「何を作るか・どこで詰まりやすいか」の知見を伝える役割に専念する。実行戦略 (どこまで自動でやるか・どこで承認を取るか) は End User の Claude Code 環境側の責務であり、このガイドでは規定しない。

## 重要な前提

- **End User が能動的に決定するのは「Slack 通知先のチャンネル」のみ**。他の項目 (Region・命名・閾値・Monitor 設定など) は既存環境/既存運用に従う、または環境依存のため End User の選好を尊重する
- **外部仕様 (Slack App manifest, AWS docs, Terraform provider, OSS の最新リリース) は時間で変わる**。各フェーズで「最新仕様は一次資料 (公式 docs / 最新リリース / README) で確認してください」と End User に促すこと
- **Terraform を基本に提示する**。End User が CLI / Console を希望すれば翻訳して提供する
- **OSS の README / `_examples/` と重複する手順はリンクで委ねる**。このガイドは判断・順序・落とし穴に集中する

## 対話フロー (F1 → F6)

順序が重要。**F5 の疎通確認が成功してから F6 の Monitor 接続に進む** — 逆順だと本番の Cost Anomaly 通知が Slack に届かないまま埋もれる事故が起きうる。

---

### F1. 前提確認フェーズ

最初の問い (Skill 起動直後にこれを聞く):

> AWS Cost Anomaly Detection の Monitor は既に設定されていますか？

#### Monitor が既にある場合 (主想定)

- Monitor の **Region と名前** を確認する
- 以降の SNS Topic / Lambda / SSM などはすべてこの Region に揃える (Cost Anomaly Detection の SNS 通知は同一 Region の SNS Topic にしか届かないことに注意。最新仕様は AWS docs を確認)
- 既存 Monitor の閾値・dimension は触らない (既存運用が決めるもの)

#### Monitor が無い場合 (副次対応)

- 「作成サポートを希望しますか？」と確認
- 希望する場合は `references/monitor-creation.md` に従う
- 何を監視軸 (dimension) にするかは End User の運用判断。このガイドは値を提示しない

---

### F2. Slack App 準備フェーズ

#### 新規作成 / 既存流用の確認

> Slack App は新規に作成しますか、既存の App を流用しますか？

#### 新規作成の場合

伝えるのは「何が必要か」だけ。詳細 YAML はこのガイドに固定で持たない (時間で陳腐化するため):

- 必須 bot scopes: `app_mentions:read`, `chat:write`, `files:write`
- Event Subscription (`app_mention`) と Interactivity の有効化
- Request URL には Lambda Function URL を後で設定する (Function URL は F4 以降に確定するため、ここでは「あとで設定する箇所がある」とだけ伝える)
- Bot Token と Signing Secret は控えておく (F3 で SSM に投入する)

**完全な manifest テンプレートは固定で書かない**。最新は OSS の `README.md` または Slack API 公式 docs を一次資料として参照させる。Slack manifest は時間でフィールドが増減するため、Skill 内に貼り付けない方針。詳細は `references/slack-app-setup.md` を参照。

#### Slack チャンネル決定 (このガイドで End User に能動的決定を求める唯一の項目)

> Cost Anomaly 通知を流す Slack チャンネル ID を決めてください。

- channel ID (`C` から始まる ID) を控えておく (F3 で SSM に投入する)
- Bot を当該チャンネルに招待することも忘れない

---

### F3. Reactor 構築フェーズ (IAM / SSM / SQS / DynamoDB)

`_examples/example_aws.tf` が骨格として最も近い。**重複する詳細はそちらに委ね**、このガイドは落とし穴だけ伝える。

詳細は `references/operational-knowledge.md` を参照。要点:

- **SSM Secret は dummy 値 + `lifecycle.ignore_changes = [value]` で投入**し、実値はマネコンで入れる (tfstate に Secret を書かない運用)
- **IAM**: マルチアカウント運用時は `organizations:DescribeAccount` を追加で許可する (`_examples` の最小 IAM には含まれていない)
- **命名**: `_examples` のリソース名は参考値。End User のプロジェクト命名規則に従う

このフェーズの apply 後、SSM パラメータが 3 つ (Bot Token, Signing Secret, Slack Channel) 存在することを確認する。dummy のままで OK。

---

### F4. Lambda デプロイフェーズ

- **bootstrap バイナリは GitHub Releases からダウンロードする**運用を基本案内 (ローカルビルド前提にしない)。最新リリースバージョンは GitHub の Releases ページを毎回確認する
- **lambroll で deploy**。Lambda Function URL も lambroll 側で管理する想定 (Terraform 側は data source で URL を読む構成)
- **Lambda Timeout は 60 秒推奨**。`_examples/function.json` のデフォルトは 5 秒で、初回起動や DynamoDB テーブル自動作成時に不足する場面がある
- Lambda 環境変数のキー (`SQS_QUEUE_NAME`, `SSMWRAP_PATHS`, `DYNAMODB_TABLE_NAME` 等) は `_examples/function.json` を参照
- 初回 deploy 後、**マネコンで SSM の Secret 実値 (dummy → 本物のトークン/シークレット) を入れて Lambda を再デプロイ**する。これを行うと Slack 側の Event Subscription Verification が通る

`_examples/Makefile` の lambroll コマンド例がそのまま使える。

---

### F5. SNS Topic 構築 + 疎通確認フェーズ

**このフェーズが完了するまで F6 (Monitor 接続) には進まない**。

#### SNS Topic を作る

- F1 で確認した Region に作成
- topic policy に `costalerts.amazonaws.com` の `SNS:Publish` を許可する (条件に `aws:SourceAccount` を付ける)。サンプルは `references/sns-topic-policy.md` を参照
- 最新の正確な許可仕様は AWS Cost Anomaly Detection の docs を一次資料で確認

#### SNS → Lambda の HTTPS Subscription

- Endpoint は `<Lambda Function URL>amazon-sns` (URL の末尾スラッシュに注意)
- Subscription の確認 (Confirmation) は Lambda 側のコードが処理するため、Lambda が稼働状態であることが前提

#### 疎通確認 (本フェーズの肝)

- SNS Topic に **ダミーメッセージを publish して Slack に通知が届くこと**を確認する
- 手順は `references/smoke-test.md` を参照
- これが通らないうちに F6 へ進まないこと

---

### F6. Monitor 接続フェーズ (最終ステップ)

疎通確認 OK 後にのみ実行する。

- 既存 Cost Anomaly Monitor に **Alert Subscription を追加**し、subscriber を F5 で作った SNS Topic ARN に向ける
- 閾値 (絶対額 `$` と変動率 `%`) は **既存運用に従う**。このガイドは値を提示しない (環境依存)
- 最新の Alert Subscription 仕様は AWS docs を確認

この設定が完了すると、本物の Cost Anomaly が検知されたタイミングで Slack に通知が届くようになる。

---

## ガイドが扱わないこと (スコープ外)

- 既存 Cost Anomaly Monitor の閾値・dimension 調整助言 (既存運用に従う前提)
- Slack 以外の通知先 (MS Teams, Chime 等) への拡張
- Slack Bot Token の rotation 運用
- 動作モード (自動 / 手動 / 承認フロー) — End User の Claude Code 環境側の責務

## 最新情報の確認が必要な箇所 (各フェーズで End User に促すこと)

| 何 | どこで確認 |
|---|---|
| Slack App manifest の最新フィールド | Slack API 公式 docs / OSS の `README.md` |
| AWS Cost Anomaly Detection の SNS 通知仕様 | AWS docs (cost-management user guide) |
| SNS topic policy の `costalerts.amazonaws.com` 許可形式 | AWS docs |
| このOSS の最新リリースバージョン (bootstrap バイナリ) | GitHub Releases |
| Terraform AWS provider のリソース仕様 | Terraform Registry |

Skill 本体が固定値で持つには陳腐化が早すぎる情報は、上記の一次資料から都度引くこと。

## reference ファイル一覧

- `references/operational-knowledge.md` — SSM dummy 値運用、IAM 追加権限、Lambda Timeout などの実運用知見
- `references/sns-topic-policy.md` — `costalerts.amazonaws.com` を許可する topic policy のサンプル
- `references/slack-app-setup.md` — Slack App 準備の本質的構成 (manifest 詳細は一次資料に委ねる)
- `references/monitor-creation.md` — Monitor を新規に作る場合のリソース構成
- `references/smoke-test.md` — SNS への dummy publish による疎通確認手順
