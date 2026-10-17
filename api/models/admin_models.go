package models

import "github.com/Annany2002/nebula-backend/internal/domain"

// AdminPagination describes one snapshot of an administrator metadata list.
type AdminPagination struct {
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

// AdminUserListResponse contains only explicitly selected account metadata.
type AdminUserListResponse struct {
	Users      []domain.AdminUser `json:"users"`
	Pagination AdminPagination    `json:"pagination"`
}

// AdminAuditListResponse exposes only persisted account transition metadata.
type AdminAuditListResponse struct {
	Events     []domain.AdminAuditEvent `json:"events"`
	Pagination AdminPagination          `json:"pagination"`
}
