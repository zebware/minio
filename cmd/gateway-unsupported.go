/*
 * Minio Cloud Storage, (C) 2017 Minio, Inc.
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
	"time"

	"github.com/minio/minio-go/pkg/policy"
	"github.com/minio/minio/pkg/errors"
	"github.com/minio/minio/pkg/hash"
	"github.com/minio/minio/pkg/madmin"
)

// GatewayUnsupported list of unsupported call stubs for gateway.
type GatewayUnsupported struct{}

// ListMultipartUploads lists all multipart uploads.
func (a GatewayUnsupported) ListMultipartUploads(bucket string, prefix string, keyMarker string, uploadIDMarker string, delimiter string, maxUploads int) (lmi ListMultipartsInfo, err error) {
	return lmi, errors.Trace(NotImplemented{})
}

// NewMultipartUpload upload object in multiple parts
func (a GatewayUnsupported) NewMultipartUpload(bucket string, object string, metadata map[string]string) (uploadID string, err error) {
	return "", errors.Trace(NotImplemented{})
}

// CopyObjectPart copy part of object to uploadID for another object
func (a GatewayUnsupported) CopyObjectPart(srcBucket, srcObject, destBucket, destObject, uploadID string, partID int, startOffset, length int64, srcInfo ObjectInfo) (pi PartInfo, err error) {
	return pi, errors.Trace(NotImplemented{})
}

// PutObjectPart puts a part of object in bucket
func (a GatewayUnsupported) PutObjectPart(bucket string, object string, uploadID string, partID int, data *hash.Reader) (pi PartInfo, err error) {
	return pi, errors.Trace(NotImplemented{})
}

// ListObjectParts returns all object parts for specified object in specified bucket
func (a GatewayUnsupported) ListObjectParts(bucket string, object string, uploadID string, partNumberMarker int, maxParts int) (lpi ListPartsInfo, err error) {
	return lpi, errors.Trace(NotImplemented{})
}

// AbortMultipartUpload aborts a ongoing multipart upload
func (a GatewayUnsupported) AbortMultipartUpload(bucket string, object string, uploadID string) error {
	return errors.Trace(NotImplemented{})
}

// CompleteMultipartUpload completes ongoing multipart upload and finalizes object
func (a GatewayUnsupported) CompleteMultipartUpload(bucket string, object string, uploadID string, uploadedParts []CompletePart) (oi ObjectInfo, err error) {
	return oi, errors.Trace(NotImplemented{})
}

// SetBucketPolicy sets policy on bucket
func (a GatewayUnsupported) SetBucketPolicy(bucket string, policyInfo policy.BucketAccessPolicy) error {
	return errors.Trace(NotImplemented{})
}

// GetBucketPolicy will get policy on bucket
func (a GatewayUnsupported) GetBucketPolicy(bucket string) (bal policy.BucketAccessPolicy, err error) {
	return bal, errors.Trace(NotImplemented{})
}

// DeleteBucketPolicy deletes all policies on bucket
func (a GatewayUnsupported) DeleteBucketPolicy(bucket string) error {
	return errors.Trace(NotImplemented{})
}

// HealFormat - Not implemented stub
func (a GatewayUnsupported) HealFormat(dryRun bool) (madmin.HealResultItem, error) {
	return madmin.HealResultItem{}, errors.Trace(NotImplemented{})
}

// HealBucket - Not implemented stub
func (a GatewayUnsupported) HealBucket(bucket string, dryRun bool) ([]madmin.HealResultItem, error) {
	return nil, errors.Trace(NotImplemented{})
}

// ListBucketsHeal - Not implemented stub
func (a GatewayUnsupported) ListBucketsHeal() (buckets []BucketInfo, err error) {
	return nil, errors.Trace(NotImplemented{})
}

// HealObject - Not implemented stub
func (a GatewayUnsupported) HealObject(bucket, object string, dryRun bool) (h madmin.HealResultItem, e error) {
	return h, errors.Trace(NotImplemented{})
}

// ListObjectsV2 - Not implemented stub
func (a GatewayUnsupported) ListObjectsV2(bucket, prefix, continuationToken, delimiter string, maxKeys int, fetchOwner bool, startAfter string) (result ListObjectsV2Info, err error) {
	return result, errors.Trace(NotImplemented{})
}

// ListObjectsHeal - Not implemented stub
func (a GatewayUnsupported) ListObjectsHeal(bucket, prefix, marker, delimiter string, maxKeys int) (loi ListObjectsInfo, e error) {
	return loi, errors.Trace(NotImplemented{})
}

// CopyObject copies a blob from source container to destination container.
func (a GatewayUnsupported) CopyObject(srcBucket string, srcObject string, destBucket string, destObject string,
	srcInfo ObjectInfo) (objInfo ObjectInfo, err error) {
	return objInfo, errors.Trace(NotImplemented{})
}

// Locking operations

// ListLocks lists namespace locks held in object layer
func (a GatewayUnsupported) ListLocks(bucket, prefix string, duration time.Duration) ([]VolumeLockInfo, error) {
	return []VolumeLockInfo{}, errors.Trace(NotImplemented{})
}

// ClearLocks clears namespace locks held in object layer
func (a GatewayUnsupported) ClearLocks([]VolumeLockInfo) error {
	return errors.Trace(NotImplemented{})
}

// RefreshBucketPolicy refreshes cache policy with what's on disk.
func (a GatewayUnsupported) RefreshBucketPolicy(bucket string) error {
	return errors.Trace(NotImplemented{})
}

// IsNotificationSupported returns whether bucket notification is applicable for this layer.
func (a GatewayUnsupported) IsNotificationSupported() bool {
	return false
}

// IsEncryptionSupported returns whether server side encryption is applicable for this layer.
func (a GatewayUnsupported) IsEncryptionSupported() bool {
	return false
}
