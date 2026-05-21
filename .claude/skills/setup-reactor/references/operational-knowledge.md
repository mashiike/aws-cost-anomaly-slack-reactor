# 実運用知見

`_examples/example_aws.tf` を骨格に使う場合に、ドキュメントから読み取りづらい運用上の落とし穴をまとめる。リソース仕様の細部は時間で変わるため、**Terraform AWS provider のドキュメント (Terraform Registry) と AWS 公式 docs を一次資料として最新を確認する**こと。

## SSM Secret を tfstate に書かない運用

`SLACK_BOT_TOKEN` と `SLACK_SIGNING_SECRET` は機微情報。Terraform の `value` に実値を書くと tfstate に平文で残る。

これを避けるためによく取るパターン:

- Terraform 側では `value = "dummy"` で投入
- `lifecycle { ignore_changes = [value] }` を付けて、以降の Terraform 実行で上書きされないようにする
- 実値はマネコン (または `aws ssm put-parameter --overwrite`) で書き換える

Slack Channel ID は機微度が低い (`String` 型で投入) ため、通常はそのまま Terraform で管理して問題ない。

最新の Terraform `aws_ssm_parameter` の `lifecycle` 挙動・属性は Terraform Registry を参照。

## IAM: マルチアカウント運用時の追加権限

`_examples/example_aws.tf` の最小 IAM には含まれていないが、Cost Anomaly が他アカウントに紐づく場合や Organizations 配下のアカウント名を取得して通知に出したい場合、以下が必要になることがある:

- `organizations:DescribeAccount`

これは管理アカウント (もしくはそれに準じる権限を持つアカウント) 側で機能する。マルチアカウント運用でないなら不要。

権限不足のときどう動くかは実装側の fallback に依存するため、必要に応じて Lambda の CloudWatch Logs を確認すること。

## Lambda Timeout は 60 秒を推奨

`_examples/function.json` のデフォルトは 5 秒。以下のケースで不足する:

- DynamoDB テーブルを初回自動作成するとき
- Slack API への通信や AWS Cost Explorer (`ce:GetCostAndUsage`) のレスポンスが遅いとき
- コールドスタート + 初回起動が重なったとき

`Timeout: 60` を推奨。lambroll で deploy する場合は `function.json` の `Timeout` を 60 に書き換える。

## DynamoDB テーブルの自動作成

Lambda 側のコードが必要に応じて DynamoDB テーブルを作る挙動になっている。そのため IAM に `dynamodb:CreateTable` `dynamodb:DescribeTable` 等が必要 (`_examples/example_aws.tf` には含まれている)。

ただし「自動作成」に頼ると初回起動が遅くなりタイムアウトを引きやすい。気になるなら Terraform 側で DynamoDB テーブルを明示的に作る選択肢もある (テーブル名は環境変数 `DYNAMODB_TABLE_NAME` と一致させる)。

## lambroll と Terraform の責務分担

- **Terraform**: IAM Role, IAM Policy, SQS (+ DLQ), DynamoDB, SSM パラメータ (dummy 値で), CloudWatch Log Group, Lambda の「箱」(dummy zip で空の Lambda)、Event Source Mapping (SQS → Lambda)
- **lambroll**: Lambda のコード本体 (bootstrap バイナリ) の deploy、Function URL の管理

Function URL を lambroll 側で管理する場合、Terraform 側は `data "aws_lambda_function_url"` でその URL を読み出して SNS Subscription の endpoint に渡す構成にすると、相互の責務が綺麗に分かれる。

最新の lambroll の挙動・オプションは lambroll の README を参照。

## bootstrap バイナリの入手

ローカルビルドではなく GitHub Releases からダウンロードする運用を基本にする (`_examples/Makefile` 参照)。

- 最新リリースバージョンは GitHub Releases ページで毎回確認する。Skill 内に固定で書かない
- arm64 か x86_64 かは Lambda の `Architectures` と揃える (`_examples` は arm64)
