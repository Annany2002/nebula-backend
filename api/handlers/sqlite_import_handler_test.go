package handlers_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

type importPart struct {
	field, filename string
	data            []byte
}

func sqliteImportRequest(t *testing.T, parts ...importPart) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, part := range parts {
		var target io.Writer
		var err error
		if part.filename == "" {
			target, err = writer.CreateFormField(part.field)
		} else {
			target, err = writer.CreateFormFile(part.field, part.filename)
		}
		require.NoError(t, err)
		_, err = target.Write(part.data)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	req := httptest.NewRequest("POST", "/api/v1/databases/import/sqlite", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func validImportParts(name string, data []byte) []importPart {
	return []importPart{{field: "db_name", data: []byte(name)}, {field: "file", filename: "../../untrusted.db", data: data}}
}

func snapshotBytes(t *testing.T, db *sql.DB) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.db")
	require.NoError(t, storage.CreateDatabaseSnapshot(t.Context(), db, path))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func assertImportClean(t *testing.T, f *backendFixture, name string) {
	t.Helper()
	_, err := storage.FindDatabasePath(t.Context(), f.meta, "regression-owner", name)
	require.ErrorIs(t, err, storage.ErrDatabaseNotFound)
	_, err = os.Lstat(filepath.Join(f.cfg.MetadataDbDir, "regression-owner", name+".db"))
	require.ErrorIs(t, err, os.ErrNotExist)
	staged, err := filepath.Glob(filepath.Join(f.cfg.MetadataDbDir, ".nebula-import-*"))
	require.NoError(t, err)
	require.Empty(t, staged)
}

func TestSQLiteImportExportRoundTrip(t *testing.T) {
	f := newBackendFixture(t)
	_, source := f.database(t, "source")
	_, err := source.Exec(`PRAGMA wal_autocheckpoint=0;
CREATE TABLE audit(label TEXT);
CREATE TABLE types(id INTEGER PRIMARY KEY AUTOINCREMENT, item_id INTEGER REFERENCES items(id), bytes BLOB, note TEXT, amount REAL);
INSERT INTO types(item_id,bytes,note,amount) VALUES(1,X'00FF', 'before'||char(0)||'after',1.25);
CREATE VIEW item_view AS SELECT id,label FROM items;
CREATE UNIQUE INDEX labels_unique ON items(label);
CREATE TRIGGER track_insert AFTER INSERT ON items BEGIN INSERT INTO audit VALUES(NEW.label); END;
INSERT INTO items VALUES(2,'committed-in-wal');`)
	require.NoError(t, err)
	exported := f.request("GET", "/api/v1/databases/source/export/sqlite", "Bearer "+f.token, "")
	require.Equal(t, http.StatusOK, exported.Code, exported.Body.String())
	data := exported.Body.Bytes()
	req := sqliteImportRequest(t, validImportParts("restored", data)...)
	req.Header.Set("Authorization", "Bearer "+f.token)
	out := httptest.NewRecorder()
	f.router.ServeHTTP(out, req)
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	var response models.ImportSQLiteResponse
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &response))
	require.Equal(t, "restored", response.DBName)
	require.Equal(t, int64(len(data)), response.SizeBytes)
	path, err := storage.FindDatabasePath(t.Context(), f.meta, "regression-owner", "restored")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(f.cfg.MetadataDbDir, "regression-owner", "restored.db"), path)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	db, err := storage.ConnectUserDB(t.Context(), path)
	require.NoError(t, err)
	defer db.Close()
	var blob, note, label string
	var amount float64
	require.NoError(t, db.QueryRow("SELECT hex(bytes),hex(note),amount FROM types WHERE id=1").Scan(&blob, &note, &amount))
	require.Equal(t, "00FF", blob)
	require.Equal(t, "6265666F7265006166746572", note)
	require.Equal(t, 1.25, amount)
	require.NoError(t, db.QueryRow("SELECT label FROM item_view WHERE id=2").Scan(&label))
	require.Equal(t, "committed-in-wal", label)
	_, err = db.Exec("INSERT INTO items VALUES(3,'committed-in-wal')")
	require.Error(t, err, "unique index must survive import")
	_, err = db.Exec("INSERT INTO items VALUES(3,'after-import'); INSERT INTO types(item_id) VALUES(3);")
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM audit").Scan(&count))
	require.Equal(t, 2, count, "validation must not fire triggers; later writes must")
	require.NoError(t, db.QueryRow("SELECT MAX(id) FROM types").Scan(&count))
	require.Equal(t, 2, count, "autoincrement sequence must survive")
	_, err = db.Exec("INSERT INTO types(item_id) VALUES(999)")
	require.Error(t, err, "foreign key enforcement must survive import")
	// Import only creates a database registration; key generation is a separate operation.
	var keys int
	require.NoError(t, f.meta.QueryRow("SELECT count(*) FROM api_keys").Scan(&keys))
	require.Zero(t, keys)
	staged, err := filepath.Glob(filepath.Join(f.cfg.MetadataDbDir, ".nebula-import-*"))
	require.NoError(t, err)
	require.Empty(t, staged)
}

