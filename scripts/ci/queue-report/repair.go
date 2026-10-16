package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// These fields only route work to the existing callback lease. Fresh source,
// topology and complete check-history reads authorize every exact-ID repair.
type reportWork struct {
	Head    string `json:"head"`
	Run     int64  `json:"run"`
	Attempt int    `json:"attempt"`
}

func decodeReportWork(raw string) (reportWork, error) {
	var work reportWork
	if len(raw) > 512 {
		return work, errors.New("report repair work exceeds bound")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&work); err != nil {
		return work, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return work, errors.New("report repair requires exactly one routing record")
	}
	if !shaPattern.MatchString(work.Head) || work.Run <= 0 || work.Attempt <= 0 {
		return work, errors.New("invalid report repair routing")
	}
	return work, nil
}

func (c client) replay(ctx context.Context, repo repository, work reportWork) error {
	if !repoPattern.MatchString(c.repo) || repo.ID <= 0 || !strings.EqualFold(repo.FullName, c.repo) || !shaPattern.MatchString(work.Head) || work.Run <= 0 || work.Attempt <= 0 {
		return errors.New("invalid report repair routing or repository")
	}
	var source run
	if err := c.read(ctx, fmt.Sprintf("actions/runs/%d/attempts/%d", work.Run, work.Attempt), &source); err != nil {
		return err
	}
	if source.ID != work.Run || source.Attempt != work.Attempt {
		return errors.New("repair source differs from requested run and attempt lease")
	}
	return c.publishReport(ctx, event{Action: "completed", Repository: repo, Run: source}, &work)
}
