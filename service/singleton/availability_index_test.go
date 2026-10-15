package singleton

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"gorm.io/gorm"
)

func TestAvailabilityWindowIndexQueryPlan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	db, err := OpenDBFromPath(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := CloseDB(db); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	node := bytes.Repeat([]byte{7}, 16)
	const rows = 6000
	bucket := int64(30 * time.Second)
	start := time.Now().Add(-48*time.Hour).UnixNano() / bucket * bucket
	if err := db.Transaction(func(tx *gorm.DB) error {
		batch := make([]model.AvailabilityBucket, 0, 200)
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			if err := tx.Create(&batch).Error; err != nil {
				return err
			}
			batch = batch[:0]
			return nil
		}
		for i := 0; i < rows; i++ {
			at := start + int64(i)*bucket
			batch = append(batch, model.AvailabilityBucket{
				NodeUUID: node, BucketStart: at, WindowEnd: at + bucket,
				Resolution: model.AvailabilityResolutionRaw, HostState: model.HostStateOnline,
				ConnectivityState: model.ConnectivityFull, Revision: 1,
			})
			if len(batch) == 200 {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		if err := flush(); err != nil {
			return err
		}
		spanStart := start - int64(4*time.Hour)
		for i := 0; i < 4; i++ {
			at := spanStart + int64(i)*int64(time.Hour)
			if err := tx.Create(&model.AvailabilityBucket{
				NodeUUID: node, BucketStart: at, WindowEnd: at + int64(time.Hour),
				Resolution: model.AvailabilityResolutionSpan, HostState: model.HostStateOnline,
				ConnectivityState: model.ConnectivityFull, Revision: 1, Finalized: true,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	now := start + int64(rows)*bucket
	queries := []func(tx *gorm.DB) *gorm.DB{
		func(tx *gorm.DB) *gorm.DB {
			var row model.AvailabilityBucket
			return tx.Where("node_uuid = ? AND window_end <= ?", node, now).
				Order(model.ClosedAvailabilityOrder).Take(&row)
		},
		func(tx *gorm.DB) *gorm.DB {
			var found []model.AvailabilityBucket
			return tx.Where("node_uuid = ? AND window_end <= ?", node, now).
				Order(model.ClosedAvailabilityOrder).Limit(12).Find(&found)
		},
		func(tx *gorm.DB) *gorm.DB {
			var covering model.AvailabilityBucket
			return tx.Where("node_uuid = ? AND resolution = ? AND window_end > ?",
				node, model.AvailabilityResolutionSpan, start+bucket).
				Order("window_end ASC").Take(&covering)
		},
	}
	for _, query := range queries {
		sqlText := db.ToSQL(query)
		assertWindowIndexPlan(t, db, sqlText)
	}
}

func assertWindowIndexPlan(t *testing.T, db *gorm.DB, sqlText string) {
	t.Helper()
	rows, err := db.Raw("EXPLAIN QUERY PLAN " + sqlText).Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	details, err := queryPlanDetails(rows)
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(details, "\n")
	if !strings.Contains(plan, "idx_availability_node_window") {
		t.Fatalf("query plan missing idx_availability_node_window:\n%s\n%s", sqlText, plan)
	}
	if strings.Contains(plan, "USE TEMP B-TREE") {
		t.Fatalf("query plan sorts:\n%s\n%s", sqlText, plan)
	}
}

func queryPlanDetails(rows *sql.Rows) ([]string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var details []string
	for rows.Next() {
		values := make([]sql.RawBytes, len(cols))
		dest := make([]any, len(cols))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		details = append(details, string(values[len(values)-1]))
	}
	return details, rows.Err()
}
