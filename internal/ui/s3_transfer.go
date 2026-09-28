package ui

import "fmt"

type s3TransferJob struct {
	filename   string
	localPath  string
	remotePath string
	isUpload   bool
}

type s3TransferQueue struct {
	jobs []s3TransferJob
	idx  int
}

func (q *s3TransferQueue) empty() bool {
	return len(q.jobs) == 0
}

func (q *s3TransferQueue) current() s3TransferJob {
	if q.empty() || q.idx < 0 || q.idx >= len(q.jobs) {
		return s3TransferJob{}
	}
	return q.jobs[q.idx]
}

func (q *s3TransferQueue) startOrEnqueue(job s3TransferJob) (s3TransferJob, bool) {
	if q.empty() {
		q.jobs = []s3TransferJob{job}
		q.idx = 0
		return job, true
	}
	q.jobs = append(q.jobs, job)
	return s3TransferJob{}, false
}

func (q *s3TransferQueue) finishCurrent() (s3TransferJob, bool) {
	if q.empty() {
		return s3TransferJob{}, false
	}
	if q.idx+1 < len(q.jobs) {
		q.idx++
		return q.jobs[q.idx], true
	}
	q.clear()
	return s3TransferJob{}, false
}

func (q *s3TransferQueue) clear() {
	q.jobs = nil
	q.idx = 0
}

func (q *s3TransferQueue) position() (cur, total int) {
	if q.empty() {
		return 0, 0
	}
	return q.idx + 1, len(q.jobs)
}

func staleS3Progress(transferring bool, currentGen, msgGen int) bool {
	return !transferring || currentGen != msgGen
}

func formatS3Progress(job s3TransferJob, done, total int64, cur, count int) string {
	action := "Downloading"
	if job.isUpload {
		action = "Uploading"
	}
	var body string
	if total > 0 {
		pct := done * 100 / total
		body = fmt.Sprintf("%s %s: %s / %s (%d%%)",
			action, job.filename, formatSize(done), formatSize(total), pct)
	} else {
		body = fmt.Sprintf("%s %s: %s", action, job.filename, formatSize(done))
	}
	if count > 1 {
		body = fmt.Sprintf("%s  [%d/%d]", body, cur, count)
	}
	return body
}
