package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Annany2002/nebula-backend/internal/domain"
)

var (
	ErrUnsupportedPrimaryKey = errors.New("record routes require exactly one primary key column")
	ErrInvalidRecordID       = errors.New("invalid record ID for primary key type")
)

func QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func RecordPrimaryKey(ctx context.Context, db *sql.DB, table string) (domain.ColumnInfo, error) {
	columns, err := getColumnInfo(ctx, db, table)
	if err != nil {
		return domain.ColumnInfo{}, err
	}
	if len(columns) == 0 {
		return domain.ColumnInfo{}, ErrTableNotFound
	}
	var primaryKey domain.ColumnInfo
	count := 0
	for _, column := range columns {
		if column.PK > 0 {
			primaryKey = column
			count++
		}
	}
	if count != 1 {
		return domain.ColumnInfo{}, ErrUnsupportedPrimaryKey
	}
	return primaryKey, nil
}

func ParseRecordID(key domain.ColumnInfo, value string) (any, error) {
	columnType := strings.ToUpper(key.Type)
	if strings.Contains(columnType, "INT") {
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: expected an integer", ErrInvalidRecordID)
		}
		return number, nil
	}
	if strings.Contains(columnType, "REAL") || strings.Contains(columnType, "FLOA") || strings.Contains(columnType, "DOUB") {
		number, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, ErrInvalidRecordID
		}
		return number, nil
	}
	if value == "" {
		return nil, ErrInvalidRecordID
	}
	return value, nil
}
