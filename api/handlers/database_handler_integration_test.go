package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/domain"
)

func TestDatabaseStudioEndpoints(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Signup & Login
	signupReq := models.SignupRequest{
		Email:    "studio_user@example.com",
		Username: "studiouser",
		Password: "Password123!",
	}
	signupBytes, _ := json.Marshal(signupReq)
	res, err := http.Post(server.URL+"/auth/signup", "application/json", bytes.NewReader(signupBytes))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	res.Body.Close()

	loginReq := models.LoginRequest{
		Email:    signupReq.Email,
		Password: signupReq.Password,
	}
	loginBytes, _ := json.Marshal(loginReq)
	res, err = http.Post(server.URL+"/auth/login", "application/json", bytes.NewReader(loginBytes))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)

	var loginRes models.LoginResponse
	err = json.NewDecoder(res.Body).Decode(&loginRes)
	require.NoError(t, err)
	res.Body.Close()
	jwtToken := loginRes.Token

	// Helper for authenticated requests
	doAuthReq := func(method, url string, body []byte) *http.Response {
		var req *http.Request
		if body != nil {
			req, _ = http.NewRequest(method, url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req, _ = http.NewRequest(method, url, nil)
		}
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		client := &http.Client{}
		resp, err := client.Do(req)
		require.NoError(t, err)
		return resp
	}

	// 2. Create a Database
	createDBReq := models.CreateDatabaseRequest{DBName: "demodb"}
	createDBBytes, _ := json.Marshal(createDBReq)
	res = doAuthReq("POST", server.URL+"/api/v1/databases", createDBBytes)
	assert.Equal(t, http.StatusCreated, res.StatusCode)
	res.Body.Close()

	// 3. Create an API Key for demodb
	res = doAuthReq("POST", server.URL+"/api/v1/account/databases/demodb/apikey", nil)
	assert.Equal(t, http.StatusCreated, res.StatusCode)
	res.Body.Close()

	// 4. Create a Table in the Database
	createTableReq := models.CreateSchemaRequest{
		TableName: "customers",
		Columns: []models.ColumnDefinition{
			{Name: "name", Type: "TEXT"},
			{Name: "email", Type: "TEXT"},
		},
	}
	createTableBytes, _ := json.Marshal(createTableReq)
	res = doAuthReq("POST", server.URL+"/api/v1/databases/demodb/tables", createTableBytes)
	assert.Equal(t, http.StatusCreated, res.StatusCode)
	res.Body.Close()

	// 5. Insert records using POST /api/v1/databases/demodb/tables/customers/records
	record1 := map[string]interface{}{
		"name":  "Alice Smith",
		"email": "alice@example.com",
	}
	recordBytes, _ := json.Marshal(record1)
	res = doAuthReq("POST", server.URL+"/api/v1/databases/demodb/tables/customers/records", recordBytes)
	assert.Equal(t, http.StatusCreated, res.StatusCode)
	res.Body.Close()

	// 6. Test ListTables returns rowCount = 1
	res = doAuthReq("GET", server.URL+"/api/v1/databases/demodb/tables", nil)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	var tablesRes struct {
		Tables []domain.TableMetadata `json:"tables"`
	}
	err = json.NewDecoder(res.Body).Decode(&tablesRes)
	require.NoError(t, err)
	res.Body.Close()
	require.Len(t, tablesRes.Tables, 1)
	assert.Equal(t, "customers", tablesRes.Tables[0].TableName)
	assert.Equal(t, int64(1), tablesRes.Tables[0].RowCount)

	// 7. Test GET /api/v1/databases/demodb (GetDatabase details)
	res = doAuthReq("GET", server.URL+"/api/v1/databases/demodb", nil)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	var dbDetailRes struct {
		Database domain.DatabaseDetailMetadata `json:"database"`
	}
	err = json.NewDecoder(res.Body).Decode(&dbDetailRes)
	require.NoError(t, err)
	res.Body.Close()

	assert.Equal(t, "demodb", dbDetailRes.Database.DBName)
	assert.Equal(t, int64(1), dbDetailRes.Database.Tables)
	assert.Equal(t, int64(1), dbDetailRes.Database.TotalRecords)
	assert.NotEmpty(t, dbDetailRes.Database.SizeDisplay)
	assert.NotEmpty(t, dbDetailRes.Database.APIKey)

	// 8. Test POST /api/v1/databases/demodb/sql (ExecuteSQL)
	// 8a. SELECT query
	sqlQueryReq := map[string]string{"query": "SELECT id, name, email FROM customers"}
	sqlQueryBytes, _ := json.Marshal(sqlQueryReq)
	res = doAuthReq("POST", server.URL+"/api/v1/databases/demodb/sql", sqlQueryBytes)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	var sqlRes domain.SQLQueryResult
	err = json.NewDecoder(res.Body).Decode(&sqlRes)
	require.NoError(t, err)
	res.Body.Close()

	assert.Equal(t, []string{"id", "name", "email"}, sqlRes.Columns)
	assert.Equal(t, int64(1), sqlRes.RowCount)
	assert.Len(t, sqlRes.Rows, 1)
	// Column order: id, name, email -> row[1] is name
	assert.Equal(t, "Alice Smith", sqlRes.Rows[0][1])
	assert.True(t, sqlRes.ExecutionMs >= 0)

	// 8b. INSERT via SQL
	sqlInsertReq := map[string]string{"query": "INSERT INTO customers (name, email) VALUES ('Bob Jones', 'bob@example.com')"}
	sqlInsertBytes, _ := json.Marshal(sqlInsertReq)
	res = doAuthReq("POST", server.URL+"/api/v1/databases/demodb/sql", sqlInsertBytes)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	var sqlInsertRes domain.SQLQueryResult
	err = json.NewDecoder(res.Body).Decode(&sqlInsertRes)
	require.NoError(t, err)
	res.Body.Close()

	assert.Equal(t, int64(1), sqlInsertRes.RowsAffected)
	assert.Contains(t, sqlInsertRes.Message, "rows affected")

	// 8c. Verify row count updated
	res = doAuthReq("GET", server.URL+"/api/v1/databases/demodb/tables", nil)
	err = json.NewDecoder(res.Body).Decode(&tablesRes)
	require.NoError(t, err)
	res.Body.Close()
	assert.Equal(t, int64(2), tablesRes.Tables[0].RowCount)

	// 9. Test Analytics & Schema Advisor
	res = doAuthReq("GET", server.URL+"/api/v1/databases/demodb/analytics", nil)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	var analyticsRes domain.DatabaseAnalytics
	err = json.NewDecoder(res.Body).Decode(&analyticsRes)
	require.NoError(t, err)
	res.Body.Close()

	assert.True(t, analyticsRes.TotalRequests >= 0)
	assert.NotEmpty(t, analyticsRes.Services)
	assert.NotNil(t, analyticsRes.Advisor)

	// 10. Test Schema Visualizer Diagram endpoint
	res = doAuthReq("GET", server.URL+"/api/v1/databases/demodb/diagram", nil)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	var diagramRes domain.SchemaDiagram
	err = json.NewDecoder(res.Body).Decode(&diagramRes)
	require.NoError(t, err)
	res.Body.Close()
	assert.Equal(t, 1, diagramRes.TotalTables)
	assert.Equal(t, "customers", diagramRes.Tables[0].Name)
	assert.NotEmpty(t, diagramRes.Tables[0].Columns)

	// 11. Test Database Objects endpoint (indexes & triggers)
	res = doAuthReq("GET", server.URL+"/api/v1/databases/demodb/objects", nil)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	var objectsRes domain.DatabaseObjects
	err = json.NewDecoder(res.Body).Decode(&objectsRes)
	require.NoError(t, err)
	res.Body.Close()
	assert.NotNil(t, objectsRes.Indexes)
	assert.NotNil(t, objectsRes.Triggers)

	// 12. Test Database SQL Export
	res = doAuthReq("GET", server.URL+"/api/v1/databases/demodb/export/sql", nil)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	var exportRes map[string]string
	err = json.NewDecoder(res.Body).Decode(&exportRes)
	require.NoError(t, err)
	res.Body.Close()
	assert.Contains(t, exportRes["sql"], "CREATE TABLE customers")
	assert.Contains(t, exportRes["sql"], "INSERT INTO customers")

	fmt.Println("All Studio backend tests passed!")
}
