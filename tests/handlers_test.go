package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cal1co/movielogv2-postservice/handlers"
	"github.com/gin-gonic/gin"
)

type MockQuery struct {
	ExecError error
}

func (mq *MockQuery) Exec() error {
	return mq.ExecError
}

type MockSession struct {
	QueryError   error
	QueryInvoked bool
	MockQuery    *MockQuery
}

func (ms *MockSession) Query(stmt string, values ...interface{}) *MockQuery {
	ms.QueryInvoked = true
	return ms.MockQuery
}

func TestHandlePost(t *testing.T) {
	mockSession := &MockSession{
		MockQuery: &MockQuery{
			ExecError: nil,
		},
	}

	r := gin.Default()

	r.POST("/post", func(c *gin.Context) {
		handlers.HandlePost(c, mockSession)
	})

	post := &Post{
		UserID:      1,
		PostContent: "Test Content",
		Media:       []string{"test1.jpg", "test2.jpg"},
	}

	reqBody, _ := json.Marshal(post)

	req, _ := http.NewRequest(http.MethodPost, "/post", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if status := w.Code; status != http.StatusCreated {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusCreated)
	}

	if !mockSession.QueryInvoked {
		t.Errorf("Query was not invoked")
	}
}

type MockScan struct {
	ScanError error
}

func (ms *MockScan) Scan(dest ...interface{}) error {
	*(dest[0].(*int)) = 1
	return ms.ScanError
}

func (ms *MockSession) Query(stmt string, values ...interface{}) *MockQuery {
	ms.QueryInvoked = true
	return ms.MockQuery
}

func TestCheckLikedByUser(t *testing.T) {
	mockSession := &MockSession{
		MockQuery: &MockQuery{
			Scan: &MockScan{
				ScanError: nil,
			},
		},
	}
	mockHandler := &Handler{
		Session: mockSession,
	}

	uid := "test-user-id"
	postId := "test-post-id"

	result := CheckLikedByUser(uid, postId, mockHandler)

	if result != true {
		t.Errorf("Expected true, but got %v", result)
	}

	if !mockSession.QueryInvoked {
		t.Errorf("Query was not invoked")
	}
}

type MockHttpClient struct{}

func (m *MockHttpClient) Post(url, contentType string, body io.Reader) (resp *http.Response, err error) {
	return &http.Response{StatusCode: http.StatusOK}, nil
}

func TestFanoutPost(t *testing.T) {
	mockClient := &MockHttpClient{}
	post := Post{
		UserID:      1,
		PostContent: "Test Content",
		Media:       []string{"test1.jpg", "test2.jpg"},
	}

	err := handlers.fanoutPost(mockClient, post)

	if err != nil {
		t.Errorf("fanoutPost() returned an error: %v", err)
	}
}
