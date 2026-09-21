package main

import (
	"testing"

	"PDT/internal/cloudsync"
)

// The cloud -> flash-drive direction only ever pulls: files the bucket has and
// the drive lacks are downloaded; conflicts (different sizes) are counted but
// never touched; files only the drive has are ignored, never uploaded.
func TestCloudDownloadJobs(t *testing.T) {
	plan := []cloudsync.PlanItem{
		{RelPath: "Canon/a.zip", Action: cloudsync.ActionDownload, RemoteSize: 100},
		{RelPath: "Canon/b.zip", Action: cloudsync.ActionDownload, RemoteSize: 250},
		{RelPath: "Canon/same.zip", Action: cloudsync.ActionSynced, LocalSize: 5, RemoteSize: 5},
		{RelPath: "Ricoh/diff.zip", Action: cloudsync.ActionConflict, LocalSize: 1, RemoteSize: 2},
		{RelPath: "Xerox/local-only.zip", Action: cloudsync.ActionUpload, LocalSize: 999},
	}
	jobs, total, conflicts := cloudDownloadJobs(plan)
	if len(jobs) != 2 || jobs[0].RelPath != "Canon/a.zip" || jobs[1].RelPath != "Canon/b.zip" {
		t.Errorf("expected exactly the two download items, got %+v", jobs)
	}
	if total != 350 {
		t.Errorf("total bytes = %d, want 350 (uploads and conflicts must not count)", total)
	}
	if conflicts != 1 {
		t.Errorf("conflicts = %d, want 1", conflicts)
	}
}
