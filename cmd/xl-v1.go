/*
 * Minio Cloud Storage, (C) 2016, 2017, 2018 Minio, Inc.
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
	"context"
	"sort"
	"time"

	"github.com/minio/minio/cmd/logger"
	"github.com/minio/minio/pkg/bpool"
)

// XL constants.
const (
	// XL metadata file carries per object metadata.
	xlMetaJSONFile = "xl.json"
)

// xlObjects - Implements XL object layer.
type xlObjects struct {
	// name space mutex for object layer.
	nsMutex *nsLockMap

	// getDisks returns list of storageAPIs.
	getDisks func() []StorageAPI

	// Byte pools used for temporary i/o buffers.
	bp *bpool.BytePoolCap

	// TODO: Deprecated only kept here for tests, should be removed in future.
	storageDisks []StorageAPI

	// TODO: ListObjects pool management, should be removed in future.
	listPool *treeWalkPool
}

// list of all errors that can be ignored in tree walk operation in XL
var xlTreeWalkIgnoredErrs = append(baseIgnoredErrs, errDiskAccessDenied, errVolumeNotFound, errFileNotFound)

// Shutdown function for object storage interface.
func (xl xlObjects) Shutdown(ctx context.Context) error {
	// Add any object layer shutdown activities here.
	closeStorageDisks(xl.getDisks())
	return nil
}

// Locking operations

// List namespace locks held in object layer
func (xl xlObjects) ListLocks(ctx context.Context, bucket, prefix string, duration time.Duration) ([]VolumeLockInfo, error) {
	xl.nsMutex.lockMapMutex.Lock()
	defer xl.nsMutex.lockMapMutex.Unlock()
	// Fetch current time once instead of fetching system time for every lock.
	timeNow := UTCNow()
	volumeLocks := []VolumeLockInfo{}

	for param, debugLock := range xl.nsMutex.debugLockMap {
		if param.volume != bucket {
			continue
		}
		// N B empty prefix matches all param.path.
		if !hasPrefix(param.path, prefix) {
			continue
		}

		volLockInfo := VolumeLockInfo{
			Bucket:                param.volume,
			Object:                param.path,
			LocksOnObject:         debugLock.counters.total,
			TotalBlockedLocks:     debugLock.counters.blocked,
			LocksAcquiredOnObject: debugLock.counters.granted,
		}
		// Filter locks that are held on bucket, prefix.
		for opsID, lockInfo := range debugLock.lockInfo {
			// filter locks that were held for longer than duration.
			elapsed := timeNow.Sub(lockInfo.since)
			if elapsed < duration {
				continue
			}
			// Add locks that are held for longer than duration.
			volLockInfo.LockDetailsOnObject = append(volLockInfo.LockDetailsOnObject,
				OpsLockState{
					OperationID: opsID,
					LockSource:  lockInfo.lockSource,
					LockType:    lockInfo.lType,
					Status:      lockInfo.status,
					Since:       lockInfo.since,
				})
			volumeLocks = append(volumeLocks, volLockInfo)
		}
	}
	return volumeLocks, nil
}

// Clear namespace locks held in object layer
func (xl xlObjects) ClearLocks(ctx context.Context, volLocks []VolumeLockInfo) error {
	// Remove lock matching bucket/prefix held longer than duration.
	for _, volLock := range volLocks {
		xl.nsMutex.ForceUnlock(volLock.Bucket, volLock.Object)
	}
	return nil
}

// byDiskTotal is a collection satisfying sort.Interface.
type byDiskTotal []DiskInfo

func (d byDiskTotal) Len() int      { return len(d) }
func (d byDiskTotal) Swap(i, j int) { d[i], d[j] = d[j], d[i] }
func (d byDiskTotal) Less(i, j int) bool {
	return d[i].Total < d[j].Total
}

// getDisksInfo - fetch disks info across all other storage API.
func getDisksInfo(disks []StorageAPI) (disksInfo []DiskInfo, onlineDisks int, offlineDisks int) {
	disksInfo = make([]DiskInfo, len(disks))
	for i, storageDisk := range disks {
		if storageDisk == nil {
			// Storage disk is empty, perhaps ignored disk or not available.
			offlineDisks++
			continue
		}
		info, err := storageDisk.DiskInfo()
		if err != nil {
			logger.LogIf(context.Background(), err)
			if IsErr(err, baseErrs...) {
				offlineDisks++
				continue
			}
		}
		onlineDisks++
		disksInfo[i] = info
	}

	// Success.
	return disksInfo, onlineDisks, offlineDisks
}

// returns sorted disksInfo slice which has only valid entries.
// i.e the entries where the total size of the disk is not stated
// as 0Bytes, this means that the disk is not online or ignored.
func sortValidDisksInfo(disksInfo []DiskInfo) []DiskInfo {
	var validDisksInfo []DiskInfo
	for _, diskInfo := range disksInfo {
		if diskInfo.Total == 0 {
			continue
		}
		validDisksInfo = append(validDisksInfo, diskInfo)
	}
	sort.Sort(byDiskTotal(validDisksInfo))
	return validDisksInfo
}

// Get an aggregated storage info across all disks.
func getStorageInfo(disks []StorageAPI) StorageInfo {
	disksInfo, onlineDisks, offlineDisks := getDisksInfo(disks)

	// Sort so that the first element is the smallest.
	validDisksInfo := sortValidDisksInfo(disksInfo)
	// If there are no valid disks, set total and free disks to 0
	if len(validDisksInfo) == 0 {
		return StorageInfo{}
	}

	_, sscParity := getRedundancyCount(standardStorageClass, len(disks))
	_, rrscparity := getRedundancyCount(reducedRedundancyStorageClass, len(disks))

	// Total number of online data drives available
	// This is the number of drives we report free and total space for
	availableDataDisks := uint64(onlineDisks - sscParity)

	// Available data disks can be zero when onlineDisks is equal to parity,
	// at that point we simply choose online disks to calculate the size.
	if availableDataDisks == 0 {
		availableDataDisks = uint64(onlineDisks)
	}

	storageInfo := StorageInfo{}

	// Combine all disks to get total usage.
	var used uint64
	for _, di := range validDisksInfo {
		used = used + di.Used
	}
	storageInfo.Used = used

	storageInfo.Backend.Type = Erasure
	storageInfo.Backend.OnlineDisks = onlineDisks
	storageInfo.Backend.OfflineDisks = offlineDisks

	storageInfo.Backend.StandardSCParity = sscParity
	storageInfo.Backend.RRSCParity = rrscparity

	return storageInfo
}

// StorageInfo - returns underlying storage statistics.
func (xl xlObjects) StorageInfo(ctx context.Context) StorageInfo {
	return getStorageInfo(xl.getDisks())
}
