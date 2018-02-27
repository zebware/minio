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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	pathutil "path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio/pkg/errors"
	mioutil "github.com/minio/minio/pkg/ioutil"

	"github.com/minio/minio/pkg/hash"
)

// Returns EXPORT/.minio.sys/multipart/SHA256/UPLOADID
func (fs *FSObjects) getUploadIDDir(bucket, object, uploadID string) string {
	return pathJoin(fs.fsPath, minioMetaMultipartBucket, getSHA256Hash([]byte(pathJoin(bucket, object))), uploadID)
}

// Returns EXPORT/.minio.sys/multipart/SHA256
func (fs *FSObjects) getMultipartSHADir(bucket, object string) string {
	return pathJoin(fs.fsPath, minioMetaMultipartBucket, getSHA256Hash([]byte(pathJoin(bucket, object))))
}

// Returns partNumber.etag
func (fs *FSObjects) encodePartFile(partNumber int, etag string) string {
	return fmt.Sprintf("%.5d.%s", partNumber, etag)
}

// Returns partNumber and etag
func (fs *FSObjects) decodePartFile(name string) (partNumber int, etag string, err error) {
	result := strings.Split(name, ".")
	if len(result) != 2 {
		return 0, "", errUnexpected
	}
	partNumber, err = strconv.Atoi(result[0])
	if err != nil {
		return 0, "", errUnexpected
	}
	return partNumber, result[1], nil
}

// Appends parts to an appendFile sequentially.
func (fs *FSObjects) backgroundAppend(bucket, object, uploadID string) {
	fs.appendFileMapMu.Lock()
	file := fs.appendFileMap[uploadID]
	if file == nil {
		file = &fsAppendFile{
			filePath: pathJoin(fs.fsPath, minioMetaTmpBucket, fs.fsUUID, fmt.Sprintf("%s.%s", uploadID, mustGetUUID())),
		}
		fs.appendFileMap[uploadID] = file
	}
	fs.appendFileMapMu.Unlock()

	file.Lock()
	defer file.Unlock()

	// Since we append sequentially nextPartNumber will always be len(file.parts)+1
	nextPartNumber := len(file.parts) + 1
	uploadIDDir := fs.getUploadIDDir(bucket, object, uploadID)

	entries, err := readDir(uploadIDDir)
	if err != nil {
		errorIf(err, "error reading directory %s", uploadIDDir)
		return
	}
	sort.Strings(entries)

	for _, entry := range entries {
		if entry == fsMetaJSONFile {
			continue
		}
		partNumber, etag, err := fs.decodePartFile(entry)
		if err != nil {
			errorIf(err, "unable to split the file name into partNumber and etag: %s", entry)
			return
		}
		if partNumber < nextPartNumber {
			// Part already appended.
			continue
		}
		if partNumber > nextPartNumber {
			// Required part number is not yet uploaded.
			return
		}

		partPath := pathJoin(uploadIDDir, entry)
		err = mioutil.AppendFile(file.filePath, partPath)
		if err != nil {
			errorIf(err, "Unable to append %s to %s", partPath, file.filePath)
			return
		}

		file.parts = append(file.parts, PartInfo{PartNumber: nextPartNumber, ETag: etag})
		nextPartNumber++
	}
}

