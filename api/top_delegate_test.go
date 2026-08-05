package api

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTopDelegateRejectsUnsupportedN(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, n := range []int{-1, 1001} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			query := "?n=" + strconv.Itoa(n)
			c.Request = httptest.NewRequest("GET", "/top_delegate"+query, nil)

			(&Server{}).topDelegate(c)

			var body struct {
				Code  int    `json:"code"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
			}
			if body.Code != 400 || body.Error != "n must be between 0 and 1000" {
				t.Fatalf("response = %#v", body)
			}
		})
	}
}

func TestTopDelegateDateAcceptsStartDateAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		query string
		want  string
	}{
		{query: "?start_date=260211", want: "260211"},
		{query: "?date=260212&start_date=260211", want: "260212"},
	} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest("GET", "/top_delegate"+test.query, nil)

		got, ok := getTopDelegateDateParam(c)
		if !ok {
			t.Fatalf("query %s unexpectedly rejected: %s", test.query, recorder.Body.String())
		}
		if got.Format("060102") != test.want {
			t.Fatalf("query %s date = %s, want %s", test.query, got.Format("060102"), test.want)
		}
	}
}
