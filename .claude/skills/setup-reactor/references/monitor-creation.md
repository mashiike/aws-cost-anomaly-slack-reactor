# Cost Anomaly Monitor を新規に作る場合

F1 で「Monitor が既にある」場合はこのドキュメントは不要。**Monitor が無く、作成サポートを希望する場合のみ**読む。

このリポジトリの主目的は「既存 Monitor の通知を Slack に流すこと」であり、Monitor 自体の運用設計はスコープ外。ここでは「Slack 通知を試すために最低限の Monitor を立てる」場合の構成例だけを示す。**何を監視軸にするか (dimension の選択) は End User の運用判断**であり、このガイドは推奨値を提示しない。

最新の Cost Anomaly Detection リソース仕様は AWS docs および Terraform Registry を参照すること:
- AWS Cost Anomaly Detection: https://docs.aws.amazon.com/cost-management/latest/userguide/getting-started-ad.html
- Terraform `aws_ce_anomaly_monitor`: Terraform Registry を確認

## 最小構成 (Terraform 例)

```hcl
resource "aws_ce_anomaly_monitor" "example" {
  name              = "<好きな名前>"   # 命名規則は End User のプロジェクトに合わせる
  monitor_type      = "DIMENSIONAL"   # or "CUSTOM" で LinkedAccount 等を指定
  monitor_dimension = "SERVICE"        # 何を見るかは運用判断
}
```

`monitor_type = "CUSTOM"` を使うと特定の LinkedAccount や Service / Tag に絞った Monitor を作れる。詳細は Terraform Registry の `aws_ce_anomaly_monitor` ドキュメントで `monitor_specification` の最新書式を確認。

## Alert Subscription はこの段階では作らない

このフェーズで Alert Subscription を作ってしまうと、Slack 側の疎通確認 (F5) より前に本物の Anomaly 通知が走り出してしまう。**Alert Subscription を作るのは F6 (Monitor 接続フェーズ) で、F5 の疎通確認に成功した後**。

## Region

Cost Anomaly Detection の API endpoint は `us-east-1` だが、SNS 通知は **同じ Region の SNS Topic** にしか届かないという制約がある (最新仕様は AWS docs を確認すること)。SNS Topic / Lambda / SSM はすべて同じ Region に揃える前提で構築する。