// ListMultipartUploads - lists all the uploadIDs for the specified object.
// We do not support prefix based listing.
func (fs *FSObjects) ListMultipartUploads(bucket, object, keyMarker, uploadIDMarker, delimiter string, maxUploads int) (result ListMultipartsInfo, e error) {
	if err := checkListMultipartArgs(bucket, object, keyMarker, uploadIDMarker, delimiter, fs); err != nil {
		return result, toObjectErr(errors.Trace(err))
	}

	if _, err := fs.statBucketDir(bucket); err != nil {
		return result, toObjectErr(errors.Trace(err), bucket)
	}

	result.MaxUploads = maxUploads
	result.KeyMarker = keyMarker
	result.Prefix = object
	result.Delimiter = delimiter
	result.NextKeyMarker = object
	result.UploadIDMarker = uploadIDMarker

	uploadIDs, err := readDir(fs.getMultipartSHADir(bucket, object))
	if err != nil {
		if err == errFileNotFound {
			result.IsTruncated = false
			return result, nil
		}
		return result, toObjectErr(errors.Trace(err))
	}

	// S3 spec says uploaIDs should be sorted based on initiated time. ModTime of fs.json
	// is the creation time of the uploadID, hence we will use that.
	var uploads []MultipartInfo
	for _, uploadID := range uploadIDs {
		metaFilePath := pathJoin(fs.getMultipartSHADir(bucket, object), uploadID, fsMetaJSONFile)
		fi, err := fsStatFile(metaFilePath)
		if err != nil {
			return result, toObjectErr(err, bucket, object)
		}
		uploads = append(uploads, MultipartInfo{
			Object:    object,
			UploadID:  strings.TrimSuffix(uploadID, slashSeparator),
			Initiated: fi.ModTime(),
		})
	}
	sort.Slice(uploads, func(i int, j int) bool {
		return uploads[i].Initiated.Before(uploads[j].Initiated)
	})

	uploadIndex := 0
	if uploadIDMarker != "" {
		for uploadIndex < len(uploads) {
			if uploads[uploadIndex].UploadID != uploadIDMarker {
				uploadIndex++
				continue
			}
			if uploads[uploadIndex].UploadID == uploadIDMarker {
				uploadIndex++
				break
			}
			uploadIndex++
		}
	}
	for uploadIndex < len(uploads) {
		result.Uploads = append(result.Uploads, uploads[uploadIndex])
		result.NextUploadIDMarker = uploads[uploadIndex].UploadID
		uploadIndex++
		if len(result.Uploads) == maxUploads {
			break
		}
	}

	result.IsTruncated = uploadIndex < len(uploads)

	if !result.IsTruncated {
		result.NextKeyMarker = ""
		result.NextUploadIDMarker = ""
	}

	return result, nil
}

// NewMultipartUpload - initialize a new multipart upload, returns a
// unique id. The unique id returned here is of UUID form, for each
// subsequent request each UUID is unique.
//
// Implements S3 compatible initiate multipart API.
func (fs *FSObjects) NewMultipartUpload(bucket, object string, meta map[string]string) (string, error) {
	if err := checkNewMultipartArgs(bucket, object, fs); err != nil {
		return "", toObjectErr(err, bucket)
	}

	if _, err := fs.statBucketDir(bucket); err != nil {
		return "", toObjectErr(err, bucket)
	}

	uploadID := mustGetUUID()
	uploadIDDir := fs.getUploadIDDir(bucket, object, uploadID)

	err := mkdirAll(uploadIDDir, 0755)
	if err != nil {
		return "", errors.Trace(err)
	}

	// Initialize fs.json values.
	fsMeta := newFSMetaV1()
	fsMeta.Meta = meta

	fsMetaBytes, err := json.Marshal(fsMeta)
	if err != nil {
		return "", errors.Trace(err)
	}

	if err = ioutil.WriteFile(pathJoin(uploadIDDir, fsMetaJSONFile), fsMetaBytes, 0644); err != nil {
		return "", errors.Trace(err)
	}
	return uploadID, nil
}

// CopyObjectPart - similar to PutObjectPart but reads data from an existing
// object. Internally incoming data is written to '.minio.sys/tmp' location
// and safely renamed to '.minio.sys/multipart' for reach parts.
func (fs *FSObjects) CopyObjectPart(srcBucket, srcObject, dstBucket, dstObject, uploadID string, partID int,
	startOffset int64, length int64, srcInfo ObjectInfo) (pi PartInfo, e error) {

	if err := checkNewMultipartArgs(srcBucket, srcObject, fs); err != nil {
		return pi, toObjectErr(errors.Trace(err))
	}

	// Initialize pipe.
	pipeReader, pipeWriter := io.Pipe()

	go func() {
		if gerr := fs.GetObject(srcBucket, srcObject, startOffset, length, pipeWriter, srcInfo.ETag); gerr != nil {
			errorIf(gerr, "Unable to read %s/%s.", srcBucket, srcObject)
			pipeWriter.CloseWithError(gerr)
			return
		}
		pipeWriter.Close() // Close writer explicitly signalling we wrote all data.
	}()

	hashReader, err := hash.NewReader(pipeReader, length, "", "")
	if err != nil {
		return pi, toObjectErr(err, dstBucket, dstObject)
	}

	partInfo, err := fs.PutObjectPart(dstBucket, dstObject, uploadID, partID, hashReader)
	if err != nil {
		return pi, toObjectErr(err, dstBucket, dstObject)
	}

	// Explicitly close the reader.
	pipeReader.Close()

	return partInfo, nil
}

