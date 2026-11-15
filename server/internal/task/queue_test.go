package task

import (
	"testing"
	"time"
)

func TestBackoffDelay(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second}, // 非法入参也取第一档，不能 panic
		{1, 1 * time.Second},
		{2, 5 * time.Second},
		{3, 30 * time.Second},
		{4, 2 * time.Minute},
		{5, 10 * time.Minute},
		{99, 10 * time.Minute}, // 超上限保持最后一档
	}
	for _, tc := range cases {
		t.Run(tc.want.String(), func(t *testing.T) {
			if got := BackoffDelay(tc.attempt); got != tc.want {
				t.Fatalf("BackoffDelay(%d) = %s，期望 %s", tc.attempt, got, tc.want)
			}
		})
	}
}

func TestMaxAttemptsMatchesBackoffSteps(t *testing.T) {
	// 改死信上限时必须同步改退避序列，否则第 N 次重试会用到未定义的档位
	if MaxAttempts != len(backoffSteps) {
		t.Fatalf("MaxAttempts=%d 与退避档位数 %d 不一致", MaxAttempts, len(backoffSteps))
	}
}

func TestIdempotencyKey(t *testing.T) {
	k1 := IdempotencyKey("cluster-1", "pool.create", map[string]any{"b": 2, "a": 1})
	k2 := IdempotencyKey("cluster-1", "pool.create", map[string]any{"a": 1, "b": 2})
	if k1 != k2 {
		t.Fatal("map 键顺序不同不应影响幂等键")
	}
	if len(k1) != 64 {
		t.Fatalf("sha256 十六进制长度应为 64，实际 %d", len(k1))
	}

	distinct := []struct {
		name string
		key  string
	}{
		{"不同集群", IdempotencyKey("cluster-2", "pool.create", map[string]any{"a": 1, "b": 2})},
		{"不同任务类型", IdempotencyKey("cluster-1", "osd.create", map[string]any{"a": 1, "b": 2})},
		{"不同参数", IdempotencyKey("cluster-1", "pool.create", map[string]any{"a": 1, "b": 3})},
		{"无参数", IdempotencyKey("cluster-1", "pool.create", nil)},
	}
	for _, tc := range distinct {
		t.Run(tc.name, func(t *testing.T) {
			if tc.key == k1 {
				t.Fatal("不同业务输入不能撞同一个幂等键")
			}
		})
	}

	// 字段间必须有分隔符，否则 ("ab","c") 与 ("a","bc") 会拼出同样的串
	if IdempotencyKey("ab", "c", nil) == IdempotencyKey("a", "bc", nil) {
		t.Fatal("拼接字段缺少分隔符，存在碰撞")
	}
}

func TestValidateType(t *testing.T) {
	cases := []struct {
		taskType string
		wantErr  bool
	}{
		{"cluster.create", false},
		{"host.precheck", false},
		{"pool.create", false},
		{"osd.create", false},
		{"", true},
		{"unknown.task", true},
		{"pool.create; drop table users", true},
	}
	for _, tc := range cases {
		t.Run(tc.taskType, func(t *testing.T) {
			err := ValidateType(tc.taskType)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateType(%q) err=%v，wantErr=%v", tc.taskType, err, tc.wantErr)
			}
		})
	}
}