func TestSQLiteImportRejectsMalformedMultipart(t *testing.T) {
	f := newBackendFixture(t)
	_, source := f.database(t, "source")
	data := snapshotBytes(t, source)
	base := validImportParts("target", data)
	cases := map[string][]importPart{
		"missing_file": {base[0]}, "missing_name": {base[1]}, "empty": nil,
		"duplicate_name": {base[0], base[0], base[1]}, "duplicate_file": {base[0], base[1], base[1]},
		"unknown_field": {base[0], base[1], {field: "replace", data: []byte("true")}},
		"name_as_file":  {{field: "db_name", filename: "name.txt", data: []byte("target")}, base[1]},
		"file_as_text":  {base[0], {field: "file", data: data}},
		"invalid_name":  validImportParts("../target", data), "long_name": validImportParts(strings.Repeat("a", 65), data),
	}
	for name, parts := range cases {
		t.Run(name, func(t *testing.T) {
			req := sqliteImportRequest(t, parts...)
			req.Header.Set("Authorization", "Bearer "+f.token)
			out := httptest.NewRecorder()
			f.router.ServeHTTP(out, req)
			require.Equal(t, http.StatusBadRequest, out.Code, out.Body.String())
			assertImportClean(t, f, "target")
		})
	}
	for _, body := range []string{"", "not-multipart", "--broken\r\n"} {
		req := httptest.NewRequest("POST", "/api/v1/databases/import/sqlite", strings.NewReader(body))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=broken")
		req.Header.Set("Authorization", "Bearer "+f.token)
		out := httptest.NewRecorder()
		f.router.ServeHTTP(out, req)
		require.Equal(t, http.StatusBadRequest, out.Code, out.Body.String())
		assertImportClean(t, f, "target")
	}
}

func TestSQLiteImportRejectsInvalidSnapshots(t *testing.T) {
	f := newBackendFixture(t)
	_, source := f.database(t, "source")
	valid := snapshotBytes(t, source)
	corrupt := bytes.Clone(valid)
	corrupt[100] = 0xff // Invalid page-one B-tree type; preserve a valid file header.
	appended := append(bytes.Clone(valid), make([]byte, 4096)...)
	cases := map[string][]byte{"empty": nil, "sql": []byte("CREATE TABLE a(id INTEGER);"), "random": bytes.Repeat([]byte("a"), 4096), "truncated": valid[:len(valid)-1], "corrupt": corrupt, "extra_pages": appended}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			req := sqliteImportRequest(t, validImportParts("target", data)...)
			req.Header.Set("Authorization", "Bearer "+f.token)
			out := httptest.NewRecorder()
			f.router.ServeHTTP(out, req)
			require.Equal(t, http.StatusBadRequest, out.Code, out.Body.String())
			assertImportClean(t, f, "target")
		})
	}
	for name, schema := range map[string]string{
		"foreign_keys":           "CREATE TABLE bad(parent_id INTEGER REFERENCES items(id)); INSERT INTO bad VALUES(999)",
		"reserved":               "CREATE TABLE _nebula_private(value TEXT)",
		"virtual":                "CREATE VIRTUAL TABLE search USING fts4(content)",
		"virtual_catalog_shadow": "CREATE TABLE pragma_table_list(schema TEXT, type TEXT); CREATE VIRTUAL TABLE search USING fts4(content)",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.db")
			db, err := sql.Open("sqlite3", path)
			require.NoError(t, err)
			_, err = db.Exec("CREATE TABLE items(id INTEGER PRIMARY KEY);" + schema)
			require.NoError(t, err)
			data := snapshotBytes(t, db)
			require.NoError(t, db.Close())
			req := sqliteImportRequest(t, validImportParts("target", data)...)
			req.Header.Set("Authorization", "Bearer "+f.token)
			out := httptest.NewRecorder()
			f.router.ServeHTTP(out, req)
			require.Equal(t, http.StatusBadRequest, out.Code, out.Body.String())
			assertImportClean(t, f, "target")
		})
	}
}

