package reactor

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnomalyMarshalJSON(t *testing.T) {
	bs, err := os.ReadFile("testdata/anomaly.json")
	require.NoError(t, err)
	var a Anomaly
	err = json.Unmarshal(bs, &a)
	require.NoError(t, err)
	require.NotEmpty(t, a)
	require.Equal(t, "https://console.aws.amazon.com/cost-management/home#/anomaly-detection/monitors/abcdef12-1234-4ea0-84cc-918a97d736ef/anomalies/12345678-abcd-ef12-3456-987654321a12", a.AnomalyDetailsLink)
	acutal, err := json.Marshal(a)
	require.NoError(t, err)
	require.JSONEq(t, string(bs), string(acutal))
}
