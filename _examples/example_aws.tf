
resource "aws_iam_role" "reactor" {
  name = "aws-cost-anomaly-slack-reactor"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Sid    = ""
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })
}

resource "aws_iam_policy" "reactor" {
  name   = "aws-cost-anomaly-slack-reactor"
  path   = "/"
  policy = data.aws_iam_policy_document.reactor.json
}

resource "aws_cloudwatch_log_group" "reactor" {
  name              = "/aws/lambda/aws-cost-anomaly-slack-reactor"
  retention_in_days = 7
}

resource "aws_iam_role_policy_attachment" "reactor" {
  role       = aws_iam_role.reactor.name
  policy_arn = aws_iam_policy.reactor.arn
}

data "aws_iam_policy_document" "reactor" {
  statement {
    actions = [
      "sqs:DeleteMessage",
      "sqs:GetQueueUrl",
      "sqs:ChangeMessageVisibility",
      "sqs:ReceiveMessage",
      "sqs:SendMessage",
      "sqs:GetQueueAttributes",
    ]
    resources = [aws_sqs_queue.reactor.arn]
  }
  statement {
    actions = [
      "ssm:GetParameter*",
      "ssm:DescribeParameters",
      "ssm:List*",
    ]
    resources = ["*"]
  }
  statement {
    actions = [
      "logs:GetLog*",
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
      "logs:GetQueryResults",
      "logs:StartQuery",
      "logs:StopQuery",
    ]
    resources = ["*"]
  }
  statement {
    actions = [
      "ce:ProvideAnomalyFeedback",
      "ce:GetCostAndUsage",
    ]
    resources = ["*"]
  }
  statement {
    actions = [
      "dynamodb:PutItem",
      "dynamodb:GetItem",
      "dynamodb:CreateTable",
      "dynamodb:DescribeTable",
      "dynamodb:DescribeTimeToLive",
      "dynamodb:UpdateTimeToLive",
    ]
    resources = ["*"]
  }
}

resource "aws_sqs_queue" "reactor" {
  name                       = "aws-cost-anomaly-slack-reactor"
  message_retention_seconds  = 86400
  visibility_timeout_seconds = 30
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.reactor-dlq.arn
    maxReceiveCount     = 3
  })
}

resource "aws_sqs_queue" "reactor-dlq" {
  name                      = "aws-cost-anomaly-slack-reactor-dlq"
  message_retention_seconds = 345600
}

data "archive_file" "reactor_dummy" {
  type        = "zip"
  output_path = "${path.module}/reactor_dummy.zip"
  source {
    content  = "reactor_dummy"
    filename = "bootstrap"
  }
  depends_on = [
    null_resource.reactor_dummy
  ]
}

resource "null_resource" "reactor_dummy" {}

resource "aws_lambda_function" "reactor" {
  lifecycle {
    ignore_changes = all
  }

  function_name = "aws-cost-anomaly-slack-reactor"
  role          = aws_iam_role.reactor.arn
  architectures = ["arm64"]
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  filename      = data.archive_file.reactor_dummy.output_path
}

resource "aws_lambda_alias" "reactor" {
  lifecycle {
    ignore_changes = all
  }
  name             = "current"
  function_name    = aws_lambda_function.reactor.arn
  function_version = aws_lambda_function.reactor.version
}

resource "aws_lambda_event_source_mapping" "reactor_invoke_from_sqs" {
  batch_size       = 1
  event_source_arn = aws_sqs_queue.reactor.arn
  enabled          = true
  function_name    = aws_lambda_alias.reactor.arn
}

resource "aws_ssm_parameter" "slack_bot_token" {
  name        = "/cost-anomaly-slack-reactor/SLACK_BOT_TOKEN"
  description = "Slack bot token for aws-cost-anomaly-slack-reactor"
  type        = "SecureString"
  value       = local.slack_bot_token
}

resource "aws_ssm_parameter" "slack_signing_secret" {
  name        = "/cost-anomaly-slack-reactor/SLACK_SIGNING_SECRET"
  description = "SLACK_SIGNING_SECRET for aws-cost-anomaly-slack-reactor"
  type        = "SecureString"
  value       = local.slack_signing_secret
}

resource "aws_ssm_parameter" "slack_channel" {
  name        = "/cost-anomaly-slack-reactor/SLACK_CHANNEL"
  description = "SLACK_CHANNEL for aws-cost-anomaly-slack-reactor"
  type        = "String"
  value       = local.slack_channel
}