func TestSQLiteImportAcceptsEmptyDatabaseAndReversedFields(t *testing.T) {
	f := newBackendFixture(t)
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "empty.db"))
	require.NoError(t, err)
	_, err = db.Exec("VACUUM")
	require.NoError(t, err)
	data := snapshotBytes(t, db)
	require.NoError(t, db.Close())
	parts := validImportParts("empty_db", data)
	req := sqliteImportRequest(t, parts[1], parts[0])
	req.Header.Set("Authorization", "Bearer "+f.token)
	out := httptest.NewRecorder()
	f.router.ServeHTTP(out, req)
	require.Equal(t, 201, out.Code, out.Body.String())
	details := f.request("GET", "/api/v1/databases/empty_db", "Bearer "+f.token, "")
	require.Equal(t, 200, details.Code, details.Body.String())
}

func TestSQLiteImportLiveHTTP(t *testing.T) {
	f := newBackendFixture(t)
	_, source := f.database(t, "source")
	data := snapshotBytes(t, source)
	server := httptest.NewServer(f.router)
	defer server.Close()
	req := sqliteImportRequest(t, validImportParts("target", data)...)
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(server.URL, "http://")
	req.RequestURI = ""
	req.Header.Set("Authorization", "Bearer "+f.token)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, 201, response.StatusCode, string(body))
}

func TestSQLiteImportKeepsLaterHTTPRequestsHealthy(t *testing.T) {
	f := newBackendFixture(t)
	_, source := f.database(t, "source")
	data := snapshotBytes(t, source)
	server := httptest.NewServer(f.router)
	t.Cleanup(server.Close)
	transport := &http.Transport{MaxConnsPerHost: 1}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request := func(req *http.Request, status int, reused, closeConnection bool) {
		t.Helper()
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(server.URL, "http://")
		req.RequestURI = ""
		req.Header.Set("Authorization", "Bearer "+f.token)
		var wasReused bool
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) { wasReused = info.Reused },
		}))
		response, err := client.Do(req)
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, response.Body.Close())
		require.NoError(t, err)
		require.Equal(t, status, response.StatusCode, string(body))
		require.Equal(t, reused, wasReused)
		require.Equal(t, closeConnection, response.Close)
	}
	request(sqliteImportRequest(t, validImportParts("target", data)...), 201, false, false)
	request(httptest.NewRequest("GET", "/api/v1/databases", http.NoBody), 200, true, false)
	request(sqliteImportRequest(t, validImportParts("target", data)...), 409, true, true)
	request(httptest.NewRequest("GET", "/api/v1/databases", http.NoBody), 200, false, false)
	request(sqliteImportRequest(t, validImportParts("another", data)...), 201, true, false)
	request(httptest.NewRequest("GET", "/api/v1/databases", http.NoBody), 200, true, false)
}

func TestSQLiteImportRejectsUnfinishedHTTPUploadPromptly(t *testing.T) {
	f := newBackendFixture(t)
	server := httptest.NewServer(f.router)
	defer server.Close()
	for _, contentType := range []string{"application/json", "multipart/form-data; boundary=boundary"} {
		conn, err := net.DialTimeout("tcp", strings.TrimPrefix(server.URL, "http://"), 2*time.Second)
		require.NoError(t, err)
		_, err = fmt.Fprintf(conn, "POST /api/v1/databases/import/sqlite HTTP/1.1\r\nHost: localhost\r\nAuthorization: Bearer %s\r\nContent-Type: %s\r\nTransfer-Encoding: chunked\r\n\r\n", f.token, contentType)
		require.NoError(t, err)
		chunk := "--boundary\r\nContent-Disposition: form-data; name=\"unknown\"\r\n\r\n"
		_, err = fmt.Fprintf(conn, "%x\r\n%s\r\n", len(chunk), chunk)
		require.NoError(t, err)
		// No body terminator is sent: the server must respond without draining it.
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		response, err := http.ReadResponse(bufio.NewReader(conn), nil)
		require.NoError(t, err)
		expected := 400
		if contentType == "application/json" {
			expected = 415
		}
		require.Equal(t, expected, response.StatusCode)
		require.NoError(t, response.Body.Close())
		require.NoError(t, conn.Close())
	}
	assertImportClean(t, f, "target")
}