// PutObjectPart - reads incoming data until EOF for the part file on
// an ongoing multipart transaction. Internally incoming data is
// written to '.minio.sys/tmp' location and safely renamed to
// '.minio.sys/multipart' for reach parts.
func (fs *FSObjects) PutObjectPart(bucket, object, uploadID string, partID int, data *hash.Reader) (pi PartInfo, e error) {
	if err := checkPutObjectPartArgs(bucket, object, fs); err != nil {
		return pi, toObjectErr(errors.Trace(err), bucket)
	}

	if _, err := fs.statBucketDir(bucket); err != nil {
		return pi, toObjectErr(errors.Trace(err), bucket)
	}

	// Validate input data size and it can never be less than zero.
	if data.Size() < 0 {
		return pi, toObjectErr(errors.Trace(errInvalidArgument))
	}

	uploadIDDir := fs.getUploadIDDir(bucket, object, uploadID)

	// Just check if the uploadID exists to avoid copy if it doesn't.
	_, err := fsStatFile(pathJoin(uploadIDDir, fsMetaJSONFile))
	if err != nil {
		if errors.Cause(err) == errFileNotFound || errors.Cause(err) == errFileAccessDenied {
			return pi, errors.Trace(InvalidUploadID{UploadID: uploadID})
		}
		return pi, toObjectErr(err, bucket, object)
	}

	bufSize := int64(readSizeV1)
	if size := data.Size(); size > 0 && bufSize > size {
		bufSize = size
	}
	buf := make([]byte, bufSize)

	tmpPartPath := pathJoin(fs.fsPath, minioMetaTmpBucket, fs.fsUUID, uploadID+"."+mustGetUUID()+"."+strconv.Itoa(partID))
	bytesWritten, err := fsCreateFile(tmpPartPath, data, buf, data.Size())
	if err != nil {
		fsRemoveFile(tmpPartPath)
		return pi, toObjectErr(err, minioMetaTmpBucket, tmpPartPath)
	}

	// Should return IncompleteBody{} error when reader has fewer
	// bytes than specified in request header.
	if bytesWritten < data.Size() {
		fsRemoveFile(tmpPartPath)
		return pi, errors.Trace(IncompleteBody{})
	}

	// Delete temporary part in case of failure. If
	// PutObjectPart succeeds then there would be nothing to
	// delete in which case we just ignore the error.
	defer fsRemoveFile(tmpPartPath)

	etag := hex.EncodeToString(data.MD5Current())
	if etag == "" {
		etag = GenETag()
	}
	partPath := pathJoin(uploadIDDir, fs.encodePartFile(partID, etag))

	if err = fsRenameFile(tmpPartPath, partPath); err != nil {
		return pi, toObjectErr(err, minioMetaMultipartBucket, partPath)
	}

	go fs.backgroundAppend(bucket, object, uploadID)

	fi, err := fsStatFile(partPath)
	if err != nil {
		return pi, toObjectErr(err, minioMetaMultipartBucket, partPath)
	}
	return PartInfo{
		PartNumber:   partID,
		LastModified: fi.ModTime(),
		ETag:         etag,
		Size:         fi.Size(),
	}, nil
}

