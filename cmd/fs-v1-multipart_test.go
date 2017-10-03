/*
 * Minio Cloud Storage, (C) 2016, 2017 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFSCleanupMultipartUploadsInRoutine(t *testing.T) {
	// Prepare for tests
	disk := filepath.Join(globalTestTmpDir, "minio-"+nextSuffix())
	defer os.RemoveAll(disk)

	obj := initFSObjects(disk, t)
	fs := obj.(*fsObjects)

	// Close the go-routine, we are going to
	// manually start it and test in this test case.
	globalServiceDoneCh <- struct{}{}

	bucketName := "bucket"
	objectName := "object"

	obj.MakeBucketWithLocation(bucketName, "")
	uploadID, err := obj.NewMultipartUpload(bucketName, objectName, nil)
	if err != nil {
		t.Fatal("Unexpected err: ", err)
	}

	go fs.cleanupStaleMultipartUploads(20*time.Millisecond, 0, globalServiceDoneCh)

	// Wait for 40ms such that - we have given enough time for
	// cleanup routine to kick in.
	time.Sleep(40 * time.Millisecond)

	// Close the routine we do not need it anymore.
	globalServiceDoneCh <- struct{}{}

	// Check if upload id was already purged.
	if err = obj.AbortMultipartUpload(bucketName, objectName, uploadID); err != nil {
		err = errorCause(err)
		if _, ok := err.(InvalidUploadID); !ok {
			t.Fatal("Unexpected err: ", err)
		}
	}
}

// Tests cleanup of stale upload ids.
func TestFSCleanupMultipartUpload(t *testing.T) {
	// Prepare for tests
	disk := filepath.Join(globalTestTmpDir, "minio-"+nextSuffix())
	defer os.RemoveAll(disk)

	obj := initFSObjects(disk, t)
	fs := obj.(*fsObjects)

	// Close the multipart cleanup go-routine.
	// In this test we are going to manually call
	// the function which actually cleans the stale
	// uploads.
	globalServiceDoneCh <- struct{}{}

	bucketName := "bucket"
	objectName := "object"

	obj.MakeBucketWithLocation(bucketName, "")
	uploadID, err := obj.NewMultipartUpload(bucketName, objectName, nil)
	if err != nil {
		t.Fatal("Unexpected err: ", err)
	}

	if err = fs.cleanupStaleMultipartUpload(bucketName, 0); err != nil {
		t.Fatal("Unexpected err: ", err)
	}

	// Check if upload id was already purged.
	if err = obj.AbortMultipartUpload(bucketName, objectName, uploadID); err != nil {
		err = errorCause(err)
		if _, ok := err.(InvalidUploadID); !ok {
			t.Fatal("Unexpected err: ", err)
		}
	}
}

// TestFSWriteUploadJSON - tests for writeUploadJSON for FS
func TestFSWriteUploadJSON(t *testing.T) {
	// Prepare for tests
	disk := filepath.Join(globalTestTmpDir, "minio-"+nextSuffix())
	defer os.RemoveAll(disk)

	obj := initFSObjects(disk, t)
	fs := obj.(*fsObjects)

	bucketName := "bucket"
	objectName := "object"

	obj.MakeBucketWithLocation(bucketName, "")
	_, err := obj.NewMultipartUpload(bucketName, objectName, nil)
	if err != nil {
		t.Fatal("Unexpected err: ", err)
	}

	// newMultipartUpload will fail.
	fs.fsPath = filepath.Join(globalTestTmpDir, "minio-"+nextSuffix())
	_, err = obj.NewMultipartUpload(bucketName, objectName, nil)
	if err != nil {
		if _, ok := errorCause(err).(BucketNotFound); !ok {
			t.Fatal("Unexpected err: ", err)
		}
	}
}

// TestNewMultipartUploadFaultyDisk - test NewMultipartUpload with faulty disks
func TestNewMultipartUploadFaultyDisk(t *testing.T) {
	// Prepare for tests
	disk := filepath.Join(globalTestTmpDir, "minio-"+nextSuffix())
	defer os.RemoveAll(disk)
	obj := initFSObjects(disk, t)

	fs := obj.(*fsObjects)
	bucketName := "bucket"
	objectName := "object"

	if err := obj.MakeBucketWithLocation(bucketName, ""); err != nil {
		t.Fatal("Cannot create bucket, err: ", err)
	}

	// Test with disk removed.
	fs.fsPath = filepath.Join(globalTestTmpDir, "minio-"+nextSuffix())
	if _, err := fs.NewMultipartUpload(bucketName, objectName, map[string]string{"X-Amz-Meta-xid": "3f"}); err != nil {
		if !isSameType(errorCause(err), BucketNotFound{}) {
			t.Fatal("Unexpected error ", err)
		}
	}
}

// TestPutObjectPartFaultyDisk - test PutObjectPart with faulty disks
func TestPutObjectPartFaultyDisk(t *testing.T) {
	root, err := newTestConfig(globalMinioDefaultRegion)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	// Prepare for tests
	disk := filepath.Join(globalTestTmpDir, "minio-"+nextSuffix())
	defer os.RemoveAll(disk)
	obj := initFSObjects(disk, t)
	fs := obj.(*fsObjects)
	bucketName := "bucket"
	objectName := "object"
	data := []byte("12345")
	dataLen := int64(len(data))

	if err = obj.MakeBucketWithLocation(bucketName, ""); err != nil {
		t.Fatal("Cannot create bucket, err: ", err)
	}

	uploadID, err := fs.NewMultipartUpload(bucketName, objectName, map[string]string{"X-Amz-Meta-xid": "3f"})
	if err != nil {
		t.Fatal("Unexpected error ", err)
	}

	md5Hex := getMD5Hash(data)
	sha256sum := ""

	fs.fsPath = filepath.Join(globalTestTmpDir, "minio-"+nextSuffix())
	_, err = fs.PutObjectPart(bucketName, objectName, uploadID, 1, NewHashReader(bytes.NewReader(data), dataLen, md5Hex, sha256sum))
	if !isSameType(errorCause(err), BucketNotFound{}) {
		t.Fatal("Unexpected error ", err)
	}
}

// TestCompleteMultipartUploadFaultyDisk - test CompleteMultipartUpload with faulty disks
func TestCompleteMultipartUploadFaultyDisk(t *testing.T) {
	// Prepare for tests
	disk := filepath.Join(globalTestTmpDir, "minio-"+nextSuffix())
	defer os.RemoveAll(disk)
	obj := initFSObjects(disk, t)

	fs := obj.(*fsObjects)
	bucketName := "bucket"
	objectName := "object"
	data := []byte("12345")

	if err := obj.MakeBucketWithLocation(bucketName, ""); err != nil {
		t.Fatal("Cannot create bucket, err: ", err)
	}

	uploadID, err := fs.NewMultipartUpload(bucketName, objectName, map[string]string{"X-Amz-Meta-xid": "3f"})
	if err != nil {
		t.Fatal("Unexpected error ", err)
	}

	md5Hex := getMD5Hash(data)

	if _, err := fs.PutObjectPart(bucketName, objectName, uploadID, 1, NewHashReader(bytes.NewReader(data), 5, md5Hex, "")); err != nil {
		t.Fatal("Unexpected error ", err)
	}

	parts := []completePart{{PartNumber: 1, ETag: md5Hex}}

	fs.fsPath = filepath.Join(globalTestTmpDir, "minio-"+nextSuffix())
	if _, err := fs.CompleteMultipartUpload(bucketName, objectName, uploadID, parts); err != nil {
		if !isSameType(errorCause(err), BucketNotFound{}) {
			t.Fatal("Unexpected error ", err)
		}
	}
}

// TestListMultipartUploadsFaultyDisk - test ListMultipartUploads with faulty disks
func TestListMultipartUploadsFaultyDisk(t *testing.T) {
	// Prepare for tests
	disk := filepath.Join(globalTestTmpDir, "minio-"+nextSuffix())
	defer os.RemoveAll(disk)

	obj := initFSObjects(disk, t)

	fs := obj.(*fsObjects)
	bucketName := "bucket"
	objectName := "object"
	data := []byte("12345")

	if err := obj.MakeBucketWithLocation(bucketName, ""); err != nil {
		t.Fatal("Cannot create bucket, err: ", err)
	}

	uploadID, err := fs.NewMultipartUpload(bucketName, objectName, map[string]string{"X-Amz-Meta-xid": "3f"})
	if err != nil {
		t.Fatal("Unexpected error ", err)
	}

	md5Hex := getMD5Hash(data)
	sha256sum := ""

	if _, err := fs.PutObjectPart(bucketName, objectName, uploadID, 1, NewHashReader(bytes.NewReader(data), 5, md5Hex, sha256sum)); err != nil {
		t.Fatal("Unexpected error ", err)
	}

	fs.fsPath = filepath.Join(globalTestTmpDir, "minio-"+nextSuffix())
	if _, err := fs.ListMultipartUploads(bucketName, objectName, "", "", "", 1000); err != nil {
		if !isSameType(errorCause(err), BucketNotFound{}) {
			t.Fatal("Unexpected error ", err)
		}
	}
}
