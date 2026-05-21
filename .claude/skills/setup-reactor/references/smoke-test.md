# 疎通確認: SNS Topic にダミー Publish して Slack に届くか確認

F5 のクライマックス。**この確認が成功するまで F6 (Monitor 接続) には進まない**。

理由: 本番の Cost Anomaly 通知は頻度が低く、初回検知を待っていると設定の誤りに気づくのが遅れる。事前に「SNS → Lambda → Slack」の経路だけ単独で疎通させておくと、後から本物の Anomaly が来たときに「届かない原因が SNS の許可なのか Slack 側なのか Cost Anomaly 側なのか」を切り分けやすい。

## 事前確認

- Lambda が deploy されており、CloudWatch Logs にエラーが出ていない
- SSM の Slack Bot Token / Signing Secret に **実値** が入っている (dummy のままだと Slack 投稿で失敗する)
- Slack App の Event Subscription Verification が通っている
- 通知先 Slack チャンネルに Bot が招待されている
- SNS Topic から Lambda Function URL への HTTPS Subscription が `Confirmed` 状態になっている (`PendingConfirmation` のままだと届かない)

## ダミー Publish 手順

### AWS CLI で publish する場合

```sh
aws sns publish \
  --topic-arn <SNS_TOPIC_ARN> \
  --region <REGION> \
  --message '<本文文字列>'
```

メッセージ本文は Lambda 側の handler がパースできる形式である必要がある。**実際のフォーマットや受け付け可能なフィールドは時間で変わる可能性がある**ため、最新は OSS の `reactor/` 配下の handler 実装または `README.md` を確認すること。難しければまずは「任意の文字列で publish して Lambda が起動するか (CloudWatch Logs にイベントが出るか)」だけでも確認価値がある。

### マネコンから publish する場合

SNS Topic の画面で "Publish message" ボタン → message body を入力 → publish。AWS CLI と同様。

## 期待結果

- CloudWatch Logs に Lambda の起動ログが出る
- Slack の指定チャンネルに通知メッセージが届く

## 届かないときの切り分け

| 症状 | 確認ポイント |
|---|---|
| Lambda が起動しない | SNS Subscription が `Confirmed` か。endpoint URL に末尾の `amazon-sns` が付いているか。SNS Topic Policy が `costalerts.amazonaws.com` (および疎通確認時はユーザー自身) からの Publish を許可しているか |
| Lambda は起動したが Slack に投げない | CloudWatch Logs を確認。SSM の Token が実値か (dummy のままになっていないか)。Bot がチャンネルに招待されているか。Channel ID が正しいか |
| Slack 側で 401 / signature error | Signing Secret が SSM の実値に置き換わった後、Lambda が再 deploy されているか |
| Lambda がタイムアウトする | Timeout を 60 秒に上げているか (`references/operational-knowledge.md` 参照) |

疎通確認が通ったら F6 (Monitor 接続) に進む。