// ListObjectParts - lists all previously uploaded parts for a given
// object and uploadID.  Takes additional input of part-number-marker
// to indicate where the listing should begin from.
//
// Implements S3 compatible ListObjectParts API. The resulting
// ListPartsInfo structure is unmarshalled directly into XML and
// replied back to the client.
func (fs *FSObjects) ListObjectParts(bucket, object, uploadID string, partNumberMarker, maxParts int) (result ListPartsInfo, e error) {
	if err := checkListPartsArgs(bucket, object, fs); err != nil {
		return result, toObjectErr(errors.Trace(err))
	}
	result.Bucket = bucket
	result.Object = object
	result.UploadID = uploadID
	result.MaxParts = maxParts
	result.PartNumberMarker = partNumberMarker

	// Check if bucket exists
	if _, err := fs.statBucketDir(bucket); err != nil {
		return result, toObjectErr(errors.Trace(err), bucket)
	}

	uploadIDDir := fs.getUploadIDDir(bucket, object, uploadID)
	_, err := fsStatFile(pathJoin(uploadIDDir, fsMetaJSONFile))
	if err != nil {
		if errors.Cause(err) == errFileNotFound || errors.Cause(err) == errFileAccessDenied {
			return result, errors.Trace(InvalidUploadID{UploadID: uploadID})
		}
		return result, toObjectErr(errors.Trace(err), bucket, object)
	}

	entries, err := readDir(uploadIDDir)
	if err != nil {
		return result, toObjectErr(errors.Trace(err), bucket)
	}

	partsMap := make(map[int]string)
	for _, entry := range entries {
		if entry == fsMetaJSONFile {
			continue
		}
		partNumber, etag1, err := fs.decodePartFile(entry)
		if err != nil {
			return result, toObjectErr(errors.Trace(err))
		}
		etag2, ok := partsMap[partNumber]
		if !ok {
			partsMap[partNumber] = etag1
			continue
		}
		stat1, err := fsStatFile(pathJoin(uploadIDDir, fs.encodePartFile(partNumber, etag1)))
		if err != nil {
			return result, toObjectErr(errors.Trace(err))
		}
		stat2, err := fsStatFile(pathJoin(uploadIDDir, fs.encodePartFile(partNumber, etag2)))
		if err != nil {
			return result, toObjectErr(errors.Trace(err))
		}
		if stat1.ModTime().After(stat2.ModTime()) {
			partsMap[partNumber] = etag1
		}
	}
	var parts []PartInfo
	for partNumber, etag := range partsMap {
		parts = append(parts, PartInfo{PartNumber: partNumber, ETag: etag})
	}
	sort.SliceStable(parts, func(i int, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})
	i := 0
	if partNumberMarker != 0 {
		// If the marker was set, skip the entries till the marker.
		for _, part := range parts {
			i++
			if part.PartNumber == partNumberMarker {
				break
			}
		}
	}

	partsCount := 0
	for partsCount < maxParts && i < len(parts) {
		result.Parts = append(result.Parts, parts[i])
		i++
		partsCount++
	}
	if i < len(parts) {
		result.IsTruncated = true
		if partsCount != 0 {
			result.NextPartNumberMarker = result.Parts[partsCount-1].PartNumber
		}
	}
	for i, part := range result.Parts {
		stat, err := fsStatFile(pathJoin(uploadIDDir, fs.encodePartFile(part.PartNumber, part.ETag)))
		if err != nil {
			return result, toObjectErr(errors.Trace(err))
		}
		result.Parts[i].LastModified = stat.ModTime()
		result.Parts[i].Size = stat.Size()
	}
	return result, nil
}

