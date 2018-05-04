/*
 * Minio Cloud Storage, (C) 2016 Minio, Inc.
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
	"fmt"
	"hash"
	"strings"

	"github.com/minio/minio/cmd/logger"
)

// HealFile tries to reconstruct an erasure-coded file spread over all
// available disks. HealFile will read the valid parts of the file,
// reconstruct the missing data and write the reconstructed parts back
// to `staleDisks` at the destination `dstVol/dstPath/`. Parts are
// verified against the given BitrotAlgorithm and checksums.
//
// `staleDisks` is a slice of disks where each non-nil entry has stale
// or no data, and so will be healed.
//
// It is required that `s.disks` have a (read-quorum) majority of
// disks with valid data for healing to work.
//
// In addition, `staleDisks` and `s.disks` must have the same ordering
// of disks w.r.t. erasure coding of the object.
//
// Errors when writing to `staleDisks` are not propagated as long as
// writes succeed for at least one disk. This allows partial healing
// despite stale disks being faulty.
//
// It returns bitrot checksums for the non-nil staleDisks on which
// healing succeeded.
func (s ErasureStorage) HealFile(ctx context.Context, staleDisks []StorageAPI, volume, path string, blocksize int64,
	dstVol, dstPath string, size int64, alg BitrotAlgorithm, checksums [][]byte) (
	f ErasureFileInfo, err error) {

	if !alg.Available() {
		logger.LogIf(ctx, errBitrotHashAlgoInvalid)
		return f, errBitrotHashAlgoInvalid
	}

	// Initialization
	f.Checksums = make([][]byte, len(s.disks))
	hashers := make([]hash.Hash, len(s.disks))
	verifiers := make([]*BitrotVerifier, len(s.disks))
	for i, disk := range s.disks {
		switch {
		case staleDisks[i] != nil:
			hashers[i] = alg.New()
		case disk == nil:
			// disregard unavailable disk
			continue
		default:
			verifiers[i] = NewBitrotVerifier(alg, checksums[i])
		}
	}
	writeErrors := make([]error, len(s.disks))

	// Read part file data on each disk
	chunksize := ceilFrac(blocksize, int64(s.dataBlocks))
	numBlocks := ceilFrac(size, blocksize)

	readLen := chunksize * (numBlocks - 1)

	lastChunkSize := chunksize
	hasSmallerLastBlock := size%blocksize != 0
	if hasSmallerLastBlock {
		lastBlockLen := size % blocksize
		lastChunkSize = ceilFrac(lastBlockLen, int64(s.dataBlocks))
	}
	readLen += lastChunkSize
	var buffers [][]byte
	buffers, _, err = s.readConcurrent(ctx, volume, path, 0, readLen, verifiers)
	if err != nil {
		return f, err
	}

	// Scan part files on disk, block-by-block reconstruct it and
	// write to stale disks.
	blocks := make([][]byte, len(s.disks))

	if numBlocks > 1 {
		// Allocate once for all the equal length blocks. The
		// last block may have a different length - allocation
		// for this happens inside the for loop below.
		for i := range blocks {
			if len(buffers[i]) == 0 {
				blocks[i] = make([]byte, chunksize)
			}
		}
	}

	var buffOffset int64
	for blockNumber := int64(0); blockNumber < numBlocks; blockNumber++ {
		if blockNumber == numBlocks-1 && lastChunkSize != chunksize {
			for i := range blocks {
				if len(buffers[i]) == 0 {
					blocks[i] = make([]byte, lastChunkSize)
				}
			}
		}

		for i := range blocks {
			if len(buffers[i]) == 0 {
				blocks[i] = blocks[i][0:0]
			}
		}

		csize := chunksize
		if blockNumber == numBlocks-1 {
			csize = lastChunkSize
		}
		for i := range blocks {
			if len(buffers[i]) != 0 {
				blocks[i] = buffers[i][buffOffset : buffOffset+csize]
			}
		}
		buffOffset += csize

		if err = s.ErasureDecodeDataAndParityBlocks(ctx, blocks); err != nil {
			return f, err
		}

		// write computed shards as chunks on file in each
		// stale disk
		writeSucceeded := false
		for i, disk := range staleDisks {
			// skip nil disk or disk that had error on
			// previous write
			if disk == nil || writeErrors[i] != nil {
				continue
			}

			writeErrors[i] = disk.AppendFile(dstVol, dstPath, blocks[i])
			if writeErrors[i] == nil {
				hashers[i].Write(blocks[i])
				writeSucceeded = true
			}
		}

		// If all disks had write errors we quit.
		if !writeSucceeded {
			// build error from all write errors
			err := joinWriteErrors(writeErrors)
			logger.LogIf(ctx, err)
			return f, err
		}
	}

	// copy computed file hashes into output variable
	f.Size = size
	f.Algorithm = alg
	for i, disk := range staleDisks {
		if disk == nil || writeErrors[i] != nil {
			continue
		}
		f.Checksums[i] = hashers[i].Sum(nil)
	}
	return f, nil
}

func joinWriteErrors(errs []error) error {
	msgs := []string{}
	for i, err := range errs {
		if err == nil {
			continue
		}
		msgs = append(msgs, fmt.Sprintf("disk %d: %v", i+1, err))
	}
	return fmt.Errorf("all stale disks had write errors during healing: %s",
		strings.Join(msgs, ", "))
}
