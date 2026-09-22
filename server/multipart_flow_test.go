package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func newMultipartTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s3Client = NewS3Client()
	mux := http.NewServeMux()
	mux.HandleFunc("/presign", handlePresign)
	mux.HandleFunc("/multipart/init", handleMultipartInit)
	mux.HandleFunc("/multipart/complete", handleMultipartComplete)
	mux.HandleFunc("/multipart/abort", handleMultipartAbort)
	return httptest.NewServer(mux)
}

func TestMultipartAbortDeletesUploadedParts(t *testing.T) {
	ts := newMultipartTestServer(t)
	defer ts.Close()

	camID, sessionID, fileName := "TESTCAM", "aborttest", fmt.Sprintf("part-abort-%d.bin", time.Now().UnixNano())
	q := url.Values{"camID": {camID}, "sessionID": {sessionID}, "fileName": {fileName}}

	initResp, err := http.Get(ts.URL + "/multipart/init?" + q.Encode())
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	uploadID, err := readOKBody(initResp)
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	partQ := url.Values{"camID": {camID}, "sessionID": {sessionID}, "fileName": {fileName}, "uploadId": {uploadID}, "partNumber": {"1"}}
	presignResp, err := http.Get(ts.URL + "/presign?" + partQ.Encode())
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	presignedURL, err := readOKBody(presignResp)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}

	part := bytes.Repeat([]byte("a"), 5*1024*1024)
	putReq, err := http.NewRequest(http.MethodPut, presignedURL, bytes.NewReader(part))
	if err != nil {
		t.Fatalf("put req: %v", err)
	}
	putReq.ContentLength = int64(len(part))
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatalf("put part: %v", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK && putResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(putResp.Body)
		t.Fatalf("put part status %s: %s", putResp.Status, body)
	}

	abortQ := url.Values{"camID": {camID}, "sessionID": {sessionID}, "fileName": {fileName}, "uploadId": {uploadID}}
	abortResp, err := http.Get(ts.URL + "/multipart/abort?" + abortQ.Encode())
	if err != nil {
		t.Fatalf("abort: %v", err)
	}
	if _, err := readOKBody(abortResp); err != nil {
		t.Fatalf("abort: %v", err)
	}

	_, err = s3Client.ListParts(t.Context(), &s3.ListPartsInput{
		Bucket:   aws.String(MinioBucket),
		Key:      aws.String(camID + "/" + sessionID + "/" + fileName),
		UploadId: aws.String(uploadID),
	})
	if err == nil {
		t.Fatal("expected ListParts to fail after abort")
	}

	_, err = s3Client.GetObject(t.Context(), &s3.GetObjectInput{
		Bucket: aws.String(MinioBucket),
		Key:    aws.String(camID + "/" + sessionID + "/" + fileName),
	})
	if err == nil {
		t.Fatal("aborted upload should not leave a completed object")
	}
}

func TestMultipartCompleteUploadsObject(t *testing.T) {
	ts := newMultipartTestServer(t)
	defer ts.Close()

	camID, sessionID, fileName := "TESTCAM", "completetest", fmt.Sprintf("part-complete-%d.bin", time.Now().UnixNano())
	q := url.Values{"camID": {camID}, "sessionID": {sessionID}, "fileName": {fileName}}
	key := camID + "/" + sessionID + "/" + fileName
	t.Cleanup(func() {
		_, _ = s3Client.DeleteObject(t.Context(), &s3.DeleteObjectInput{
			Bucket: aws.String(MinioBucket),
			Key:    aws.String(key),
		})
	})

	initResp, err := http.Get(ts.URL + "/multipart/init?" + q.Encode())
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	uploadID, err := readOKBody(initResp)
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	part1 := bytes.Repeat([]byte("b"), 5*1024*1024)
	part2 := bytes.Repeat([]byte("c"), 1024)
	payloads := [][]byte{part1, part2}
	var completed []types.CompletedPart
	for i, payload := range payloads {
		partQ := url.Values{
			"camID": {camID}, "sessionID": {sessionID}, "fileName": {fileName},
			"uploadId": {uploadID}, "partNumber": {fmt.Sprintf("%d", i+1)},
		}
		presignResp, err := http.Get(ts.URL + "/presign?" + partQ.Encode())
		if err != nil {
			t.Fatalf("presign part %d: %v", i+1, err)
		}
		presignedURL, err := readOKBody(presignResp)
		if err != nil {
			t.Fatalf("presign part %d: %v", i+1, err)
		}
		putReq, err := http.NewRequest(http.MethodPut, presignedURL, bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("put req part %d: %v", i+1, err)
		}
		putReq.ContentLength = int64(len(payload))
		putResp, err := http.DefaultClient.Do(putReq)
		if err != nil {
			t.Fatalf("put part %d: %v", i+1, err)
		}
		body, _ := io.ReadAll(putResp.Body)
		putResp.Body.Close()
		if putResp.StatusCode != http.StatusOK && putResp.StatusCode != http.StatusNoContent {
			t.Fatalf("put part %d status %s: %s", i+1, putResp.Status, body)
		}
		etag := putResp.Header.Get("ETag")
		if etag == "" {
			t.Fatalf("part %d missing ETag", i+1)
		}
		completed = append(completed, types.CompletedPart{
			ETag:       aws.String(etag),
			PartNumber: aws.Int32(int32(i + 1)),
		})
	}

	payload, err := json.Marshal(completed)
	if err != nil {
		t.Fatalf("marshal parts: %v", err)
	}
	completeQ := url.Values{"camID": {camID}, "sessionID": {sessionID}, "fileName": {fileName}, "uploadId": {uploadID}}
	completeResp, err := http.Post(ts.URL+"/multipart/complete?"+completeQ.Encode(), "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := readOKBody(completeResp); err != nil {
		t.Fatalf("complete: %v", err)
	}

	out, err := s3Client.GetObject(t.Context(), &s3.GetObjectInput{
		Bucket: aws.String(MinioBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("get completed object: %v", err)
	}
	got, err := io.ReadAll(out.Body)
	out.Body.Close()
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	want := append(part1, part2...)
	if !bytes.Equal(got, want) {
		t.Fatalf("object bytes mismatch: got %d want %d", len(got), len(want))
	}
}

func readOKBody(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %s: %s", resp.Status, bytes.TrimSpace(body))
	}
	return string(bytes.TrimSpace(body)), nil
}