// CompleteMultipartUpload - completes an ongoing multipart
// transaction after receiving all the parts indicated by the client.
// Returns an md5sum calculated by concatenating all the individual
// md5sums of all the parts.
//
// Implements S3 compatible Complete multipart API.
func (fs *FSObjects) CompleteMultipartUpload(bucket string, object string, uploadID string, parts []CompletePart) (oi ObjectInfo, e error) {
	if err := checkCompleteMultipartArgs(bucket, object, fs); err != nil {
		return oi, toObjectErr(err)
	}

	// Check if an object is present as one of the parent dir.
	if fs.parentDirIsObject(bucket, pathutil.Dir(object)) {
		return oi, toObjectErr(errors.Trace(errFileAccessDenied), bucket, object)
	}

	if _, err := fs.statBucketDir(bucket); err != nil {
		return oi, toObjectErr(err, bucket)
	}

	uploadIDDir := fs.getUploadIDDir(bucket, object, uploadID)
	// Just check if the uploadID exists to avoid copy if it doesn't.
	_, err := fsStatFile(pathJoin(uploadIDDir, fsMetaJSONFile))
	if err != nil {
		if errors.Cause(err) == errFileNotFound || errors.Cause(err) == errFileAccessDenied {
			return oi, errors.Trace(InvalidUploadID{UploadID: uploadID})
		}
		return oi, toObjectErr(err, bucket, object)
	}

	// Calculate s3 compatible md5sum for complete multipart.
	s3MD5, err := getCompleteMultipartMD5(parts)
	if err != nil {
		return oi, err
	}

	partSize := int64(-1) // Used later to ensure that all parts sizes are same.

	// Validate all parts and then commit to disk.
	for i, part := range parts {
		partPath := pathJoin(uploadIDDir, fs.encodePartFile(part.PartNumber, part.ETag))
		var fi os.FileInfo
		fi, err = fsStatFile(partPath)
		if err != nil {
			if errors.Cause(err) == errFileNotFound || errors.Cause(err) == errFileAccessDenied {
				return oi, errors.Trace(InvalidPart{})
			}
			return oi, errors.Trace(err)
		}
		if partSize == -1 {
			partSize = fi.Size()
		}
		if i == len(parts)-1 {
			break
		}

		// All parts except the last part has to be atleast 5MB.
		if !isMinAllowedPartSize(fi.Size()) {
			return oi, errors.Trace(PartTooSmall{
				PartNumber: part.PartNumber,
				PartSize:   fi.Size(),
				PartETag:   part.ETag,
			})
		}

		// TODO: Make necessary changes in future as explained in the below comment.
		// All parts except the last part has to be of same size. We are introducing this
		// check to see if any clients break. If clients do not break then we can optimize
		// multipart PutObjectPart by writing the part at the right offset using pwrite()
		// so that we don't need to do background append at all. i.e by the time we get
		// CompleteMultipartUpload we already have the full file available which can be
		// renamed to the main name-space.
		if partSize != fi.Size() {
			return oi, errors.Trace(PartsSizeUnequal{})
		}
	}

	appendFallback := true // In case background-append did not append the required parts.
	appendFilePath := pathJoin(fs.fsPath, minioMetaTmpBucket, fs.fsUUID, fmt.Sprintf("%s.%s", uploadID, mustGetUUID()))

	// Most of the times appendFile would already be fully appended by now. We call fs.backgroundAppend()
	// to take care of the following corner case:
	// 1. The last PutObjectPart triggers go-routine fs.backgroundAppend, this go-routine has not started yet.
	// 2. Now CompleteMultipartUpload gets called which sees that lastPart is not appended and starts appending
	//    from the beginning
	fs.backgroundAppend(bucket, object, uploadID)

	fs.appendFileMapMu.Lock()
	file := fs.appendFileMap[uploadID]
	delete(fs.appendFileMap, uploadID)
	fs.appendFileMapMu.Unlock()

	if file != nil {
		file.Lock()
		defer file.Unlock()
		// Verify that appendFile has all the parts.
		if len(file.parts) == len(parts) {
			for i := range parts {
				if parts[i].ETag != file.parts[i].ETag {
					break
				}
				if parts[i].PartNumber != file.parts[i].PartNumber {
					break
				}
				if i == len(parts)-1 {
					appendFilePath = file.filePath
					appendFallback = false
				}
			}
		}
	}

	if appendFallback {
		fsRemoveFile(file.filePath)
		for _, part := range parts {
			partPath := pathJoin(uploadIDDir, fs.encodePartFile(part.PartNumber, part.ETag))
			err = mioutil.AppendFile(appendFilePath, partPath)
			if err != nil {
				return oi, toObjectErr(errors.Trace(err))
			}
		}
	}

	// Hold write lock on the object.
	destLock := fs.nsMutex.NewNSLock(bucket, object)
	if err = destLock.GetLock(globalObjectTimeout); err != nil {
		return oi, err
	}
	defer destLock.Unlock()
	fsMetaPath := pathJoin(fs.fsPath, minioMetaBucket, bucketMetaPrefix, bucket, object, fsMetaJSONFile)
	metaFile, err := fs.rwPool.Create(fsMetaPath)
	if err != nil {
		return oi, toObjectErr(errors.Trace(err), bucket, object)
	}
	defer metaFile.Close()

	fsMeta := fsMetaV1{}
	// Read saved fs metadata for ongoing multipart.
	fsMetaBuf, err := ioutil.ReadFile(pathJoin(uploadIDDir, fsMetaJSONFile))
	if err != nil {
		return oi, toObjectErr(errors.Trace(err), bucket, object)
	}
	err = json.Unmarshal(fsMetaBuf, &fsMeta)
	if err != nil {
		return oi, toObjectErr(errors.Trace(err), bucket, object)
	}
	// Save additional metadata.
	if len(fsMeta.Meta) == 0 {
		fsMeta.Meta = make(map[string]string)
	}
	fsMeta.Meta["etag"] = s3MD5
	if _, err = fsMeta.WriteTo(metaFile); err != nil {
		return oi, toObjectErr(errors.Trace(err), bucket, object)
	}

	err = fsRenameFile(appendFilePath, pathJoin(fs.fsPath, bucket, object))
	if err != nil {
		return oi, toObjectErr(errors.Trace(err), bucket, object)
	}
	fsRemoveAll(uploadIDDir)
	fi, err := fsStatFile(pathJoin(fs.fsPath, bucket, object))
	if err != nil {
		return oi, toObjectErr(errors.Trace(err), bucket, object)
	}

	return fsMeta.ToObjectInfo(bucket, object, fi), nil
}

