package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

func (c *controller) checkPayload(b binding, result outcome) (map[string]any, error) {
	payload := map[string]any{"name": c.checkName, "external_id": b.externalID(), "status": result.Status, "output": map[string]string{"title": result.Title, "summary": result.Summary}}
	if id := os.Getenv("GITHUB_RUN_ID"); id != "" {
		runID, err := strconv.ParseInt(id, 10, 64)
		if err != nil || runID <= 0 {
			return nil, errors.New("invalid controller GITHUB_RUN_ID")
		}
		payload["details_url"] = fmt.Sprintf("https://github.com/%s/actions/runs/%d", c.repoName, runID)
	} else if b.Run > 0 {
		payload["details_url"] = fmt.Sprintf("https://github.com/%s/actions/runs/%d", c.repoName, b.Run)
	}
	if result.Status == "completed" {
		payload["conclusion"] = result.Conclusion
	}
	return payload, nil
}