func TestSQLiteImportBusySlotsAndStalledUploadCancellation(t *testing.T) {
	f := newBackendFixture(t)
	_, source := f.database(t, "source")
	data := snapshotBytes(t, source)
	var cancelUploads []context.CancelFunc
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		reader, writer := io.Pipe()
		multipartWriter := multipart.NewWriter(writer)
		ctx, cancel := context.WithCancel(t.Context())
		cancelUploads = append(cancelUploads, cancel)
		t.Cleanup(cancel)
		req := httptest.NewRequest("POST", "/api/v1/databases/import/sqlite", reader).WithContext(ctx)
		req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+f.token)
		go func() {
			out := httptest.NewRecorder()
			f.router.ServeHTTP(out, req)
			results <- out.Code
		}()
		started := make(chan error, 1)
		go func() {
			err := multipartWriter.WriteField("db_name", "cancelled")
			if err == nil {
				_, err = multipartWriter.CreateFormFile("file", "snapshot.db")
			}
			started <- err // Header consumed; no file bytes will arrive until cancellation.
		}()
		select {
		case err := <-started:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("upload did not start")
		}
		t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	}
	req := sqliteImportRequest(t, validImportParts("target", data)...)
	req.Header.Set("Authorization", "Bearer "+f.token)
	out := httptest.NewRecorder()
	f.router.ServeHTTP(out, req)
	require.Equal(t, 503, out.Code, out.Body.String())
	require.Equal(t, "5", out.Header().Get("Retry-After"))
	for _, cancel := range cancelUploads {
		cancel()
	}
	for i := 0; i < 2; i++ {
		select {
		case status := <-results:
			require.Equal(t, 408, status)
		case <-time.After(3 * time.Second):
			t.Fatal("cancelled upload stayed blocked")
		}
	}
	assertImportClean(t, f, "cancelled")
	assertImportClean(t, f, "target")
}

func TestSQLiteImportAuthenticationAndOwnerIsolation(t *testing.T) {
	f := newBackendFixture(t)
	id, source := f.database(t, "source")
	data := snapshotBytes(t, source)
	key, err := storage.StoreAPIKey(t.Context(), f.meta, "regression-owner", id)
	require.NoError(t, err)
	for _, tc := range []struct {
		auth   string
		status int
	}{{"", 401}, {"Bearer invalid", 401}, {"ApiKey " + key, 403}} {
		req := sqliteImportRequest(t, validImportParts("target", data)...)
		req.Header.Set("Authorization", tc.auth)
		out := httptest.NewRecorder()
		f.router.ServeHTTP(out, req)
		require.Equal(t, tc.status, out.Code, out.Body.String())
		assertImportClean(t, f, "target")
	}
	_, err = storage.CreateUser(t.Context(), f.meta, "other-owner", "other", "other@example.test", "hash")
	require.NoError(t, err)
	otherToken, err := auth.GenerateJWT("other-owner", f.cfg.JWTSecret, f.cfg.JWTExpiration)
	require.NoError(t, err)
	for _, token := range []string{f.token, otherToken} {
		req := sqliteImportRequest(t, validImportParts("shared_name", data)...)
		req.Header.Set("Authorization", "Bearer "+token)
		out := httptest.NewRecorder()
		f.router.ServeHTTP(out, req)
		require.Equal(t, 201, out.Code, out.Body.String())
	}
	first, err := storage.FindDatabasePath(t.Context(), f.meta, "regression-owner", "shared_name")
	require.NoError(t, err)
	second, err := storage.FindDatabasePath(t.Context(), f.meta, "other-owner", "shared_name")
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	infoA, err := os.Stat(first)
	require.NoError(t, err)
	infoB, err := os.Stat(second)
	require.NoError(t, err)
	require.False(t, os.SameFile(infoA, infoB))
}

func TestSQLiteImportDoesNotReplaceRegistrationsFilesOrSidecars(t *testing.T) {
	f := newBackendFixture(t)
	_, source := f.database(t, "source")
	data := snapshotBytes(t, source)
	userDir := filepath.Join(f.cfg.MetadataDbDir, "regression-owner")
	require.NoError(t, os.MkdirAll(userDir, 0o750))
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		path := filepath.Join(userDir, "target.db"+suffix)
		require.NoError(t, os.WriteFile(path, []byte("do-not-touch"), 0o600))
		req := sqliteImportRequest(t, validImportParts("target", data)...)
		req.Header.Set("Authorization", "Bearer "+f.token)
		out := httptest.NewRecorder()
		f.router.ServeHTTP(out, req)
		require.Equal(t, 409, out.Code, out.Body.String())
		preserved, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "do-not-touch", string(preserved))
		_, err = storage.FindDatabasePath(t.Context(), f.meta, "regression-owner", "target")
		require.ErrorIs(t, err, storage.ErrDatabaseNotFound)
		require.NoError(t, os.Remove(path))
	}
	req := sqliteImportRequest(t, validImportParts("source", data)...)
	req.Header.Set("Authorization", "Bearer "+f.token)
	out := httptest.NewRecorder()
	f.router.ServeHTTP(out, req)
	require.Equal(t, 409, out.Code, out.Body.String())
	var count int
	require.NoError(t, source.QueryRow("SELECT count(*) FROM items").Scan(&count))
	require.Equal(t, 1, count)
}

