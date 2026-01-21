// internal/core/query_params.go
package core

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Default and limit constants for pagination
const (
	DefaultLimit = 100
	MaxLimit     = 1000
	DefaultOrder = "asc"
)

// ReservedParams contains query parameter names reserved for pagination, sorting, and field selection.
// These should not be treated as column filters.
var ReservedParams = map[string]bool{
	"limit":  true,
	"offset": true,
	"sort":   true,
	"order":  true,
	"fields": true,
}

// ListQueryOptions holds parsed query parameters for ListRecords
type ListQueryOptions struct {
	// Pagination
	Limit  int
	Offset int

	// Sorting
	SortBy    string
	SortOrder string // "asc" or "desc"

	// Field Selection
	Fields []string // Columns to return (empty = all columns)
}

// ParseListQueryOptions extracts pagination, sorting, and field selection options from query parameters.
// Returns the parsed options and any validation error.
func ParseListQueryOptions(queryParams url.Values) (*ListQueryOptions, error) {
	opts := &ListQueryOptions{
		Limit:     DefaultLimit,
		Offset:    0,
		SortBy:    "",
		SortOrder: DefaultOrder,
		Fields:    nil,
	}

	// Parse limit
	if limitStr := queryParams.Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			return nil, fmt.Errorf("invalid 'limit' parameter: must be an integer")
		}
		if limit < 1 {
			return nil, fmt.Errorf("invalid 'limit' parameter: must be at least 1")
		}
		if limit > MaxLimit {
			return nil, fmt.Errorf("invalid 'limit' parameter: maximum is %d", MaxLimit)
		}
		opts.Limit = limit
	}

	// Parse offset
	if offsetStr := queryParams.Get("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil {
			return nil, fmt.Errorf("invalid 'offset' parameter: must be an integer")
		}
		if offset < 0 {
			return nil, fmt.Errorf("invalid 'offset' parameter: must be non-negative")
		}
		opts.Offset = offset
	}

	// Parse sort column
	if sortBy := queryParams.Get("sort"); sortBy != "" {
		if !IsValidIdentifier(sortBy) {
			return nil, fmt.Errorf("invalid 'sort' parameter: '%s' is not a valid column name", sortBy)
		}
		opts.SortBy = sortBy
	}

	// Parse sort order
	if order := queryParams.Get("order"); order != "" {
		lowerOrder := strings.ToLower(order)
		if lowerOrder != "asc" && lowerOrder != "desc" {
			return nil, fmt.Errorf("invalid 'order' parameter: must be 'asc' or 'desc'")
		}
		opts.SortOrder = lowerOrder
	}

	// Parse fields
	if fieldsStr := queryParams.Get("fields"); fieldsStr != "" {
		fields := strings.Split(fieldsStr, ",")
		validFields := make([]string, 0, len(fields))
		for _, field := range fields {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			if !IsValidIdentifier(field) {
				return nil, fmt.Errorf("invalid 'fields' parameter: '%s' is not a valid column name", field)
			}
			validFields = append(validFields, field)
		}
		if len(validFields) > 0 {
			opts.Fields = validFields
		}
	}

	return opts, nil
}

// IsReservedParam checks if a query parameter name is reserved for pagination/sorting/fields.
func IsReservedParam(key string) bool {
	return ReservedParams[strings.ToLower(key)]
}
