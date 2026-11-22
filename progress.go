package main

import "fmt"

func scanProgress(job Job) (int, string) { return chinese.scanProgress(job) }

func (l uiLanguage) scanProgress(job Job) (int, string) {
	if job.Total <= 0 {
		if job.Running {
			return 0, l.text("准备扫描…")
		}
		if job.Error != "" {
			return 0, l.text("扫描失败")
		}
		return 0, l.text("等待扫描")
	}
	done := min(max(job.Done, 0), job.Total)
	percent := int(int64(done) * 100 / int64(job.Total))
	state := l.text("扫描结束")
	if job.Running {
		state = l.text("扫描中")
	} else if done < job.Total || job.Error != "" {
		state = l.text("扫描已停止")
	}
	return percent, fmt.Sprintf("%s %d%% · %d / %d", state, percent, done, job.Total)
}

func progressBarLabel(job Job, width int32) string { return chinese.progressBarLabel(job, width) }

func (l uiLanguage) progressBarLabel(job Job, width int32) string {
	percent, detail := l.scanProgress(job)
	if width >= 300 {
		return detail
	}
	if job.Total <= 0 {
		return detail
	}
	state := l.text("扫描完成")
	if job.Running {
		state = l.text("扫描中")
	} else if job.Done < job.Total || job.Error != "" {
		state = l.text("已停止")
	}
	return fmt.Sprintf("%s %d%%", state, percent)
}
