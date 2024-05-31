# Changelog

## [v0.6.2](https://github.com/mashiike/aws-cost-anomaly-slack-reactor/compare/v0.6.1...v0.6.2) - 2024-05-31
- Adjust API calls to avoid errors when crossing months by @mashiike in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/55

## [v0.6.1](https://github.com/mashiike/aws-cost-anomaly-slack-reactor/compare/v0.6.0...v0.6.1) - 2024-05-28
- ddb table ttl is eporch , not string by @mashiike in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/53

## [v0.6.0](https://github.com/mashiike/aws-cost-anomaly-slack-reactor/compare/v0.5.0...v0.6.0) - 2024-05-28
- Handle Multiple SNS Notifications for the Same AnomalyID in AWS Cost Anomaly Detector by @mashiike in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/50
- Bump github.com/handlename/ssmwrap from 1.2.1 to 2.1.0 by @mashiike in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/52

## [v0.5.0](https://github.com/mashiike/aws-cost-anomaly-slack-reactor/compare/v0.4.0...v0.5.0) - 2024-05-27
- Bump the aws-sdk-go-v2 group with 4 updates by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/39
- Enhance Anomaly Detection by Visualizing Cost Trends Excluding Savings Plan for Specific Services by @mashiike in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/46
- Bump github.com/slack-go/slack from 0.12.5 to 0.13.0 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/45
- Bump github.com/fatih/color from 1.16.0 to 1.17.0 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/41
- Bump the aws-sdk-go-v2 group with 5 updates by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/43

## [v0.4.0](https://github.com/mashiike/aws-cost-anomaly-slack-reactor/compare/v0.3.0...v0.4.0) - 2024-05-12
- Bump the aws-sdk-go-v2 group with 4 updates by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/31
- Bump github.com/aws/aws-lambda-go from 1.46.0 to 1.47.0 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/34
- Bump golang.org/x/net from 0.22.0 to 0.23.0 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/33
- add info to log. for debug user name and team by @mashiike in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/36
- modify workflow for manual release by @mashiike in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/35
- Bump github.com/aws/aws-sdk-go-v2/service/costexplorer from 1.37.1 to 1.38.0 in the aws-sdk-go-v2 group by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/37
- RouteCase Linked Account not set, Graph generate with Group By Linked Account by @mashiike in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/38

## [v0.3.0](https://github.com/mashiike/aws-cost-anomaly-slack-reactor/compare/v0.2.0...v0.3.0) - 2024-04-17
- Bump github.com/stretchr/testify from 1.8.4 to 1.9.0 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/8
- switch to UploadFileV2 API by @fujiwara in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/28
- add credits on tagpr by @mashiike in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/29
- group update aws-sdk-go-v2 by @mashiike in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/30
- Bump github.com/aws/aws-sdk-go-v2 from 1.26.0 to 1.26.1 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/24
- Bump github.com/aws/aws-sdk-go-v2/service/costexplorer from 1.37.0 to 1.37.1 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/25
- Bump github.com/aws/aws-sdk-go-v2/config from 1.27.9 to 1.27.11 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/27
- Bump github.com/aws/aws-sdk-go-v2/service/sns from 1.29.3 to 1.29.4 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/23

## [v0.2.0](https://github.com/mashiike/aws-cost-anomaly-slack-reactor/compare/v0.1.0...v0.2.0) - 2024-03-28
- Fix:ValidationException: end date past the beginning of next month by @mashiike in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/18
- Bump github.com/aws/aws-sdk-go-v2/service/costexplorer from 1.34.0 to 1.36.2 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/16
- Bump github.com/fujiwara/ridge from 0.7.0 to 0.9.0 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/14
- error report on postAnomalyDetectedMessage by @mashiike in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/20

## [v0.1.0](https://github.com/mashiike/aws-cost-anomaly-slack-reactor/commits/v0.1.0) - 2024-03-04
- Bump github.com/slack-go/slack from 0.12.3 to 0.12.5 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/5
- Bump github.com/mashiike/canyon from 0.7.0 to 0.7.1 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/4
- Bump github.com/aws/aws-sdk-go-v2/config from 1.27.0 to 1.27.4 by @dependabot in https://github.com/mashiike/aws-cost-anomaly-slack-reactor/pull/1
