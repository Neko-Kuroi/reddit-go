// Package reports is the report service layer, mirroring the Python
// version's app/reports/services.py.
package reports

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ValidReasons are the accepted report reasons, in display order.
var ValidReasons = []string{
	"spam", "harassment", "hate_speech", "misinformation", "other",
}

// ReasonLabels maps each reason to its Japanese label.
var ReasonLabels = map[string]string{
	"spam":           "スパム",
	"harassment":     "嫌がらせ・ハラスメント",
	"hate_speech":    "ヘイトスピーチ",
	"misinformation": "誤情報",
	"other":          "その他",
}

func validReason(reason string) bool {
	for _, r := range ValidReasons {
		if r == reason {
			return true
		}
	}
	return false
}

// CreateReport files a report on a post or comment. A nonexistent
// target and a report on one's own content are errors.
func CreateReport(ctx context.Context, db *sql.DB,
	reporterID, targetType, targetID, reason string) error {

	if !validReason(reason) {
		return fmt.Errorf("無効な報告理由です: %s", reason)
	}
	if targetType != "post" && targetType != "comment" {
		return errors.New("target_type は 'post' または 'comment' のみ有効です")
	}

	table := "posts"
	if targetType == "comment" {
		table = "comments"
	}
	var authorID string
	err := db.QueryRowContext(ctx,
		`SELECT author_id FROM `+table+` WHERE id = $1`, targetID).Scan(&authorID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("報告対象が見つかりません")
	}
	if err != nil {
		return err
	}
	if authorID == reporterID {
		return errors.New("自分のコンテンツは報告できません")
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO reports (reporter_id, target_type, target_id, reason)
		VALUES ($1, $2, $3, $4)`, reporterID, targetType, targetID, reason)
	return err
}
