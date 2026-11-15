package task

import "fmt"

// AllowedTypes 是 M0 的任务模板白名单（《API 与数据模型设计》§6）。
//
// 这是安全边界而不是便利列表：Agent 只按 type 查本地模板执行，
// 服务端若放行任意字符串，等于把命令参数拼进了执行面。
// 新增模板必须同时发布 Agent 侧实现，并走代码评审 + 版本化发布。
var AllowedTypes = map[string]bool{
	"cluster.create": true,
	"cluster.import": true,
	"cluster.sync":   true,
	"cluster.delete": true,
	"host.precheck":  true,
	"pool.create":    true,
	"osd.create":     true,
}

// ValidateType 拒绝白名单外的任务类型。
func ValidateType(taskType string) error {
	if !AllowedTypes[taskType] {
		return fmt.Errorf("未知任务类型 %q：必须是白名单内的模板", taskType)
	}
	return nil
}
