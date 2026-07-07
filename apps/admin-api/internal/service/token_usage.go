package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

const (
	tokenUsageDefaultLimit = 100
	tokenUsageMaxLimit     = 500
)

func (a *Admin) TokenUsageReport(ctx context.Context, q store.TokenUsageQuery) (store.TokenUsageReport, error) {
	nq, err := a.normalizeTokenUsageQuery(q)
	if err != nil {
		return store.TokenUsageReport{}, err
	}
	return a.tokenUsageReport(ctx, nq)
}

func (a *Admin) tokenUsageReport(ctx context.Context, nq store.TokenUsageQuery) (store.TokenUsageReport, error) {
	summary, err := a.store.TokenUsageSummary(ctx, nq)
	if err != nil {
		return store.TokenUsageReport{}, err
	}
	trend, err := a.store.TokenUsageTrend(ctx, nq)
	if err != nil {
		return store.TokenUsageReport{}, err
	}
	users, err := a.store.TokenUsageUsers(ctx, nq)
	if err != nil {
		return store.TokenUsageReport{}, err
	}
	return store.TokenUsageReport{
		From:    nq.From,
		To:      nq.To,
		Bucket:  nq.Bucket,
		Summary: summary,
		Trend:   trend,
		Users:   users,
		Limit:   nq.Limit,
		Offset:  nq.Offset,
	}, nil
}

func (a *Admin) TokenUsageUserReport(ctx context.Context, userID string, q store.TokenUsageQuery) (store.TokenUsageReport, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return store.TokenUsageReport{}, ErrInvalidArg
	}
	q.UserID = userID
	q.Limit = 0
	q.Offset = 0
	nq, err := a.normalizeTokenUsageQuery(q)
	if err != nil {
		return store.TokenUsageReport{}, err
	}
	summary, err := a.store.TokenUsageSummary(ctx, nq)
	if err != nil {
		return store.TokenUsageReport{}, err
	}
	trend, err := a.store.TokenUsageTrend(ctx, nq)
	if err != nil {
		return store.TokenUsageReport{}, err
	}
	return store.TokenUsageReport{
		From:    nq.From,
		To:      nq.To,
		Bucket:  nq.Bucket,
		Summary: summary,
		Trend:   trend,
	}, nil
}

func (a *Admin) ExportTokenUsageXLSX(ctx context.Context, q store.TokenUsageQuery) ([]byte, string, error) {
	nq, err := a.normalizeTokenUsageQuery(q)
	if err != nil {
		return nil, "", err
	}
	nq.Limit = 0
	nq.Offset = 0
	report, err := a.tokenUsageReport(ctx, nq)
	if err != nil {
		return nil, "", err
	}
	data, err := tokenUsageWorkbook(report)
	if err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("cocola-token-usage-%s-%s.xlsx",
		report.From.Format("20060102"),
		report.To.Add(-time.Nanosecond).Format("20060102"),
	)
	return data, name, nil
}

func (a *Admin) normalizeTokenUsageQuery(q store.TokenUsageQuery) (store.TokenUsageQuery, error) {
	now := a.now().UTC()
	if q.To.IsZero() {
		q.To = now
	} else {
		q.To = q.To.UTC()
	}
	if q.From.IsZero() {
		q.From = q.To.AddDate(0, 0, -30)
	} else {
		q.From = q.From.UTC()
	}
	if !q.From.Before(q.To) {
		return store.TokenUsageQuery{}, ErrInvalidArg
	}
	q.UserID = strings.TrimSpace(q.UserID)
	q.Bucket = strings.TrimSpace(strings.ToLower(q.Bucket))
	if q.Bucket == "" || q.Bucket == "auto" {
		if q.To.Sub(q.From) <= 48*time.Hour {
			q.Bucket = "hour"
		} else {
			q.Bucket = "day"
		}
	}
	if q.Bucket != "hour" && q.Bucket != "day" {
		return store.TokenUsageQuery{}, ErrInvalidArg
	}
	if q.Limit <= 0 {
		q.Limit = tokenUsageDefaultLimit
	}
	if q.Limit > tokenUsageMaxLimit {
		q.Limit = tokenUsageMaxLimit
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	return q, nil
}

func tokenUsageWorkbook(report store.TokenUsageReport) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	overview := f.GetSheetName(0)
	if overview == "" {
		overview = "Sheet1"
	}
	if err := f.SetSheetName(overview, "Overview"); err != nil {
		return nil, err
	}
	overview = "Overview"
	setRows := func(sheet string, rows [][]any) error {
		for r, row := range rows {
			for c, value := range row {
				cell, err := excelize.CoordinatesToCellName(c+1, r+1)
				if err != nil {
					return err
				}
				if err := f.SetCellValue(sheet, cell, value); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := setRows(overview, [][]any{
		{"Metric", "Value"},
		{"From", report.From.Format(time.RFC3339)},
		{"To", report.To.Format(time.RFC3339)},
		{"Bucket", report.Bucket},
		{"Calls", report.Summary.Calls},
		{"Users", report.Summary.UserCount},
		{"Prompt Tokens", report.Summary.PromptTokens},
		{"Completion Tokens", report.Summary.CompletionTokens},
		{"Total Tokens", report.Summary.TotalTokens},
		{"Cost USD", report.Summary.CostUSD},
	}); err != nil {
		return nil, err
	}

	if _, err := f.NewSheet("Trend"); err != nil {
		return nil, err
	}
	trendRows := [][]any{{"Bucket Start", "Calls", "Prompt Tokens", "Completion Tokens", "Total Tokens", "Cost USD"}}
	for _, point := range report.Trend {
		trendRows = append(trendRows, []any{
			point.BucketStart.Format(time.RFC3339),
			point.Calls,
			point.PromptTokens,
			point.CompletionTokens,
			point.TotalTokens,
			point.CostUSD,
		})
	}
	if err := setRows("Trend", trendRows); err != nil {
		return nil, err
	}

	if _, err := f.NewSheet("Users"); err != nil {
		return nil, err
	}
	userRows := [][]any{{"Rank", "User ID", "Email", "Username", "Name", "Role", "Enabled", "Known User", "Calls", "Prompt Tokens", "Completion Tokens", "Total Tokens", "Cost USD", "Last Used At"}}
	for i, user := range report.Users {
		lastUsed := ""
		if !user.LastUsedAt.IsZero() {
			lastUsed = user.LastUsedAt.Format(time.RFC3339)
		}
		userRows = append(userRows, []any{
			i + 1,
			user.UserID,
			user.Email,
			user.Username,
			user.Name,
			user.Role,
			user.Enabled,
			user.KnownUser,
			user.Calls,
			user.PromptTokens,
			user.CompletionTokens,
			user.TotalTokens,
			user.CostUSD,
			lastUsed,
		})
	}
	if err := setRows("Users", userRows); err != nil {
		return nil, err
	}

	for _, sheet := range []string{"Overview", "Trend", "Users"} {
		_ = f.SetColWidth(sheet, "A", "N", 18)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return bytes.Clone(buf.Bytes()), nil
}
