package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigrateSQLiteLogDBAddsFinanceColumnsWithoutRebuildingDecimalSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE logs (
		id integer,
		quota integer DEFAULT 0,
		request_id varchar(64) DEFAULT '',
		other text
	)`).Error)

	require.NoError(t, migrateSQLiteLogDB(db))
	require.NoError(t, migrateSQLiteLogDB(db))

	for _, column := range []string{
		"quota_before_group",
		"quota_after_group_unrounded",
		"channel_revenue_usd",
		"channel_cost_usd",
		"channel_cost_ratio",
		"channel_cost_mode",
	} {
		assert.True(t, db.Migrator().HasColumn(&Log{}, column), column)
	}
}
