package reactor

import "time"

type Anomaly struct {
	AccountID          string        `json:"accountId"`
	AnomalyDetailsLink string        `json:"anomalyDetailsLink"`
	AnomalyEndDate     time.Time     `json:"anomalyEndDate"`
	AnomalyID          string        `json:"anomalyId"`
	AnomalyScore       AnomalyScore  `json:"anomalyScore"`
	AnomalyStartDate   time.Time     `json:"anomalyStartDate"`
	DimensionalValue   string        `json:"dimensionalValue"`
	Impact             AnomalyImpact `json:"impact"`
	MonitorArn         string        `json:"monitorArn"`
	RootCauses         []RootCause   `json:"rootCauses"`
	SubscriptionID     string        `json:"subscriptionId"`
	SubscriptionName   string        `json:"subscriptionName"`
}

type AnomalyScore struct {
	CurrentScore float64 `json:"currentScore"`
	MaxScore     float64 `json:"maxScore"`
}

type AnomalyImpact struct {
	MaxImpact             int     `json:"maxImpact"`
	TotalActualSpend      int     `json:"totalActualSpend"`
	TotalExpectedSpend    int     `json:"totalExpectedSpend"`
	TotalImpact           int     `json:"totalImpact"`
	TotalImpactPercentage float64 `json:"totalImpactPercentage"`
}

type RootCause struct {
	LinkedAccount     string `json:"linkedAccount"`
	LinkedAccountName string `json:"linkedAccountName"`
	Region            string `json:"region"`
	Service           string `json:"service"`
	UsageType         string `json:"usageType"`
}
