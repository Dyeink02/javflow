package crawlexecution

import "fmt"

// index_action.go owns loop-level index-page action planning after one page has
// been processed.
//
// Ownership summary:
// 1) convert analyzed index-page outcomes into next-loop actions
// 2) centralize continue/stop/persist decisions after one page iteration
// 3) keep loop action policy separate from fetch/parsing implementations
//
// File map for maintainers:
// 1) loop action input/output DTOs
// 2) continue/stop/persist decision helpers
// 3) index iteration log message builders

// IndexProcessingActionPlanInput is the loop-level decision input after one
// index page has already been fetched and analyzed.
type IndexProcessingActionPlanInput struct {
	Action             string `json:"action"`
	ShouldStopIndexing bool   `json:"shouldStopIndexing"`
	CurrentPage        int    `json:"currentPage"`
	TargetTotalPages   int    `json:"targetTotalPages"`
}

// IndexProcessingActionPlan is the bridge between low-level page analysis and
// the next runner loop action.
type IndexProcessingActionPlan struct {
	ShouldStopIndexing        bool   `json:"shouldStopIndexing"`
	ShouldContinueCurrentLoop bool   `json:"shouldContinueCurrentLoop"`
	NextPageNumber            int    `json:"nextPageNumber"`
	LogMessage                string `json:"logMessage,omitempty"`
	ShouldPersistState        bool   `json:"shouldPersistState"`
	StateReason               string `json:"stateReason,omitempty"`
	StateMessage              string `json:"stateMessage,omitempty"`
}

func ResolveIndexProcessingActionPlan(input IndexProcessingActionPlanInput) IndexProcessingActionPlan {
	switch input.Action {
	case "continue_after_gap":
		return IndexProcessingActionPlan{
			ShouldStopIndexing:        input.ShouldStopIndexing,
			ShouldContinueCurrentLoop: true,
			NextPageNumber:            input.CurrentPage + 1,
			LogMessage:                fmt.Sprintf("第 %d 页当前未解析到影片链接，但目标共 %d 页。已记录为分页缺口，继续尝试后续页面。", input.CurrentPage, input.TargetTotalPages),
			ShouldPersistState:        true,
			StateReason:               "索引页存在分页缺口",
			StateMessage:              fmt.Sprintf("第 %d 页当前未解析到影片链接，继续尝试后续页面。", input.CurrentPage),
		}
	case "stop_empty_page":
		return IndexProcessingActionPlan{
			ShouldStopIndexing:        input.ShouldStopIndexing,
			ShouldContinueCurrentLoop: true,
			NextPageNumber:            input.CurrentPage,
			LogMessage:                fmt.Sprintf("第 %d 页为空，停止继续抓取索引页。", input.CurrentPage),
			ShouldPersistState:        true,
			StateReason:               "索引页为空，停止继续抓取",
			StateMessage:              fmt.Sprintf("第 %d 页为空，停止继续抓取索引页。", input.CurrentPage),
		}
	case "continue_resume_completed_page":
		return IndexProcessingActionPlan{
			ShouldStopIndexing:        input.ShouldStopIndexing,
			ShouldContinueCurrentLoop: true,
			NextPageNumber:            input.CurrentPage + 1,
			LogMessage:                fmt.Sprintf("第 %d 页均为已完成内容，继续检查后续页面是否存在漏抓项目。", input.CurrentPage),
			ShouldPersistState:        true,
			StateReason:               "恢复模式继续检查下一页",
			StateMessage:              "当前页均为已完成内容。",
		}
	case "stop_no_new_links":
		return IndexProcessingActionPlan{
			ShouldStopIndexing:        input.ShouldStopIndexing,
			ShouldContinueCurrentLoop: true,
			NextPageNumber:            input.CurrentPage,
			LogMessage:                fmt.Sprintf("第 %d 页没有新链接，停止继续抓取索引页。", input.CurrentPage),
			ShouldPersistState:        true,
			StateReason:               "索引页无新链接",
			StateMessage:              fmt.Sprintf("第 %d 页没有新链接。", input.CurrentPage),
		}
	case "stop_limit_reached":
		return IndexProcessingActionPlan{
			ShouldStopIndexing:        input.ShouldStopIndexing,
			ShouldContinueCurrentLoop: false,
			NextPageNumber:            input.CurrentPage,
			LogMessage:                "已达到目标抓取数量上限，当前页面处理完后将结束索引页扫描。",
			ShouldPersistState:        false,
		}
	case "stop_target_page_reached":
		return IndexProcessingActionPlan{
			ShouldStopIndexing:        input.ShouldStopIndexing,
			ShouldContinueCurrentLoop: false,
			NextPageNumber:            input.CurrentPage,
			LogMessage:                fmt.Sprintf("已达到配置或推算的最后一页：%d。", input.TargetTotalPages),
			ShouldPersistState:        false,
		}
	default:
		return IndexProcessingActionPlan{
			ShouldStopIndexing:        input.ShouldStopIndexing,
			ShouldContinueCurrentLoop: false,
			NextPageNumber:            input.CurrentPage,
			LogMessage:                "",
			ShouldPersistState:        false,
			StateReason:               "",
			StateMessage:              "",
		}
	}
}