// AbortMultipartUpload - aborts an ongoing multipart operation
// signified by the input uploadID. This is an atomic operation
// doesn't require clients to initiate multiple such requests.
//
// All parts are purged from all disks and reference to the uploadID
// would be removed from the system, rollback is not possible on this
// operation.
//
// Implements S3 compatible Abort multipart API, slight difference is
// that this is an atomic idempotent operation. Subsequent calls have
// no affect and further requests to the same uploadID would not be
// honored.
func (fs *FSObjects) AbortMultipartUpload(bucket, object, uploadID string) error {
	if err := checkAbortMultipartArgs(bucket, object, fs); err != nil {
		return err
	}

	if _, err := fs.statBucketDir(bucket); err != nil {
		return toObjectErr(errors.Trace(err), bucket)
	}

	fs.appendFileMapMu.Lock()
	delete(fs.appendFileMap, uploadID)
	fs.appendFileMapMu.Unlock()

	uploadIDDir := fs.getUploadIDDir(bucket, object, uploadID)
	// Just check if the uploadID exists to avoid copy if it doesn't.
	_, err := fsStatFile(pathJoin(uploadIDDir, fsMetaJSONFile))
	if err != nil {
		if errors.Cause(err) == errFileNotFound || errors.Cause(err) == errFileAccessDenied {
			return errors.Trace(InvalidUploadID{UploadID: uploadID})
		}
		return toObjectErr(errors.Trace(err), bucket, object)
	}
	// Ignore the error returned as Windows fails to remove directory if a file in it
	// is Open()ed by the backgroundAppend()
	fsRemoveAll(uploadIDDir)

	return nil
}

// Removes multipart uploads if any older than `expiry` duration
// on all buckets for every `cleanupInterval`, this function is
// blocking and should be run in a go-routine.
func (fs *FSObjects) cleanupStaleMultipartUploads(cleanupInterval, expiry time.Duration, doneCh chan struct{}) {
	ticker := time.NewTicker(cleanupInterval)
	for {
		select {
		case <-doneCh:
			// Stop the timer.
			ticker.Stop()
			return
		case <-ticker.C:
			now := time.Now()
			entries, err := readDir(pathJoin(fs.fsPath, minioMetaMultipartBucket))
			if err != nil {
				continue
			}
			for _, entry := range entries {
				uploadIDs, err := readDir(pathJoin(fs.fsPath, minioMetaMultipartBucket, entry))
				if err != nil {
					continue
				}
				for _, uploadID := range uploadIDs {
					fi, err := fsStatDir(pathJoin(fs.fsPath, minioMetaMultipartBucket, entry, uploadID))
					if err != nil {
						continue
					}
					if now.Sub(fi.ModTime()) > expiry {
						fsRemoveAll(pathJoin(fs.fsPath, minioMetaMultipartBucket, entry, uploadID))
					}
				}
			}
		}
	}
}
