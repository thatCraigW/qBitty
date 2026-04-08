package main

import (
	"encoding/json"
	"testing"
)

func TestHumanizeArrEnum(t *testing.T) {
	if g := humanizeArrEnum("importPending"); g != "import pending" {
		t.Fatalf("got %q", g)
	}
	if g := humanizeArrEnum("completed"); g != "completed" {
		t.Fatalf("got %q", g)
	}
}

func TestShouldShowArrColumnStatus(t *testing.T) {
	if !shouldShowArrColumnStatus(&arrQueueRecord{TrackedDownloadState: "importPending"}) {
		t.Fatal("expected import pending")
	}
	if shouldShowArrColumnStatus(&arrQueueRecord{TrackedDownloadState: "downloading"}) {
		t.Fatal("downloading should use qB")
	}
	if !shouldShowArrColumnStatus(&arrQueueRecord{Status: "completed"}) {
		t.Fatal("completed queue status")
	}
	if shouldShowArrColumnStatus(&arrQueueRecord{Status: "queued"}) {
		t.Fatal("queued without tracked state")
	}
}

func TestArrQueueRecordUnmarshalJSON_StringEnums(t *testing.T) {
	const j = `{"id":42,"downloadId":"x","title":"t","status":"completed","trackedDownloadState":"importPending","trackedDownloadStatus":"ok"}`
	var r arrQueueRecord
	if err := json.Unmarshal([]byte(j), &r); err != nil {
		t.Fatal(err)
	}
	if r.ID != 42 || r.Status != "completed" || r.TrackedDownloadState != "importPending" || r.TrackedDownloadStatus != "ok" {
		t.Fatalf("%+v", r)
	}
}

func TestArrQueueRecordUnmarshalJSON_NumericEnums(t *testing.T) {
	const j = `{"id":1,"downloadId":"x","title":"t","status":4,"trackedDownloadState":2,"trackedDownloadStatus":0}`
	var r arrQueueRecord
	if err := json.Unmarshal([]byte(j), &r); err != nil {
		t.Fatal(err)
	}
	if r.Status != "completed" || r.TrackedDownloadState != "importPending" || r.TrackedDownloadStatus != "ok" {
		t.Fatalf("%+v", r)
	}
}

func TestTorrentStatusColumnText_NonArrCategory(t *testing.T) {
	text, col := torrentStatusColumnText(Torrent{Category: "TV", State: "stoppedDL"}, nil, nil)
	if text != "stopped" || col == cyanColor {
		t.Fatalf("%q %q", text, col)
	}
}

func TestArrCredentialsUsable(t *testing.T) {
	if !arrCredentialsUsable("http://127.0.0.1:8989", "key") {
		t.Fatal("expected usable")
	}
	if arrCredentialsUsable("http://127.0.0.1:8989", "") {
		t.Fatal("missing key")
	}
	if arrCredentialsUsable("", "key") {
		t.Fatal("missing url")
	}
	if arrCredentialsUsable("ftp://h/x", "key") {
		t.Fatal("bad scheme")
	}
}
