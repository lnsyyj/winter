package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestFailReturnsUnifiedError(t *testing.T) {
	c, w := testContext(http.MethodGet, "/v1/me")
	c.Set("trace_id", "trace-123")

	fail(c, http.StatusForbidden, codeForbidden, "权限不足")

	if w.Code != http.StatusForbidden {
		t.Fatalf("状态码 = %d，期望 403", w.Code)
	}
	var got APIError
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("响应不是 JSON: %v（body=%s）", err, w.Body.String())
	}
	if got.Code != codeForbidden || got.Message != "权限不足" || got.TraceID != "trace-123" {
		t.Fatalf("错误体不符合约定: %+v", got)
	}
	if got.Details != nil {
		t.Fatalf("无 details 时应省略该字段: %+v", got)
	}
}

func TestFailWithoutTraceOmitsField(t *testing.T) {
	c, w := testContext(http.MethodPost, "/v1/auth/login")
	fail(c, http.StatusUnauthorized, codeUnauthorized, "用户名或口令不正确")

	var raw map[string]any
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("响应不是 JSON: %v", err)
	}
	if _, ok := raw["trace_id"]; ok {
		t.Fatalf("未落 intent 的请求不应带 trace_id: %v", raw)
	}
	if raw["code"] != codeUnauthorized {
		t.Fatalf("业务码不正确: %v", raw)
	}
}
