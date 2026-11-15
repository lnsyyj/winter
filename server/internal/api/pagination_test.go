package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func testContext(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	return c, w
}

func TestParsePagination(t *testing.T) {
	cases := []struct {
		name   string
		query  string
		page   int
		size   int
		offset int
	}{
		{"默认", "", 1, 20, 0},
		{"第二页", "?page=2&size=10", 2, 10, 10},
		{"第三页", "?page=3&size=5", 3, 5, 10},
		{"size 超上限截断", "?size=1000", 1, 100, 0},
		{"非法值回退默认", "?page=abc&size=-1", 1, 20, 0},
		{"page 为 0", "?page=0&size=0", 1, 20, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := testContext(http.MethodGet, "/v1/tasks"+tc.query)
			p := parsePagination(c)
			if p.Page != tc.page || p.Size != tc.size || p.Offset != tc.offset {
				t.Fatalf("= %+v，期望 page=%d size=%d offset=%d", p, tc.page, tc.size, tc.offset)
			}
		})
	}
}

func TestPageResultEmptyItemsIsArray(t *testing.T) {
	// 空列表必须输出 [] 而不是 null，否则前端 .map 会炸
	items := make([]taskListItem, 0)
	body, err := json.Marshal(pageResult(items, 0, pagination{Page: 1, Size: 20}))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	arr, ok := got["items"].([]any)
	if !ok || len(arr) != 0 {
		t.Fatalf("items 应是空数组，实际 %v（body=%s）", got["items"], body)
	}
	if got["total"] != float64(0) || got["page"] != float64(1) || got["size"] != float64(20) {
		t.Fatalf("分页字段不正确: %s", body)
	}
}
