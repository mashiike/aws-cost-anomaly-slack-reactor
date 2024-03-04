# aws-cost-anomaly-slack-reactor

これは、AWSのコスト異常検知に反応するSlack Botです。

## Usage

[_examples](./_examples)にサンプルのTerraformコードがあります。
基本的にはSQSを１つ作成して、LambdaをDeployするのが1st Stepです。

その後、AWSコスト異常検知の通知先に設定しているSNSトピックのサブスクリプションとSlackの設定を行います。

### Slackの設定。

以下のようなマニュフェストのSlackAppを用意し、Slackにインストールします。

```yaml
display_information:
  name: aws-cost-anomaly-slack-reactor
  description: AWS Cost Anomaly Detection BOT
  background_color: "#346947"
features:
  app_home:
    home_tab_enabled: true
    messages_tab_enabled: false
    messages_tab_read_only_enabled: false
  bot_user:
    display_name: AWS Cost Anomaly Detection
    always_online: true
oauth_config:
  scopes:
    bot:
      - app_mentions:read
      - chat:write
      - files:write
settings:
  event_subscriptions:
    request_url: https://<deployしたLambdaのLambda Function URL>/slack/events
    bot_events:
      - app_mention
  interactivity:
    is_enabled: true
    request_url: https://<deployしたLambdaのLambda Function URL>/slack/events
  org_deploy_enabled: false
  socket_mode_enabled: false
  token_rotation_enabled: false
```

その後、SlackのBOT_TOKENや、SINGING_SECRETを設定してLambdaを再デプロイします。
再デプロイ後に、EventSubscriptionのVerificationを実行してください。

### SNSの設定。

SNSは ` https://<deployしたLambdaのLambda Function URL>/amazon-sns` にHTTPSの配信設定をしてください。
