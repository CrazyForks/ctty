package ui

import "fmt"

type webdavTransferJob struct {
	filename   string
	localPath  string
	remotePath string
	isUpload   bool
}

type webdavTransferQueue struct {
	jobs []webdavTransferJob
	idx  int
}

func (q *webdavTransferQueue) empty() bool {
	return len(q.jobs) == 0
}

func (q *webdavTransferQueue) current() webdavTransferJob {
	if q.empty() || q.idx < 0 || q.idx >= len(q.jobs) {
		return webdavTransferJob{}
	}
	return q.jobs[q.idx]
}

func (q *webdavTransferQueue) startOrEnqueue(job webdavTransferJob) (webdavTransferJob, bool) {
	if q.empty() {
		q.jobs = []webdavTransferJob{job}
		q.idx = 0
		return job, true
	}
	q.jobs = append(q.jobs, job)
	return webdavTransferJob{}, false
}

func (q *webdavTransferQueue) finishCurrent() (webdavTransferJob, bool) {
	if q.empty() {
		return webdavTransferJob{}, false
	}
	if q.idx+1 < len(q.jobs) {
		q.idx++
		return q.jobs[q.idx], true
	}
	q.clear()
	return webdavTransferJob{}, false
}

func (q *webdavTransferQueue) clear() {
	q.jobs = nil
	q.idx = 0
}

func (q *webdavTransferQueue) position() (cur, total int) {
	if q.empty() {
		return 0, 0
	}
	return q.idx + 1, len(q.jobs)
}

func staleWebDAVProgress(transferring bool, currentGen, msgGen int) bool {
	return !transferring || currentGen != msgGen
}

func formatWebDAVProgress(job webdavTransferJob, done, total int64, cur, count int) string {
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
