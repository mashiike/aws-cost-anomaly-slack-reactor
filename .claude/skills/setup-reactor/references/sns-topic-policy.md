# SNS Topic Policy: costalerts.amazonaws.com の許可

AWS Cost Anomaly Detection が SNS Topic に Publish できるよう、Topic Policy で `costalerts.amazonaws.com` を許可する必要がある。

**最新の正確な仕様は AWS 公式 docs を一次資料として確認すること**:
- AWS Cost Anomaly Detection の SNS 通知設定: https://docs.aws.amazon.com/cost-management/latest/userguide/ad-SNS.html

時間で書式や推奨条件が変わる可能性がある。下記サンプルはあくまで現時点での基本形であり、確定的なものとして扱わない。

## サンプル (基本形)

`aws_sns_topic_policy` を使う場合の JSON サンプル。`<TOPIC_ARN>` と `<ACCOUNT_ID>` は環境に応じて差し替える。

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AllowCostAnomalyDetectionPublish",
      "Effect": "Allow",
      "Principal": {
        "Service": "costalerts.amazonaws.com"
      },
      "Action": "SNS:Publish",
      "Resource": "<TOPIC_ARN>",
      "Condition": {
        "StringEquals": {
          "aws:SourceAccount": "<ACCOUNT_ID>"
        }
      }
    }
  ]
}
```

## ポイント

- **`aws:SourceAccount` 条件は付ける**。付けないと他アカウントからの偽装 Publish の余地が残る (confused deputy 対策)
- Topic の暗号化に KMS を使う場合、KMS Key Policy 側でも `costalerts.amazonaws.com` に `kms:GenerateDataKey` / `kms:Decrypt` を許可する必要がある (こちらも `aws:SourceAccount` 条件を付ける)。詳細は AWS docs 参照
- Region は F1 で確認した Monitor の Region と揃える。Cross-Region Subscription はできない (最新仕様は AWS docs 確認)

## Terraform で書く場合のヒント

- `data "aws_iam_policy_document"` で組み立てて `aws_sns_topic_policy.policy` に渡すと、JSON 直書きより読みやすい
- Lambda の HTTPS Subscription は `aws_sns_topic_subscription` リソースで `protocol = "https"`, `endpoint = "<Function URL>amazon-sns"` を指定。Subscription Confirmation の自動応答は Lambda 側のコードが行う

Terraform リソースの最新仕様は Terraform Registry を参照。
