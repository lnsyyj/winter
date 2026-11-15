// Package task 实现 ADR-36「任务表即队列」的服务端语义，以及任务元数据规则。
// 队列的唯一真相是 PG tasks 表；本包不引入任何 MQ 依赖。
package task

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// IdempotencyKey 按《API 与数据模型设计》§4 生成：
// sha256(cluster_id + type + canonical(params))。
//
// encoding/json 在编码 map 时会排序 key，因此同一组参数无论提交顺序、
// 无论来自 Web 还是内部调用，都得到同一个键——这是幂等能成立的前提。
// 字段之间写入 0 字节分隔，避免 ("ab","c") 与 ("a","bc") 拼出同样的串。
func IdempotencyKey(clusterID, taskType string, params any) string {
	canonical, err := json.Marshal(params)
	if err != nil {
		canonical = []byte("null")
	}
	h := sha256.New()
	_, _ = h.Write([]byte(clusterID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(taskType))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil))
}
