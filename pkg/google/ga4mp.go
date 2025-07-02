package google

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/perfect-panel/server/pkg/logger"
)

type GAClient struct {
	MeasurementID string
	APISecret     string
	HttpClient    *http.Client
}

func NewGAClient() *GAClient {
	logger.Errorf("GA4 %s - %s", os.Getenv("GA4_MEASUREMENT_ID"), os.Getenv("GA4_API_SECRET"))
	return &GAClient{
		MeasurementID: os.Getenv("GA4_MEASUREMENT_ID"),
		APISecret:     os.Getenv("GA4_API_SECRET"),
		HttpClient:    &http.Client{},
	}
}

func (c *GAClient) SendEvent(eventName string, userId any, payload map[string]any) error {
	clientID := uuid.New().String()

	ga4Payload := map[string]any{
		"client_id": clientID,
		"events": []map[string]any{
			{
				"name":   eventName,
				"params": payload,
			},
		},
	}

	// Add user_id if provided
	if userId != nil {
		ga4Payload["user_id"] = fmt.Sprintf("%v", userId)
	}

	url := fmt.Sprintf("https://www.google-analytics.com/mp/collect?measurement_id=%s&api_secret=%s", c.MeasurementID, c.APISecret)

	b, _ := json.Marshal(ga4Payload)
	resp, err := c.HttpClient.Post(url, "application/json", bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		logger.Errorf("GA4 error: %s", resp.Status)
		return fmt.Errorf("GA4 error: %s", resp.Status)
	}
	return nil
}
