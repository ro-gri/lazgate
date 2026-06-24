package admin

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateAWGSignatures(t *testing.T) {
	for _, body := range []string{
		`{"count":0}`, `{"count":6}`, `{"count":"1"}`, `{"count":1,"profile":"invalid"}`,
		`{"count":1,"profile":"dns","service":"custom","server_name":"https://example.com"}`,
		`{"count":1,"extra":true}`, `{"count":1} {}`,
	} {
		w := httptest.NewRecorder()
		(&App{}).generateAWGSignatures(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("%s: status %d", body, w.Code)
		}
	}
	for _, count := range []string{"1", "5"} {
		w := httptest.NewRecorder()
		(&App{}).generateAWGSignatures(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"count":`+count+`,"profile":"dns","service":"google"}`)))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var result struct {
			Signatures []string `json:"signatures"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Signatures) != int(count[0]-'0') {
			t.Fatal(result)
		}
		for _, s := range result.Signatures {
			if !strings.Contains(s, "<b ") {
				t.Fatal(s)
			}
		}
	}
}
