{
	"blocks": [
    {
			"type": "section",
			"text": {
				"type": "mrkdwn",
				"text": ":mega: *<https://console.aws.amazon.com/cost-management/home#/anomaly-detection/monitors/{{ .MonitorID }}/anomalies/{{ .AnomalyID }}|AWS Cost Anomaly Detected | Account: {{ .AccountID }}| {{ .AnomalyTimeRange }} >*"
			}
		},
		{
			"type": "divider"
		},
		{
			"type": "section",
			"text": {
				"type": "mrkdwn",
				"text": "コスト異常を検知しました。\n\n- Start Date: {{ .AnomalyStartDate }}\n- End Date: {{ .AnomalyEndDate }}\n-  Total Impact: ${{ .Impact.TotalImpact }} \n"
      }
		},
    {{ range $i, $v := .RootCauses }}
		{
			"type": "divider"
		},
    {
			"type": "section",
			"text": {
				"type": "mrkdwn",
				"text": "根本原因 #{{ $i }}\n\n- Service: {{ $v.Service }}\n- Account: {{ $v.LinkedAccount }}\n - AccountName: {{ $v.LinkedAccountName }}\n- Region: {{ $v.Region }}\n-  UsageType: ${{ $v.UsageType }} \n"
      }
		},
    {{ end }}
		{
			"type": "divider"
		},
		{
			"type": "actions",
      "block_id": "{{ .ActionsBlockID }}",
			"elements": [
				{
					"type": "button",
					"text": {
						"type": "plain_text",
						"text": "正確な異常",
						"emoji": false
					},
					"value": "{{ .ActionsYesValue }}",
					"action_id": "{{ .ActionsYesID }}"
				},
				{
					"type": "button",
					"text": {
						"type": "plain_text",
						"text": "誤検出",
						"emoji": false
					},
					"value": "{{ .ActionsNoValue }}",
					"action_id": "{{ .ActionsNoID }}"
				},
				{
					"type": "button",
					"text": {
						"type": "plain_text",
						"text": "問題ではありません",
						"emoji": false
					},
					"value": "{{ .ActionsPlanedActivityValue }}",
					"action_id": "{{ .ActionsPlanedActivityID }}"
				}
			]
		}
	]
}