func TestSQLiteImportConcurrentSameName(t *testing.T) {
	f := newBackendFixture(t)
	_, source := f.database(t, "source")
	data := snapshotBytes(t, source)
	requests := []*http.Request{sqliteImportRequest(t, validImportParts("target", data)...), sqliteImportRequest(t, validImportParts("target", data)...)}
	results := make(chan int, 2)
	var group sync.WaitGroup
	start := make(chan struct{})
	for _, req := range requests {
		req.Header.Set("Authorization", "Bearer "+f.token)
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			out := httptest.NewRecorder()
			f.router.ServeHTTP(out, req)
			results <- out.Code
		}()
	}
	close(start)
	group.Wait()
	close(results)
	var statuses []int
	for status := range results {
		statuses = append(statuses, status)
	}
	require.ElementsMatch(t, []int{201, 409}, statuses)
	var count int
	require.NoError(t, f.meta.QueryRow("SELECT count(*) FROM databases WHERE db_name='target'").Scan(&count))
	require.Equal(t, 1, count)
}

func TestSQLiteImportCancellationAndContentType(t *testing.T) {
	f := newBackendFixture(t)
	_, source := f.database(t, "source")
	data := snapshotBytes(t, source)
	for _, contentType := range []string{"application/json", "application/octet-stream", "multipart/form-data", ""} {
		req := sqliteImportRequest(t, validImportParts("target", data)...)
		req.Header.Set("Authorization", "Bearer "+f.token)
		req.Header.Set("Content-Type", contentType)
		out := httptest.NewRecorder()
		f.router.ServeHTTP(out, req)
		require.Equal(t, 415, out.Code)
		assertImportClean(t, f, "target")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := sqliteImportRequest(t, validImportParts("target", data)...).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+f.token)
	out := httptest.NewRecorder()
	f.router.ServeHTTP(out, req)
	require.Equal(t, 408, out.Code, out.Body.String())
	assertImportClean(t, f, "target")
}

type importZeroReader struct{}

func (importZeroReader) Read(data []byte) (int, error) {
	clear(data)
	return len(data), nil
}

func TestSQLiteImportSizeLimits(t *testing.T) {
	f := newBackendFixture(t)
	_, source := f.database(t, "source")
	data := snapshotBytes(t, source)
	req := sqliteImportRequest(t, validImportParts("target", data)...)
	req.ContentLength = storage.MaxSQLiteImportBytes + (64 << 10) + 1
	req.Header.Set("Authorization", "Bearer "+f.token)
	out := httptest.NewRecorder()
	f.router.ServeHTTP(out, req)
	require.Equal(t, 413, out.Code)
	assertImportClean(t, f, "target")

	// Stream an oversized file with an unknown Content-Length; never allocate it in RAM.
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	errors := make(chan error, 1)
	go func() {
		defer writer.Close()
		err := multipartWriter.WriteField("db_name", "target")
		if err == nil {
			var part io.Writer
			part, err = multipartWriter.CreateFormFile("file", "large.db")
			if err == nil {
				_, err = io.CopyN(part, importZeroReader{}, storage.MaxSQLiteImportBytes+1)
			}
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		errors <- err
	}()
	req = httptest.NewRequest("POST", "/api/v1/databases/import/sqlite", reader)
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+f.token)
	out = httptest.NewRecorder()
	f.router.ServeHTTP(out, req)
	<-errors // The handler closes the body; the producer cannot leak on a rejected upload.
	require.Equal(t, 413, out.Code, out.Body.String())
	assertImportClean(t, f, "target")

	// Oversized trailing bytes must not evade the overall multipart body limit.
	req = sqliteImportRequest(t, validImportParts("target", data)...)
	base, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	req.Body = io.NopCloser(io.MultiReader(bytes.NewReader(base), io.LimitReader(importZeroReader{}, storage.MaxSQLiteImportBytes+(64<<10))))
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer "+f.token)
	out = httptest.NewRecorder()
	f.router.ServeHTTP(out, req)
	require.Equal(t, 413, out.Code, out.Body.String())
	assertImportClean(t, f, "target")
}
