package storage

import (
	"database/sql"
	"strings"

	"github.com/mattn/go-sqlite3"
)

func init() {
	sql.Register("nebula_sqlite3", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			// Inspect compiled operations rather than SQL text, including nested statements.
			conn.RegisterAuthorizer(func(operation int, first, second, _ string) int {
				switch operation {
				case sqlite3.SQLITE_ATTACH, sqlite3.SQLITE_DETACH:
					return sqlite3.SQLITE_DENY
				case sqlite3.SQLITE_PRAGMA:
					switch strings.ToLower(first) {
					case "writable_schema", "temp_store_directory", "data_store_directory":
						return sqlite3.SQLITE_DENY
					}
				case sqlite3.SQLITE_FUNCTION:
					switch strings.ToLower(second) {
					case "load_extension", "readfile", "writefile":
						return sqlite3.SQLITE_DENY
					}
				}
				return sqlite3.SQLITE_OK
			})
			return nil
		},
	})
}
